"""精简游戏数据的 ``build_*``。

入 pipeline 的六份（生成到 ``data/raw/``，再由投影把世界坐标换成底图像素）：

| 模块 | 数据 |
| --- | --- |
| ``delivery_jobs`` | ``delivery_jobs.json`` |
| ``environment_monitoring`` | ``environment_monitoring.json`` |
| ``sell_product`` | ``sell_product.json`` |
| ``delivery_destinations`` | ``delivery_destinations.json`` |
| ``teleport_anchors`` | ``teleport_anchors.json`` |
| ``collect_points`` | ``collect_points.json`` |

给具体工具用的（原样落到该工具的输入目录，不投影）：

| 模块 | 数据 | 消费者 |
| --- | --- | --- |
| ``essence`` | ``energy_point_gems.json`` | ``tools/essence_filter/`` |
| ``weapons`` | ``weapons.json`` | ``tools/essence_filter/`` 与 ``tools/icon_recognition/`` |

每个 ``build_*`` 的入参都是 ``(table_cfg_dir, json_data_dir)``——只用到其中一个的也照这个签名收，
统一由 :data:`DATASETS` 注册、由 ``uv run zmdmap generate`` 按顺序调用。``target`` 决定产物去哪：
:data:`PIPELINE` 落 ``data/raw/`` 并参与投影，其余落各自工具的目录、只当输入用。共用的
``coords.py`` / ``levels.py`` 与读取本地数据的 ``game_data.py`` 放在 ``utils/``。
"""

from __future__ import annotations

from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from ..paths import DEFAULT_OUT_DIR, ZMDMAP_DATA_DIR
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

#: 产物去向：落 ``data/raw/``，再投影进入库的 ``data/*.json``。
PIPELINE = "pipeline"
#: 产物去向：不需要投影，直接落 ``data/`` 给工具当输入。
PLAIN = "plain"

#: 每个去向的默认目录（可用 CLI 参数覆盖）。
TARGET_DIRS: dict[str, Path] = {
    PIPELINE: DEFAULT_OUT_DIR,
    PLAIN: ZMDMAP_DATA_DIR,
}


@dataclass(frozen=True)
class Dataset:
    """一份精简数据：生成函数、文件名与产物去向。"""

    name: str
    file_name: str
    build: Builder
    target: str = PIPELINE


#: 生成顺序与文件名（入 pipeline 的六份在前，工具用的在后）。
DATASETS: tuple[Dataset, ...] = (
    Dataset("delivery_jobs", "delivery_jobs.json", build_delivery_jobs),
    Dataset("environment_monitoring", "environment_monitoring.json", build_environment_monitoring),
    Dataset("sell_product", "sell_product.json", build_sell_product),
    Dataset("delivery_destinations", "delivery_destinations.json", build_delivery_destinations),
    Dataset("teleport_anchors", "teleport_anchors.json", build_teleport_anchors),
    Dataset("collect_points", "collect_points.json", build_collect_points),
    Dataset("essence_points", "energy_point_gems.json", build_energy_points, PLAIN),
    Dataset("weapons", "weapons.json", build_weapons, PLAIN),
    Dataset("items", "item_mini_table.json", build_items, PLAIN),
)

DATASET_NAMES = tuple(dataset.name for dataset in DATASETS)


def select_datasets(names: Sequence[str]) -> tuple[Dataset, ...]:
    """按名字挑数据集；顺序始终跟随 :data:`DATASETS`。"""
    if not names:
        return DATASETS
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset.name in wanted)


def names_of(datasets: Iterable[Dataset], *, target: str) -> tuple[str, ...]:
    """挑出某个去向的数据集名（投影与入库报告只看 pipeline 那几份）。"""
    return tuple(dataset.name for dataset in datasets if dataset.target == target)


#: 入 pipeline 的六份（``fetch`` 只管这几份的投影）；它们要下载、要投影。
PIPELINE_NAMES = names_of(DATASETS, target=PIPELINE)
#: 不需要投影、直接当工具输入的那几份。
PLAIN_NAMES = names_of(DATASETS, target=PLAIN)
#: 不需要投影的产物文件名；``fetch`` 把它们直接下到 ``data/``。
PLAIN_FILES: tuple[str, ...] = tuple(
    dataset.file_name for dataset in DATASETS if dataset.target == PLAIN
)
