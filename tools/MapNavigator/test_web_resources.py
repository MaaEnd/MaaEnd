from __future__ import annotations

import asyncio
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi import HTTPException

from web import serve


class WebResourceTest(unittest.TestCase):
    def test_zipline_records_exposes_the_current_installation_snapshot(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            records = Path(temp_dir) / "Ziplines.json"
            records.write_text('{"maps": []}', encoding="utf-8")
            with patch.object(serve, "ZIPLINE_RECORDS", records):
                response = asyncio.run(serve.api_zipline_records())

        self.assertEqual(Path(response.path), records)
        self.assertEqual(response.media_type, "application/json")

    def test_zipline_records_reports_a_missing_snapshot(self) -> None:
        missing = Path("missing") / "Ziplines.json"
        with (
            patch.object(serve, "ZIPLINE_RECORDS", missing),
            self.assertRaises(HTTPException) as context,
        ):
            asyncio.run(serve.api_zipline_records())

        self.assertEqual(context.exception.status_code, 404)
        self.assertIn("没有滑索记录", str(context.exception.detail))


if __name__ == "__main__":
    unittest.main()
