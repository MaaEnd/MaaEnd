"""精简游戏数据的数据集（一个模块一份数据）。

``datasets/<模块名>.py`` 生成 ``<模块名>.json``：**模块名 = 数据集名 = 产物文件名**。每份数据在模块里
定义一个 :class:`~tools.zmdmap.datasets.base.Dataset` 子类，用 ``needs_projection`` 说明入库时要不要
投影；本模块扫 ``datasets/`` 把子类收成 :data:`DATASETS`（需要投影的在前，其余按名字排），没有需要手工
维护的登记表。

| 模块 | 数据 | 入库方式 |
| --- | --- | --- |
| ``collect_points`` | ``collect_points.json`` | 投影 |
| ``delivery_destinations`` | ``delivery_destinations.json`` | 投影 |
| ``teleport_anchors`` | ``teleport_anchors.json`` | 投影 |
| ``delivery_jobs`` | ``delivery_jobs.json`` | 复制 |
| ``energy_point_gems`` | ``energy_point_gems.json`` | 复制 |
| ``environment_monitoring`` | ``environment_monitoring.json`` | 复制 |
| ``item_mini_table`` | ``item_mini_table.json`` | 复制 |
| ``sell_product`` | ``sell_product.json`` | 复制 |
| ``weapons`` | ``weapons.json`` | 复制 |

每份数据先出原始形态（只带游戏世界坐标）进 ``raw/``，再由入库步骤进 ``data/``：需要投影的按 BaseNav 的
zone 表投影，其余原样复制。要加一份新数据，就在 ``datasets/`` 下加一个同名模块、定义 ``Dataset`` 子类并
实现 ``build``。读取本地数据、记录取值与关卡索引都在 ``utils/game_data.py``，
各数据集都用的手法（五语文本 ``localized_names``、关卡索引 ``level_index``）与组装错误 ``DatasetError``
在 ``base.py`` 的 ``Dataset`` 上。
"""

from __future__ import annotations

import importlib
import pkgutil
from collections.abc import Iterable, Sequence

from .base import REGISTRY, Dataset

#: 不是数据集的模块：基类所在模块（本包内以 ``_`` 开头的模块同样跳过）。
_BASE_MODULE = "base"


def _discover() -> tuple[type[Dataset], ...]:
    """导入 ``datasets/`` 下每个数据集模块，返回它们登记的 :class:`Dataset` 子类。"""
    modules = tuple(
        module.name
        for module in pkgutil.iter_modules(__path__)
        if module.name != _BASE_MODULE and not module.name.startswith("_")
    )
    for name in modules:
        importlib.import_module(f"{__name__}.{name}")

    missing = [name for name in modules if name not in REGISTRY]
    if missing:
        raise RuntimeError(
            f"datasets/ 下这些模块没有 Dataset 子类：{'、'.join(missing)}"
            "（一个模块就是一份数据，辅助模块请用 _ 开头命名）"
        )
    # 需要投影的在前，其余按名字排：生成顺序固定，日志好对照。
    return tuple(
        sorted(REGISTRY.values(), key=lambda dataset: (not dataset.needs_projection, dataset.name))
    )


#: 全部数据集（顺序即生成顺序）。
DATASETS: tuple[type[Dataset], ...] = _discover()
#: 数据集名（``--only`` 的取值），顺序同 :data:`DATASETS`。
DATASET_NAMES: tuple[str, ...] = tuple(dataset.name for dataset in DATASETS)

#: 产物文件名 -> 数据集。
_BY_FILE: dict[str, type[Dataset]] = {dataset.file_name: dataset for dataset in DATASETS}

#: 入库要投影 / 原样复制的数据集文件名。
PROJECT_FILES: tuple[str, ...] = tuple(
    dataset.file_name for dataset in DATASETS if dataset.needs_projection
)
COPY_FILES: tuple[str, ...] = tuple(
    dataset.file_name for dataset in DATASETS if not dataset.needs_projection
)


def select_datasets(names: Sequence[str]) -> tuple[type[Dataset], ...]:
    """按名字挑数据集；顺序始终跟随 :data:`DATASETS`。"""
    if not names:
        return DATASETS
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset.name in wanted)


def files_of(datasets: Iterable[type[Dataset]], *, projected: bool) -> tuple[str, ...]:
    """挑出需要（``projected=True``）或不需要投影的数据集文件名。"""
    return tuple(
        dataset.file_name for dataset in datasets if dataset.needs_projection == projected
    )


def dataset_of(file_name: str) -> type[Dataset] | None:
    """按产物文件名找数据集；不认识的（如上游刚发布的新文件）返回 ``None``。"""
    return _BY_FILE.get(file_name)
