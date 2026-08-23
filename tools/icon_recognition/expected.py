"""把手动识别报告合并进完整 expected.csv 基线。"""

from __future__ import annotations

import argparse
import csv
import json
import re
from collections import Counter
from pathlib import Path
from typing import Any


LOCAL_SUFFIX = re.compile(r"\.local\d+(?=\.png$)", re.IGNORECASE)


def _roi_text(roi: dict[str, Any]) -> str:
    return f'[{roi["x"]},{roi["y"]},{roi["width"]},{roi["height"]}]'


def merge_expected_results(
    base_path: str | Path,
    report_path: str | Path,
    output_path: str | Path,
) -> None:
    base = Path(base_path)
    report_file = Path(report_path)
    rows: dict[tuple[str, str], dict[str, int]] = {}
    with base.open("r", encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        if reader.fieldnames != ["image", "roi", "item_id", "count"]:
            raise ValueError("expected CSV header must be image,roi,item_id,count")
        for row in reader:
            key = (row["image"], row["roi"])
            rows.setdefault(key, {})[row["item_id"]] = int(row["count"])

    report = json.loads(report_file.read_text(encoding="utf-8"))
    cases = report.get("cases", [])
    reported_images = {
        LOCAL_SUFFIX.sub("", case["image"])
        for case in cases
    }
    for key in [key for key in rows if key[0] in reported_images]:
        del rows[key]
    for case in cases:
        image = LOCAL_SUFFIX.sub("", case["image"])
        roi = _roi_text(case["roi"])
        detail_path = Path(case["detail"])
        if not detail_path.is_absolute():
            detail_path = report_file.parent / detail_path
        detail = json.loads(detail_path.read_text(encoding="utf-8"))
        counts = Counter(match["item_id"] for match in detail.get("matches", []))
        rows[(image, roi)] = dict(counts) if counts else {"": 0}

    output = Path(output_path)
    output.parent.mkdir(parents=True, exist_ok=True)
    with output.open("w", encoding="utf-8", newline="") as stream:
        writer = csv.writer(stream, lineterminator="\n")
        writer.writerow(["image", "roi", "item_id", "count"])
        for (image, roi), counts in sorted(rows.items()):
            for item_id, count in sorted(counts.items()):
                writer.writerow([image, roi, item_id, count])


def main() -> int:
    parser = argparse.ArgumentParser(description="合并 IconRecognition expected.csv")
    parser.add_argument("--base", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    merge_expected_results(args.base, args.report, args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
