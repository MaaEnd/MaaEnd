"""Tests for tools/essence_filter/extract_skill_pools.py JSONC handling."""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

# 把 tools/ 插到 sys.path 最前，以便裸名 import extract_skill_pools。
_TOOLS = Path(__file__).resolve().parent.parent
if str(_TOOLS) not in sys.path:
    sys.path.insert(0, str(_TOOLS))

from extract_skill_pools import load_suffix_stopwords  # noqa: E402


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


if __name__ == "__main__":
    unittest.main()
