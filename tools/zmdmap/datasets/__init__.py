"""六份精简游戏数据的 ``build_*``。

| 模块 | 数据 |
| --- | --- |
| ``delivery_jobs`` | ``delivery_jobs.json`` |
| ``environment_monitoring`` | ``environment_monitoring.json`` |
| ``sell_product`` | ``sell_product.json`` |
| ``delivery_destinations`` | ``delivery_destinations.json`` |
| ``teleport_anchors`` | ``teleport_anchors.json`` |
| ``collect_points`` | ``collect_points.json`` |

每个 ``build_*`` 的入参都是 ``(table_cfg_dir, json_data_dir)``——只用到其中一个的也照这个签名收，
统一由 :data:`DATASETS` 注册、由 ``uv run zmdmap generate`` 按顺序调用。共用的 ``coords.py`` /
``levels.py`` 与读取本地数据的 ``game_data.py`` 放在 ``utils/``。
"""

from __future__ import annotations

from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

from .collect_points import build_collect_points
from .delivery_destinations import build_delivery_destinations
from .delivery_jobs import build_delivery_jobs
from .environment_monitoring import build_environment_monitoring
from .sell_product import build_sell_product
from .teleport_anchors import build_teleport_anchors

Builder = Callable[[Path, Path], dict[str, Any]]

#: 生成与投影共用的固定顺序与文件名。
DATASETS: tuple[tuple[str, str, Builder], ...] = (
    ("delivery_jobs", "delivery_jobs.json", build_delivery_jobs),
    ("environment_monitoring", "environment_monitoring.json", build_environment_monitoring),
    ("sell_product", "sell_product.json", build_sell_product),
    ("delivery_destinations", "delivery_destinations.json", build_delivery_destinations),
    ("teleport_anchors", "teleport_anchors.json", build_teleport_anchors),
    ("collect_points", "collect_points.json", build_collect_points),
)

DATASET_NAMES = tuple(name for name, _, _ in DATASETS)


def select_datasets(names: Sequence[str]) -> tuple[tuple[str, str, Builder], ...]:
    """按名字挑数据集；顺序始终跟随 :data:`DATASETS`。"""
    if not names:
        return DATASETS
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset[0] in wanted)
