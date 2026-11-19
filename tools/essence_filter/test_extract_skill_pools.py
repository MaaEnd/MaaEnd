"""Tests for skill pool extraction and matcher config loading."""

import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

from extract_skill_pools import load_suffix_stopwords, main


class LoadSuffixStopwordsTest(unittest.TestCase):
    def test_jsonc_matcher_config(self):
        # matcher_config 允许注释 + 尾逗号。
        body = ('{\n  // 手写注释\n  "suffixStopwords": {\n    "CN": ["提升", "提高",],\n  },\n}')
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "matcher_config.json"
            p.write_text(body, encoding="utf-8")
            got = load_suffix_stopwords(p)
        self.assertEqual(got["CN"], ["提升", "提高"])

    def test_legacy_array(self):
        body = '{"suffixStopwords": ["增强", "增幅"]}'
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "matcher_config.json"
            p.write_text(body, encoding="utf-8")
            got = load_suffix_stopwords(p)
        self.assertEqual(got["CN"], ["增强", "增幅"])

    def test_missing_file_returns_default(self):
        got = load_suffix_stopwords(Path("does_not_exist.json"))
        self.assertIn("CN", got)


class TermIDTest(unittest.TestCase):
    def regenerate(self, existing):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            weapons = root / "weapons.json"
            weapons.write_text(json.dumps({"weapon": {"skills": {"CN": ["力量"]}}}), encoding="utf-8")
            pools = root / "pools.json"
            pools.write_text(json.dumps(existing), encoding="utf-8")
            warnings = io.StringIO()
            args = ["extract_skill_pools", "--base-dir", d, "--input", str(weapons), "--output", str(pools)]
            with patch.object(sys, "argv", args), redirect_stderr(warnings):
                self.assertEqual(main(), 0)
            return json.loads(pools.read_text(encoding="utf-8"))["slot1"][0], warnings.getvalue()

    def test_preserves_term_id(self):
        row, warnings = self.regenerate({"slot1": [{"cn": "力量", "id": 7, "term_id": "gat_passive_attr_str"}]})
        self.assertEqual((row["id"], row["term_id"]), (7, "gat_passive_attr_str"))
        self.assertEqual(warnings, "")

    def test_warns_about_new_skill(self):
        row, warnings = self.regenerate({})
        self.assertEqual(row["cn"], "力量")
        self.assertNotIn("term_id", row)
        self.assertIn("[WARNING]", warnings)
        self.assertIn("term_id", warnings)
        self.assertIn("slot1/1 (力量)", warnings)


if __name__ == "__main__":
    unittest.main()
