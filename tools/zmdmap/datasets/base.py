"""精简数据的基类。

``datasets/`` 下**一个模块或一个文件夹就是一份数据**：名字既是数据集名，也是产物文件名
（``datasets/<名字>.py`` 或 ``datasets/<名字>/`` 生成 ``<名字>.json``）。每个数据集定义一个
:class:`Dataset` 子类并实现 :meth:`Dataset.build`，入库方式（要不要投影）也由这个子类说明；
:mod:`tools.zmdmap.datasets` 扫包把它们收成 ``DATASETS``，没有需要手工维护的登记表。以 ``_`` 开头的模块
不算数据集。一份数据要拆成好几个模块时用文件夹（如 ``region3d/``），子类写在文件夹的 ``__init__.py`` 里
（或由它导入）。

除了登记与入库声明，基类还给各份数据提供共用的手法 —— 五语文本（:meth:`Dataset.localized_names`）与
组装错误 :class:`DatasetError`；读数据的逻辑留在各自模块里。

``build`` 不接收一个装满输入的容器：参数写成它需要的那几份输入服务（``table_cfg: TableCfg``、
``lua: LuaScripts`` 之类），``generate`` 解析签名后按参数名注入，没声明的一样都不会传进来（规则见
:mod:`tools.zmdmap.utils.injection`）。
"""

import inspect
import re
from collections.abc import Callable
from typing import Any, ClassVar

from ..utils.config import MAAEND_LOCALES
from ..utils.game_data import inline_text_of, text_id_of
from ..utils.inputs import TableCfg

#: 内联文本算哪一种语言：表里自带的那份 ``text`` 是游戏里的中文原文。
INLINE_LOCALE = "zh_cn"

#: 子类所在的模块名 -> :class:`Dataset` 子类；子类定义时自动登记。数据集名（= 产物文件名）由
#: :func:`tools.zmdmap.datasets._dataset_in` 按入口（模块 / 文件夹）名确定，所以文件夹数据集把子类写在
#: 内部模块里也能认出属于哪一份。
REGISTRY: dict[str, type[Dataset]] = {}

# 富文本标签（如 <@qu.key>…</>、日文注音 <p="…">…</p>）
RICH_TEXT_TAG = re.compile(r"<[^<>]*>")


class DatasetError(ValueError):
    """数据组装错误：表里的引用对不上、缺文本之类的。

    cli 会拿数据集名当前缀（``[collect_points] …``）：各份数据统一抛它，不各建一个错误类。
    """


class Dataset:
    """一份精简数据：生成原始形态 + 说明入库方式。"""

    #: 入库时是否需要投影：需要投影的按 BaseNav 的 zone 表把世界坐标换成底图像素，其余原样复制。
    needs_projection: ClassVar[bool] = False
    #: 需要投影时，条目数组在数据里的键（按投影顺序）。
    projection_containers: ClassVar[tuple[str, ...]] = ()

    #: 数据集名（= 模块名 / 文件夹名）与产物文件名，扫描时按入口名确定。
    name: ClassVar[str]
    file_name: ClassVar[str]

    #: 由每份数据实现：参数按 :mod:`tools.zmdmap.utils.injection` 的规则写成要用的输入服务。
    build: ClassVar[Callable[..., Any]]

    def __init_subclass__(cls, **kwargs: Any) -> None:
        super().__init_subclass__(**kwargs)
        if cls.needs_projection != bool(cls.projection_containers):
            raise TypeError(
                f"{cls.__name__}：needs_projection 与 projection_containers 必须一致"
                "（需要投影的列出条目数组所在的键，不需要投影的两者都留空）"
            )
        if not isinstance(inspect.getattr_static(cls, "build", None), classmethod):
            raise TypeError(f"{cls.__name__}：build 必须定义成 @classmethod（参数写要用的输入服务）")
        # 先按模块名的最后一段填一个默认名；文件夹数据集的入口名可能不是它，扫描时再改。
        cls.name = cls.__module__.rsplit(".", 1)[-1]
        cls.file_name = f"{cls.name}.json"
        REGISTRY[cls.__module__] = cls

    @staticmethod
    def localized_names(ref: Any, table_cfg: TableCfg, label: str) -> dict[str, str]:
        """表里的文本引用（``{"id": …, "text": …}``）-> 五语字典（locale -> 文本）。

        逐语查 ``I18nTextTable_*.json``；文本自带（``text``）时它只顶中文那一份，其余语言照样查表，查不到
        才退回它 —— 直接把中文铺满五种语言会让产物里的英/日/韩名字变成中文，而外面看不出来。缺失一律
        报错：管线按 locale 取名字，少一种语言是任务侧的隐形失败，不如在这里挡下来。
        """
        inline = inline_text_of(ref)
        text_id = text_id_of(ref)
        if not text_id and not inline:
            raise DatasetError(f"{label} 缺少 i18n id")

        names: dict[str, str] = {}
        for locale, lang_code in MAAEND_LOCALES:
            if locale == INLINE_LOCALE and inline:
                names[locale] = inline
                continue
            text = table_cfg.text(text_id, lang_code) if text_id else ""
            text = text or inline
            if not text:
                raise DatasetError(f"{label} 在 {locale} 中缺少文本 {text_id}")
            names[locale] = text
        return names

    @staticmethod
    def plain_names(names: dict[str, str], label: str) -> dict[str, str]:
        """剥掉每个语言的富文本标签，剥完后为空则报错。"""
        stripped: dict[str, str] = {}
        for locale, text in names.items():
            value = RICH_TEXT_TAG.sub("", text).strip()
            if not value:
                raise DatasetError(f"{label} 在 {locale} 中为空")
            stripped[locale] = value
        return stripped
