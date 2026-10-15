"""本地生成六个精简数据文件到 ``data/raw/``。

与 endfield-workflow 的 ``maaend_game_data`` 处理器同序跑同一批 ``build_*``
（delivery_jobs -> environment_monitoring -> sell_product -> delivery_destinations ->
teleport_anchors -> collect_points），数据来自本地兄弟仓库：

- ``../BeyondTableCfg/TableCfg``；
- ``../BeyondMemoryPack/JsonData``。

产物只带游戏世界坐标，落在不入库的 ``data/raw/``；底图像素由
``tools/pipeline-generate/fetch-data.mjs`` 下载完发布数据后一并投影进入库的
``data/*.json``（``--project-only`` 可以只投影本地这份）。

用法::

    uv run tools/pipeline-generate/data/scripts/generate.py
    uv run tools/pipeline-generate/data/scripts/generate.py --only collect_points
    uv run tools/pipeline-generate/data/scripts/generate.py --out-dir /tmp/x --force
"""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

from collect_points import build_collect_points
from delivery_destinations import build_delivery_destinations
from delivery_jobs import build_delivery_jobs
from environment_monitoring import build_environment_monitoring
from local_context import RunContext
from sell_product import build_sell_product
from teleport_anchors import build_teleport_anchors

DATA_DIR = Path(__file__).resolve().parent.parent
RAW_DIR = DATA_DIR / "raw"
MAAEND_REPO_DIR = DATA_DIR.parents[2]
ENDFIELD_WORKSPACE_DIR = MAAEND_REPO_DIR.parent
DEFAULT_TABLE_CFG_DIR = ENDFIELD_WORKSPACE_DIR / "BeyondTableCfg" / "TableCfg"
DEFAULT_JSON_DATA_DIR = ENDFIELD_WORKSPACE_DIR / "BeyondMemoryPack" / "JsonData"

Builder = Callable[[RunContext], dict[str, Any]]
# 与 workflow ``maaend_game_data`` 处理器的顺序、文件名一致。
DATASETS: tuple[tuple[str, str, Builder], ...] = (
    ("delivery_jobs", "delivery_jobs.json", build_delivery_jobs),
    ("environment_monitoring", "environment_monitoring.json", build_environment_monitoring),
    ("sell_product", "sell_product.json", build_sell_product),
    ("delivery_destinations", "delivery_destinations.json", build_delivery_destinations),
    ("teleport_anchors", "teleport_anchors.json", build_teleport_anchors),
    ("collect_points", "collect_points.json", build_collect_points),
)


def should_skip(output_path: Path, data: dict[str, Any], force: bool) -> bool:
    if force or not output_path.is_file():
        return False
    try:
        current = json.loads(output_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    return current == data


def write_dataset(output_path: Path, data: dict[str, Any]) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(data, ensure_ascii=False, indent=4) + "\n",
        encoding="utf-8",
    )


def select_datasets(names: Sequence[str] | None) -> tuple[tuple[str, str, Builder], ...]:
    if not names:
        return DATASETS
    known = {name for name, _, _ in DATASETS}
    unknown = [name for name in names if name not in known]
    if unknown:
        raise ValueError(f"未知数据集 {', '.join(unknown)}；可选：{', '.join(sorted(known))}")
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset[0] in wanted)


def parse_arguments(args: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="本地生成六个精简数据文件到 data/raw/")
    parser.add_argument(
        "--table-cfg-dir",
        type=Path,
        default=DEFAULT_TABLE_CFG_DIR,
        help=f"TableCfg 目录（默认：{DEFAULT_TABLE_CFG_DIR}）",
    )
    parser.add_argument(
        "--json-data-dir",
        type=Path,
        default=DEFAULT_JSON_DATA_DIR,
        help=f"BeyondMemoryPack 的 JsonData 目录（默认：{DEFAULT_JSON_DATA_DIR}）",
    )
    parser.add_argument(
        "--out-dir",
        type=Path,
        default=RAW_DIR,
        help=f"输出目录（默认：{RAW_DIR}）",
    )
    parser.add_argument(
        "--only",
        nargs="+",
        default=None,
        metavar="数据集",
        help=f"只生成列出的数据集（默认全部：{' '.join(name for name, _, _ in DATASETS)}）",
    )
    parser.add_argument("--force", action="store_true", help="结果未变化也重写输出文件")
    parser.add_argument("--list", action="store_true", help="只列出数据集名（每行一个）后退出")
    return parser.parse_args(args)


def main(args: Sequence[str] | None = None) -> int:
    options = parse_arguments(args)
    if options.list:
        for name, file_name, _ in DATASETS:
            print(f"{name}\t{file_name}")
        return 0
    try:
        datasets = select_datasets(options.only)
        context = RunContext(options.table_cfg_dir, options.json_data_dir)
        for name, file_name, build in datasets:
            data = build(context)
            output_path = options.out_dir / file_name
            if should_skip(output_path, data, options.force):
                print(f"[{name}] 生成结果未变化，跳过写入：{output_path}")
                continue
            write_dataset(output_path, data)
            print(f"[{name}] 已生成：{output_path}")
    except (OSError, ValueError) as error:
        print(f"[generate] {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
