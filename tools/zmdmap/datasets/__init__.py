"""精简游戏数据的数据集（一个模块或一个文件夹一份数据）。

``datasets/<名字>.py`` 或 ``datasets/<名字>/`` 生成 ``<名字>.json``：**名字 = 数据集名 = 产物文件名**。
每份数据定义一个 :class:`~tools.zmdmap.datasets.base.Dataset` 子类，用 ``needs_projection`` 说明入库时
要不要投影；本模块扫 ``datasets/`` 把子类收成 :data:`DATASETS`（需要投影的在前，其余按名字排），没有需要
手工维护的登记表。一份数据要拆成好几个模块时用文件夹（如 ``region3d/``）：文件夹本身（``__init__.py``）
里定义子类，其余模块只是它的实现。

| 模块 / 文件夹 | 数据 | 入库方式 |
| --- | --- | --- |
| ``collect_points`` | ``collect_points.json`` | 投影 |
| ``delivery_destinations`` | ``delivery_destinations.json`` | 投影 |
| ``teleport_anchors`` | ``teleport_anchors.json`` | 投影 |
| ``delivery_jobs`` | ``delivery_jobs.json`` | 复制 |
| ``energy_point_gems`` | ``energy_point_gems.json`` | 复制 |
| ``environment_monitoring`` | ``environment_monitoring.json`` | 复制 |
| ``item_mini_table`` | ``item_mini_table.json`` | 复制 |
| ``operator_growth`` | ``operator_growth.json`` | 复制 |
| ``region3d`` | ``region3d.json`` | 复制 |
| ``sell_product`` | ``sell_product.json`` | 复制 |
| ``stock_staple`` | ``stock_staple.json`` | 复制 |
| ``weapons`` | ``weapons.json`` | 复制 |

每份数据先出原始形态（只带游戏世界坐标）进 ``raw/``，再由入库步骤进 ``data/``：需要投影的按 BaseNav 的
zone 表投影，其余原样复制。要加一份新数据，就在 ``datasets/`` 下加一个同名模块（模块多就加同名文件夹）、
定义 ``Dataset`` 子类并实现 ``build``：参数上写要用的输入服务（``table_cfg: TableCfg``、``lua: LuaScripts``
之类），``generate`` 按签名注入，没写的不会传（规则见 :mod:`tools.zmdmap.utils.injection`）。表、JsonData、
Lua 与解包资源的读取方法在 ``utils/inputs.py``，关卡索引在 ``JsonData.level_index()``，五语文本
``localized_names`` 与组装错误 ``DatasetError`` 在 ``base.py`` 的 ``Dataset`` 上。
"""

import importlib
import pkgutil
from collections.abc import Iterable, Sequence

from ..utils.injection import InjectionError, parse_injection
from .base import REGISTRY, Dataset

#: 不是数据集的模块：基类所在模块（本包内以 ``_`` 开头的模块同样跳过）。
_BASE_MODULE = "base"


def _dataset_in(entry: str) -> type[Dataset]:
    """一个模块或文件夹里必须恰好登记一个 :class:`Dataset` 子类；数据集名以入口名为准。

    文件夹数据集把子类写在文件夹的 ``__init__.py`` 里最省事；写在内部模块、由 ``__init__.py`` 导入也认：
    收集按模块名前缀 ``datasets.<入口名>.`` 匹配，然后把默认名字改成入口名（产物文件名跟着改）。
    """
    prefix = f"{__name__}.{entry}"
    found = [dataset for module, dataset in REGISTRY.items() if module == prefix or module.startswith(f"{prefix}.")]
    where = f"datasets/{entry}（模块或文件夹）"
    if not found:
        raise RuntimeError(
            f"{where} 里没有 Dataset 子类"
            "（一个模块或文件夹就是一份数据；辅助模块请用 _ 开头命名，文件夹的子类写在 __init__.py）"
        )
    if len(found) > 1:
        modules = "、".join(dataset.__module__ for dataset in found)
        raise RuntimeError(f"{where} 里有多个 Dataset 子类：{modules}（一份数据只能有一个）")

    dataset = found[0]
    try:
        parse_injection(dataset.build)
    except InjectionError as error:
        raise RuntimeError(f"{where} 的 build 签名不对：{error}") from error
    if dataset.name != entry:
        dataset.name = entry
        dataset.file_name = f"{entry}.json"
    return dataset


def _discover() -> tuple[type[Dataset], ...]:
    """导入 ``datasets/`` 下每个数据集（模块或文件夹），返回它们登记的 :class:`Dataset` 子类。"""
    entries = tuple(
        module.name
        for module in pkgutil.iter_modules(__path__)
        if module.name != _BASE_MODULE and not module.name.startswith("_")
    )
    for name in entries:
        importlib.import_module(f"{__name__}.{name}")

    # 需要投影的在前，其余按名字排：生成顺序固定，日志好对照。
    return tuple(
        sorted(
            (_dataset_in(name) for name in entries),
            key=lambda dataset: (not dataset.needs_projection, dataset.name),
        )
    )


#: 全部数据集（顺序即生成顺序）。
DATASETS: tuple[type[Dataset], ...] = _discover()
#: 数据集名（``--only`` 的取值），顺序同 :data:`DATASETS`。
DATASET_NAMES: tuple[str, ...] = tuple(dataset.name for dataset in DATASETS)

#: 产物文件名 -> 数据集。
_BY_FILE: dict[str, type[Dataset]] = {dataset.file_name: dataset for dataset in DATASETS}


def select_datasets(names: Sequence[str]) -> tuple[type[Dataset], ...]:
    """按名字挑数据集；顺序始终跟随 :data:`DATASETS`。"""
    if not names:
        return DATASETS
    wanted = set(names)
    return tuple(dataset for dataset in DATASETS if dataset.name in wanted)


def files_of(datasets: Iterable[type[Dataset]], *, projected: bool) -> tuple[str, ...]:
    """挑出需要（``projected=True``）或不需要投影的数据集文件名。"""
    return tuple(dataset.file_name for dataset in datasets if dataset.needs_projection == projected)


def dataset_of(file_name: str) -> type[Dataset] | None:
    """按产物文件名找数据集；不认识的（如上游刚发布的新文件）返回 ``None``。"""
    return _BY_FILE.get(file_name)
