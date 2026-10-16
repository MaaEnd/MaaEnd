"""精简数据的基类。

``datasets/`` 下**一个模块就是一份数据**：模块名既是数据集名，也是产物文件名
（``datasets/<模块名>.py`` 生成 ``<模块名>.json``）。每个数据集模块定义一个 :class:`Dataset`
子类并实现 :meth:`Dataset.build`，入库方式（要不要投影）也由这个子类说明；:mod:`tools.zmdmap.datasets`
扫包把它们收成 ``DATASETS``，没有需要手工维护的登记表。以 ``_`` 开头的模块不算数据集。

除了登记与入库声明，基类还给各份数据提供共用的手法 —— 五语文本（:meth:`Dataset.localized_names`）与
关卡索引（:meth:`Dataset.level_index`），以及组装错误 :class:`DatasetError`；取数逻辑留在各自模块里。
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any, ClassVar

from ..constants import MAAEND_LOCALES
from ..utils.game_data import localized_text, read_gameplay_config
from ..utils.levels import build_level_index

#: 数据集名（= 模块名）-> :class:`Dataset` 子类；子类定义时自动登记。
REGISTRY: dict[str, type[Dataset]] = {}


class DatasetError(ValueError):
    """数据组装错误：表里的引用对不上、缺文本之类的。

    cli 会拿数据集名当前缀（``[collect_points] …``）：各份数据统一抛它，不各建一个错误类。
    """


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

    @staticmethod
    def localized_names(ref: Any, table_cfg_dir: Path, label: str) -> dict[str, str]:
        """表里的文本引用（``{"id": …, "text": …}``）-> 五语字典（locale -> 文本）。

        文本自带（``text``）就不用查表；否则按 ``id`` 逐语查 ``I18nTextTable_*.json``。缺失一律报错：
        管线按 locale 取名字，少一种语言是任务侧的隐形失败，不如在这里挡下来。
        """
        inline = ref.get("text") if isinstance(ref, dict) else None
        if inline:
            return {locale: inline for locale, _ in MAAEND_LOCALES}

        text_id = ref.get("id") if isinstance(ref, dict) else None
        if not text_id:
            raise DatasetError(f"{label} 缺少 i18n id")

        names: dict[str, str] = {}
        for locale, lang_code in MAAEND_LOCALES:
            text = localized_text(table_cfg_dir, text_id, lang_code)
            if not text:
                raise DatasetError(f"{label} 在 {locale} 中缺少文本 {text_id}")
            names[locale] = text
        return names

    @staticmethod
    def level_index(json_data_dir: Path) -> dict[int, str]:
        """读 ``LevelBasicInfoTable`` 建 ``idNum -> levelId``（ID 前缀 → 关卡 → 底图的第一步）。"""
        return build_level_index(read_gameplay_config(json_data_dir, "LevelBasicInfoTable"))
