"""精简游戏数据的 ``build_*``。

每份数据先出原始形态（只带游戏世界坐标）进 ``raw/``，再由入库步骤进 ``data/``：带坐标的按
BaseNav 的 zone 表投影，其余原样复制。

| 模块 | 数据 | 入库方式 |
| --- | --- | --- |
| ``delivery_destinations`` | ``delivery_destinations.json`` | 投影 |
| ``teleport_anchors`` | ``teleport_anchors.json`` | 投影 |
| ``collect_points`` | ``collect_points.json`` | 投影 |
| ``delivery_jobs`` | ``delivery_jobs.json`` | 复制 |
| ``environment_monitoring`` | ``environment_monitoring.json`` | 复制 |
| ``sell_product`` | ``sell_product.json`` | 复制 |
| ``essence`` | ``energy_point_gems.json`` | 复制 |
| ``weapons`` | ``weapons.json`` | 复制 |
| ``items`` | ``item_mini_table.json`` | 复制 |

每个 ``build_*`` 的入参都是 ``(table_cfg_dir, json_data_dir)``——只用到其中一个的也照这个签名收，
统一由 :data:`DATASETS` 注册、由 ``uv run zmdmap generate`` 按顺序调用。``materialize`` 只说明入库
方式（:data:`PROJECT` 投影 / :data:`COPY` 复制），生成阶段一律写同一个原始目录。共用的 ``coords.py`` /
``levels.py`` 与读取本地数据的 ``game_data.py`` 放在 ``utils/``。
"""

from __future__ import annotations

from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .collect_points import build_collect_points
from .delivery_destinations import build_delivery_destinations
from .delivery_jobs import build_delivery_jobs
from .environment_monitoring import build_environment_monitoring
from .essence import build_energy_points
from .items import build_items
from .sell_product import build_sell_product
from .teleport_anchors import build_teleport_anchors
from .weapons import build_weapons

Builder = Callable[[Path, Path], Any]

#: 入库方式：按 BaseNav 的 zone 表把世界坐标换成底图像素（只有带坐标的数据需要）。
PROJECT = "project"
#: 入库方式：不需要投影，原样复制。
COPY = "copy"


@dataclass(frozen=True)
class Dataset:
    """一份精简数据：生成函数、文件名与入库方式。"""

    name: str
    file_name: str
    build: Builder
    materialize: str = COPY


#: 生成顺序与文件名（需要投影的在前）。
DATASETS: tuple[Dataset, ...] = (
    Dataset("delivery_destinations", "delivery_destinations.json", build_delivery_destinations, PROJECT),
    Dataset("teleport_anchors", "teleport_anchors.json", build_teleport_anchors, PROJECT),
    Dataset("collect_points", "collect_points.json", build_collect_points, PROJECT),
    Dataset("delivery_jobs", "delivery_jobs.json", build_delivery_jobs),
    Dataset("environment_monitoring", "environment_monitoring.json", build_environment_monitoring),
    Dataset("sell_product", "sell_product.json", build_sell_product),
    Dataset("essence_points", "energy_point_gems.json", build_energy_points),
    Dataset("weapons", "weapons.json", build_weapons),
    Dataset("items", "item_mini_table.json", build_items),
)

DATASET_NAMES = tuple(dataset.name for dataset in DATASETS)


def select_datasets(names: Sequence[str]) -> tuple[Dataset, ...]:
    """按名字挑数据集；顺序始终跟随 :data:`DATASETS`。"""
    if not names:
        return DATASETS
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset.name in wanted)


def names_of(datasets: Iterable[Dataset], *, materialize: str) -> tuple[str, ...]:
    """挑出某种入库方式的数据集名。"""
    return tuple(dataset.name for dataset in datasets if dataset.materialize == materialize)


def files_of(datasets: Iterable[Dataset], *, materialize: str) -> tuple[str, ...]:
    """挑出某种入库方式的数据集文件名。"""
    return tuple(dataset.file_name for dataset in datasets if dataset.materialize == materialize)


#: 入库要投影的数据集名 / 文件名。
PROJECT_NAMES = names_of(DATASETS, materialize=PROJECT)
PROJECT_FILES: tuple[str, ...] = files_of(DATASETS, materialize=PROJECT)
#: 入库原样复制的数据集名 / 文件名。
COPY_NAMES = names_of(DATASETS, materialize=COPY)
COPY_FILES: tuple[str, ...] = files_of(DATASETS, materialize=COPY)
