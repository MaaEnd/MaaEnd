"""读取本地游戏数据，以及从读进来的记录里取值、建索引。

各份数据的 ``Dataset.build`` 只从本地游戏数据的表目录里读表格：

- TableCfg：``<workspace>/BeyondTableCfg/TableCfg``，一批 ``<表名>.json``，另有各语言文本表
  ``I18nTextTable_{CN,TC,EN,JP,KR}.json``；
- JsonData：``<workspace>/BeyondMemoryPack/JsonData``，数据在 ``GameplayConfig/<表名>.json``，关卡地图配置在
  ``UILevelMapLoadConfig/<关卡>.json``。

目录默认取与 MaaEnd 平级的兄弟仓库（见 :mod:`tools.zmdmap.utils.config`），可用 CLI 参数或环境变量
覆盖。表按 :mod:`tools.zmdmap.utils.tables` 里的结构体解码，``build`` 里直接写 ``entry.position.x``。

少数结构完全动态的地方（交互模板、``LevelData`` 里按组件名分的属性）仍按原始 JSON 读，用
:mod:`tools.zmdmap.utils.shapes` 里的 ``is_object`` / ``as_objects`` 一层层取值。

记录里常取的另两类东西也在这里：

- **世界坐标**：:func:`read_xyz` 从坐标记录取 x/y/z（y 是世界高度，原样透传给
  NAVMESH 终点）。投影参数（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）与底图像素属于 BaseNav pack，
  只有 MaaEnd 手里有，换算由入库步骤（:mod:`tools.zmdmap.utils.project`）完成。
- **关卡归属**：:func:`map_of_level` / :func:`build_level_index` / :func:`level_of` 走「ID 前缀 -> 关卡 ->
  底图」。地图标记的分组 key 与世界实体 ID，除以 10^8 就是 ``LevelBasicInfoTable`` 的 ``idNum``，再由
  关卡 ID 推出它挂在哪张底图上；``teleport_anchors`` 与 ``collect_points`` 共用这一套。

数据文件里不放字段说明，说明写在 ``datasets/`` 的模块与文档里。
"""

import json
import sys
from collections.abc import Mapping
from functools import cache
from pathlib import Path
from typing import Any, Protocol

from .config import I18N_TABLE_SUFFIXES
from .shapes import is_object


class LocalDataError(ValueError):
    """本地数据缺失或不是预期形状。"""


class _Logger:
    """把 ``logger.info`` / ``warning`` 这类调用打到标准输出/标准错误。"""

    def info(self, message: str) -> None:
        print(f"[maaend] {message}")

    def success(self, message: str) -> None:
        print(f"[maaend] {message}")

    def debug(self, message: str) -> None:
        print(f"[maaend] {message}")

    def warning(self, message: str) -> None:
        print(f"[maaend] {message}", file=sys.stderr)

    def error(self, message: str) -> None:
        print(f"[maaend] {message}", file=sys.stderr)


logger = _Logger()


def load_json_dict(file_path: str | Path) -> dict[str, Any]:
    """原样返回 JSON 对象。"""
    with open(file_path, encoding="utf-8") as handle:
        return json.load(handle)


def _read_json(path: Path, label: str) -> Any:
    if not path.is_file():
        raise LocalDataError(f"缺少 {label} 文件：{path}")
    try:
        return load_json_dict(path)
    except json.JSONDecodeError as error:
        raise LocalDataError(f"{label} 文件不是合法 JSON：{path}（{error}）") from error


@cache
def _text_table(table_cfg_dir: Path, lang: str) -> dict[str, Any]:
    suffix = I18N_TABLE_SUFFIXES.get(lang)
    if suffix is None:
        raise LocalDataError(f"MaaEnd 不读 {lang} 的文本表")
    return _read_json(table_cfg_dir / f"I18nTextTable_{suffix}.json", f"文本表 {suffix}")


def localized_text(table_cfg_dir: Path, text_id: int, lang: str) -> str:
    """按语言取 ``I18nTextTable_<后缀>.json`` 里的文本；``text_id`` 为 0 时是空串。"""
    if text_id == 0:
        return ""
    text = _text_table(table_cfg_dir, lang).get(str(text_id), "")
    if not text:
        logger.debug(f"No text found for ID {text_id} in {lang}")
    return text if isinstance(text, str) else ""


def _text_ref(ref: Any) -> tuple[int, str]:
    """文本引用的 ``(id, 内联文本)``。

    表格行现在是 :class:`tools.zmdmap.utils.tables.TextRef`（带属性的结构），而少数读原始 JSON 的
    地方还是 dict，两种都认；不是这个形状一律当「没有」。
    """
    if is_object(ref):
        return as_int(ref.get("id")), as_text(ref.get("text"))
    return as_int(getattr(ref, "id", 0)), as_text(getattr(ref, "text", None))


def text_id_of(ref: Any) -> int:
    """``{"id": N, "text": ...}`` 里的文本 id；不是这个形状就是 0。"""
    return _text_ref(ref)[0]


def inline_text_of(ref: Any) -> str:
    """文本引用里内联的那份文本（没有就是空串）。"""
    return _text_ref(ref)[1]


def as_text(value: Any) -> str:
    """表里的 id/名字字段当字符串用；不是字符串就是空串。"""
    return value if isinstance(value, str) else ""


def as_int(value: Any, default: int = 0) -> int:
    """表里的整数字段当 int 用；缺失、不是整数或是布尔值时用 ``default``。"""
    if isinstance(value, bool) or not isinstance(value, int):
        return default
    return value


#: 内联文本算哪一种语言：表里自带的那份 ``text`` 是游戏里的中文原文。
INLINE_TEXT_SUFFIX = "CN"


def texts_of(table_cfg_dir: Path, text_id: int, inline: str = "") -> dict[str, str]:
    """多语言文本：key 是语言后缀（CN/TC/...）。

    表里自带 ``text`` 时把它当中文，其余语言照 ``text_id`` 查表 —— 只有查不到才退回这份中文。以前是直接
    把中文铺满五种语言，于是英文/日文/韩文的名字都会变成中文，而产物里看不出来。
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


def ref_texts(table_cfg_dir: Path, ref: Any) -> dict[str, str]:
    """表里 ``{"id", "text"}`` 这类文本引用的多语言文本。"""
    return texts_of(table_cfg_dir, text_id_of(ref), inline=inline_text_of(ref))


def ref_text(table_cfg_dir: Path, ref: Any, lang: str = "zh-CN") -> str:
    """表里文本引用的某一语言文本。"""
    return inline_text_of(ref) or localized_text(table_cfg_dir, text_id_of(ref), lang)




# ── 世界坐标 ────────────────────────────────────────────────────────────────


class CoordinateError(ValueError):
    """坐标记录缺字段或不是数值。"""


def _axis_value(pos: Any, axis: str) -> Any:
    """坐标记录的某个轴：表格结构体走属性，原始 dict 走键。"""
    if is_object(pos):
        return pos.get(axis)
    return getattr(pos, axis, None)


def _read_axes(pos: Any, axes: tuple[str, ...], label: str) -> tuple[float, ...]:
    if not is_object(pos) and not hasattr(pos, "x"):
        raise CoordinateError(f"{label} 不是坐标记录")
    values: list[float] = []
    for axis in axes:
        number = _axis_value(pos, axis)
        if not isinstance(number, (int, float)) or isinstance(number, bool):
            raise CoordinateError(f"{label} 缺少或不是数值的 {axis}")
        values.append(float(number))
    return tuple(values)


def read_xyz(pos: Any, label: str) -> tuple[float, float, float]:
    """从坐标记录取 (x, y, z)；y 是世界高度，原样透传给 NAVMESH 终点。"""
    x, y, z = _read_axes(pos, ("x", "y", "z"), label)
    return x, y, z


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
