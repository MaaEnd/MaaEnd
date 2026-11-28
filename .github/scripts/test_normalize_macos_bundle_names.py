"""Tests for normalize_macos_bundle_names.py."""

import contextlib
import io
import os
import sys
import tempfile
import unicodedata
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import normalize_macos_bundle_names as normalizer

# The real offender in the bundle, spelled in NFC as it is stored in git.
NFC_NAME = unicodedata.normalize("NFC", "Seš'qamamKnucklebones.Tier2.png")
NFC_DIR = unicodedata.normalize("NFC", "Région")


def run(*args: str) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = normalizer.main(list(args))
    return code, out.getvalue(), err.getvalue()


def all_names(root: Path) -> list[str]:
    names = []
    for _, dirnames, filenames in os.walk(root):
        names.extend(dirnames + filenames)
    return names


class NormalizeTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.bundle = Path(self._tmp.name) / "MaaEnd.app"
        goods = self.bundle / "Contents" / "MacOS" / "resource" / NFC_DIR
        goods.mkdir(parents=True)
        (goods / NFC_NAME).write_bytes(b"png")
        (goods / "AstarronCrystals.Tier2.png").write_bytes(b"ascii")

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_check_fails_before_normalizing(self) -> None:
        # Some file systems normalize on create; only assert when the NFC name survived.
        if any(not unicodedata.is_normalized("NFD", n) for n in all_names(self.bundle)):
            code, _, err = run("--check", str(self.bundle))
            self.assertEqual(code, 1)
            self.assertIn("not NFD", err)

    def test_normalize_makes_every_name_nfd_and_keeps_contents(self) -> None:
        code, _, _ = run(str(self.bundle))
        self.assertEqual(code, 0)
        for name in all_names(self.bundle):
            self.assertTrue(unicodedata.is_normalized("NFD", name), name)

        goods = self.bundle / "Contents" / "MacOS" / "resource" / unicodedata.normalize("NFD", NFC_DIR)
        self.assertEqual((goods / unicodedata.normalize("NFD", NFC_NAME)).read_bytes(), b"png")
        self.assertEqual((goods / "AstarronCrystals.Tier2.png").read_bytes(), b"ascii")

        code, out, _ = run("--check", str(self.bundle))
        self.assertEqual(code, 0)
        self.assertIn("all names are NFD", out)

    def test_normalize_is_idempotent(self) -> None:
        run(str(self.bundle))
        code, out, _ = run(str(self.bundle))
        self.assertEqual(code, 0)
        self.assertIn("0 name(s) normalized", out)

    def test_missing_bundle_is_an_input_error(self) -> None:
        code, _, err = run(str(self.bundle / "nope"))
        self.assertEqual(code, 2)
        self.assertIn("not a directory", err)


if __name__ == "__main__":
    unittest.main()
