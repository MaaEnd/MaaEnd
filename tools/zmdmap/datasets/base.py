"""精简数据的基类。

``datasets/`` 下**一个模块就是一份数据**：模块名既是数据集名，也是产物文件名
（``datasets/<模块名>.py`` 生成 ``<模块名>.json``）。每个数据集模块定义一个 :class:`Dataset`
子类并实现 :meth:`Dataset.build`，入库方式（要不要投影）也由这个子类说明；:mod:`tools.zmdmap.datasets`
扫包把它们收成 ``DATASETS``，没有需要手工维护的登记表。以 ``_`` 开头的模块不算数据集。
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any, ClassVar

#: 数据集名（= 模块名）-> :class:`Dataset` 子类；子类定义时自动登记。
REGISTRY: dict[str, type[Dataset]] = {}


class Dataset(ABC):
    """一份精简数据：生成原始形态 + 说明入库方式。"""

    #: 入库时是否需要投影：需要投影的按 BaseNav 的 zone 表把世界坐标换成底图像素，其余原样复制。
    needs_projection: ClassVar[bool] = False
    #: 需要投影时，条目数组在数据里的键（按投影顺序）。
    projection_containers: ClassVar[tuple[str, ...]] = ()

    #: 数据集名（= 模块名）与产物文件名，定义子类时自动填上。
    name: ClassVar[str]
    file_name: ClassVar[str]

    def __init_subclass__(cls, **kwargs: Any) -> None:
        super().__init_subclass__(**kwargs)
        if cls.needs_projection != bool(cls.projection_containers):
            raise TypeError(
                f"{cls.__name__}：needs_projection 与 projection_containers 必须一致"
                "（需要投影的列出条目数组所在的键，不需要投影的两者都留空）"
            )
        cls.name = cls.__module__.rsplit(".", 1)[-1]
        cls.file_name = f"{cls.name}.json"
        REGISTRY[cls.name] = cls

    @classmethod
    @abstractmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> Any:
        """从本地 TableCfg / JsonData 生成**原始形态**（只带游戏世界坐标）。

        两个输入目录只用得到一个的也照这个签名收。
        """
