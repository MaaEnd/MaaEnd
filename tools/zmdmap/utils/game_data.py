"""读取本地游戏数据，以及从读进来的记录里取值、建索引。

各份数据的 ``Dataset.build`` 只从本地游戏数据的表目录里读表格：

- TableCfg：``<workspace>/BeyondTableCfg/TableCfg``，一批 ``<表名>.json``，另有各语言文本表
  ``I18nTextTable_{CN,TC,EN,JP,KR}.json``；
- JsonData：``<workspace>/BeyondMemoryPack/JsonData``，数据在 ``GameplayConfig/<表名>.json``，关卡地图配置在
  ``UILevelMapLoadConfig/<关卡>.json``。

目录默认取与 MaaEnd 平级的兄弟仓库（见 :mod:`tools.zmdmap.utils.config`），可用 CLI 参数或环境变量
覆盖。表、关卡数据、交互模板都按 :mod:`tools.zmdmap.utils.tables` 里的结构体解码，``build`` 里直接写
``entry.position.x``、``prop.value.value_array[0].value_string``；没有「按原始 JSON 读」的旁路了。

记录里常取的另两类东西也在这里：

- **世界坐标**：:func:`read_xyz` 从坐标记录取 x/y/z（y 是世界高度，原样透传给
  NAVMESH 终点）。投影参数（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）与底图像素属于 BaseNav pack，
  只有 MaaEnd 手里有，换算由入库步骤（:mod:`tools.zmdmap.utils.project`）完成。
- **关卡归属**：:func:`map_of_level` / :func:`build_level_index` / :func:`level_of` 走「ID 前缀 -> 关卡 ->
  底图」。地图标记的分组 key 与世界实体 ID，除以 10^8 就是 ``LevelBasicInfoTable`` 的 ``idNum``，再由
  关卡 ID 推出它挂在哪张底图上；``teleport_anchors`` 与 ``collect_points`` 共用这一套。

数据文件里不放字段说明，说明写在 ``datasets/`` 的模块与文档里。
"""

from collections.abc import Mapping
from pathlib import Path
from typing import Protocol

from .config import I18N_TABLE_SUFFIXES
from .errors import LocalDataError
from .log import logger
from .tables import Position3, TextRef, load_table


def _text_table(table_cfg_dir: Path, lang: str) -> dict[str, str]:
    """``I18nTextTable_<后缀>.json``：``{文本 id: 该语言的文字}``。"""
    suffix = I18N_TABLE_SUFFIXES.get(lang)
    if suffix is None:
        raise LocalDataError(f"MaaEnd 不读 {lang} 的文本表")
    return load_table(table_cfg_dir, f"I18nTextTable_{suffix}", str)


def localized_text(table_cfg_dir: Path, text_id: int, lang: str) -> str:
    """按语言取 ``I18nTextTable_<后缀>.json`` 里的文本；``text_id`` 为 0 时是空串。"""
    if text_id == 0:
        return ""
    text = _text_table(table_cfg_dir, lang).get(str(text_id), "")
    if not text:
        logger.debug(f"文本 {text_id} 在 {lang} 里没有译文")
    return text


def text_id_of(ref: TextRef | None) -> int:
    """文本引用里的文本 id；没有引用就是 0。"""
    return ref.id if ref is not None else 0


def inline_text_of(ref: TextRef | None) -> str:
    """文本引用里内联的那份中文；多数时候没有（``None``），当空串。"""
    return (ref.text or "") if ref is not None else ""


#: 内联文本算哪一种语言：表里自带的那份 ``text`` 是游戏里的中文原文。
INLINE_TEXT_SUFFIX = "CN"


def texts_of(table_cfg_dir: Path, text_id: int, inline: str = "") -> dict[str, str]:
    """多语言文本：key 是语言后缀（CN/TC/...）。

    表里自带的 ``text`` 只是中文原文：``CN`` 用它，其余语言照 ``text_id`` 查表，查不到才退回这份中文。
    ``text_id`` 也查不到时，五种语言都是同一份中文。
    """
    if not inline:
        return {
            suffix: localized_text(table_cfg_dir, text_id, lang)
            for lang, suffix in I18N_TABLE_SUFFIXES.items()
        }
    return {
        suffix: inline
        if suffix == INLINE_TEXT_SUFFIX
        else (localized_text(table_cfg_dir, text_id, lang) or inline)
        for lang, suffix in I18N_TABLE_SUFFIXES.items()
    }


def ref_texts(table_cfg_dir: Path, ref: TextRef | None) -> dict[str, str]:
    """表里 ``{"id", "text"}`` 这类文本引用的多语言文本。"""
    return texts_of(table_cfg_dir, text_id_of(ref), inline=inline_text_of(ref))


def ref_text(table_cfg_dir: Path, ref: TextRef | None, lang: str = "zh-CN") -> str:
    """表里文本引用的某一语言文本。"""
    return inline_text_of(ref) or localized_text(table_cfg_dir, text_id_of(ref), lang)


# ── 世界坐标 ────────────────────────────────────────────────────────────────


def read_xyz(pos: Position3) -> tuple[float, float, float]:
    """从坐标结构里取 (x, y, z)；y 是世界高度，原样透传给 NAVMESH 终点。"""
    return pos.x, pos.y, pos.z


# ── 关卡归属 ────────────────────────────────────────────────────────────────

ID_LEVEL_FACTOR = 10**8
# indie_dgXXX 各自一张底图，其余关卡按 mapXX / base01 / dung01 归图。
INDIE_LEVEL_HEAD = "indie"


class LevelIndexError(ValueError):
    """关卡编号表有问题。"""


class HasIdNum(Protocol):
    """``LevelBasicInfoTable`` 的一行要有的东西：只是关卡编号 ``id_num``。"""

    @property
    def id_num(self) -> int: ...


def map_of_level(level_id: str) -> str:
    """关卡 ID -> 底图 key。"""
    head = level_id.split("_")[0]
    return level_id if head == INDIE_LEVEL_HEAD else head


def build_level_index(level_basic_info: Mapping[str, HasIdNum]) -> dict[int, str]:
    """``idNum -> levelId``。"""
    levels: dict[int, str] = {}
    for level_id in sorted(level_basic_info):
        id_num = level_basic_info[level_id].id_num
        if id_num in levels:
            raise LevelIndexError(f"idNum {id_num} 同时属于 {levels[id_num]} 和 {level_id}")
        levels[id_num] = level_id
    if not levels:
        raise LevelIndexError("LevelBasicInfoTable 里一个关卡都没有")
    return levels


def level_of(levels: dict[int, str], raw_id: str) -> str | None:
    """按 ID 前缀定位关卡；非数字 ID 或未登记的前缀都返回 None。"""
    if not raw_id.isdigit():
        return None
    return levels.get(int(raw_id) // ID_LEVEL_FACTOR)
