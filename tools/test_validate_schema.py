import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tools import validate_schema


class JsoncCommentsTest(unittest.TestCase):
    def test_block_comments_do_not_join_tokens(self):
        for value in ("1/* comment */2", "tr/* comment */ue", "nu/**/ll"):
            with self.subTest(value=value):
                with self.assertRaises(json.JSONDecodeError):
                    json.loads(
                        validate_schema.strip_jsonc_comments('{"value":' + value + "}")
                    )

    def test_unterminated_block_comments_are_rejected(self):
        for comment in ("/*", "/**", "/* unterminated", "/* first\nsecond"):
            text = '{"value":1}\n' + comment
            with self.subTest(comment=comment):
                with self.assertRaises(json.JSONDecodeError) as error:
                    validate_schema.strip_jsonc_comments(text)
                self.assertEqual(error.exception.pos, text.index("/*"))
                self.assertEqual(error.exception.lineno, 2)

    def test_line_comments_end_at_each_line_ending(self):
        for newline in ("\n", "\r", "\r\n"):
            text = '{// comment' + newline + '"value":1}// final comment'
            with self.subTest(newline=repr(newline)):
                self.assertEqual(
                    json.loads(validate_schema.strip_jsonc_comments(text)), {"value": 1}
                )

    def test_comment_markers_and_escapes_in_strings_are_preserved(self):
        data = {"value": 'https://example.com/a/*b*/c//d "quoted" \\ path'}
        text = json.dumps(data)
        self.assertEqual(validate_schema.strip_jsonc_comments(text), text)

    def test_block_comments_preserve_line_endings(self):
        text = '{/* first\r\nsecond\rthird\nfourth */"value":1}'
        cleaned = validate_schema.strip_jsonc_comments(text)
        self.assertEqual(cleaned.count("\n"), text.count("\n"))
        self.assertEqual(cleaned.count("\r"), text.count("\r"))
        self.assertEqual(json.loads(cleaned), {"value": 1})

    def assert_decode_error_position(self, text, position):
        with self.assertRaises(json.JSONDecodeError) as error:
            json.loads(validate_schema.strip_jsonc_comments(text))
        expected = json.JSONDecodeError("", text, position)
        self.assertEqual(
            (error.exception.pos, error.exception.lineno, error.exception.colno),
            (expected.pos, expected.lineno, expected.colno),
        )

    def test_errors_after_block_comments_keep_original_positions(self):
        for text in (
            '{"value":/* comment */ @}',
            '{"value":/* 注释 */ @}',
            '{"value":/* first\nsecond */ @}',
            '{"value":/* first\r\nsecond */ @}',
        ):
            with self.subTest(text=text):
                self.assert_decode_error_position(text, text.index("@"))

    def test_errors_after_line_comments_keep_original_positions(self):
        for newline in ("\n", "\r", "\r\n"):
            text = '{"value": // comment' + newline + '  @}'
            with self.subTest(newline=repr(newline)):
                self.assert_decode_error_position(text, text.index("@"))

    def test_error_at_end_of_line_comment_keeps_original_position(self):
        for comment in ("//", "// final comment", "// 末尾注释"):
            text = '{"value":1' + comment
            with self.subTest(comment=comment):
                self.assert_decode_error_position(text, len(text))

    def test_valid_comments_around_values(self):
        text = '/* header */{"value"/* key */:/* value */true/**/}// final comment'
        self.assertEqual(
            json.loads(validate_schema.strip_jsonc_comments(text)), {"value": True}
        )

    def test_load_jsonc_reads_utf8_with_comments(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "resource.jsonc"
            path.write_text('{/* comment */"value":"测试"}', encoding="utf-8")
            self.assertEqual(validate_schema.load_jsonc(path), {"value": "测试"})


class SchemaValidationCliTest(unittest.TestCase):
    def run_validator(self, content):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            schema_dir = root / "schema"
            resource_dir = root / "resource"
            schema_dir.mkdir()
            resource_dir.mkdir()
            schema = {"$schema": "https://json-schema.org/draft/2020-12/schema"}
            for name in ("pipeline.schema.json", "interface.schema.json"):
                (schema_dir / name).write_text(json.dumps(schema), encoding="utf-8")
            interface = root / "interface.json"
            interface.write_text("{}", encoding="utf-8")
            resource = resource_dir / "resource.jsonc"
            resource.write_text(content, encoding="utf-8")
            env = dict(os.environ, PYTHONUTF8="1", TMP=str(root), TEMP=str(root))
            return subprocess.run(
                [
                    sys.executable,
                    str(Path(validate_schema.__file__).resolve()),
                    "--schema-dir",
                    str(schema_dir),
                    "--resource-dirs",
                    str(resource_dir),
                    "--interface-files",
                    str(interface),
                ],
                capture_output=True,
                text=True,
                encoding="utf-8",
                env=env,
                check=False,
            )

    def test_cli_rejects_invalid_jsonc(self):
        for content in (
            '{"value":1/* comment */2}',
            '{"value":tr/* comment */ue}',
            '{"value":1}/* unterminated',
        ):
            with self.subTest(content=content):
                result = self.run_validator(content)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("Some validations failed!", result.stdout)
                self.assertIn("::error file=", result.stdout)

    def test_cli_accepts_valid_jsonc(self):
        result = self.run_validator('{/* comment */"value":true}// final comment')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("All validations passed!", result.stdout)


if __name__ == "__main__":
    unittest.main()
