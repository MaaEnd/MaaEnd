"""读取本地游戏数据，以及从读进来的记录里取值、建索引。

各份数据的 ``Dataset.build`` 只从本地游戏数据的表目录取数据：

- TableCfg：``<workspace>/BeyondTableCfg/TableCfg``，一批 ``<表名>.json``，另有各语言文本表
  ``I18nTextTable_{CN,TC,EN,JP,KR}.json``；
- JsonData：``<workspace>/BeyondMemoryPack/JsonData``，数据在 ``GameplayConfig/<表名>.json``，关卡地图配置在
  ``UILevelMapLoadConfig/<关卡>.json``。

目录默认取与 MaaEnd 平级的兄弟仓库（见 :mod:`tools.zmdmap.utils.config`），可用 CLI 参数或环境变量
覆盖。读出来的 JSON 换成属性可访问的 ``Record``，``build`` 里就能直接写 ``entry.position.x``。

记录里常取的另两类东西也在这里：

- **世界坐标**：:func:`read_xz` / :func:`read_xyz` 从坐标记录取 x/z 或 x/y/z（y 是世界高度，原样透传给
  NAVMESH 终点）。投影参数（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）与底图像素属于 BaseNav pack，
  只有 MaaEnd 手里有，换算由入库步骤（:mod:`tools.zmdmap.utils.project`）完成。
- **关卡归属**：:func:`map_of_level` / :func:`build_level_index` / :func:`level_of` 走「ID 前缀 -> 关卡 ->
  底图」。地图标记的分组 key 与世界实体 ID，除以 10^8 就是 ``LevelBasicInfoTable`` 的 ``idNum``，再由
  关卡 ID 推出它挂在哪张底图上；``teleport_anchors`` 与 ``collect_points`` 共用这一套。

数据文件里不放字段说明，说明写在 ``datasets/`` 的模块与文档里。
"""

import json
import sys
from functools import cache
from pathlib import Path
from typing import Any, cast

from .config import I18N_TABLE_SUFFIXES


class Record(dict[str, Any]):
    """属性可访问的 JSON 记录：``record.field`` 等价于 ``record["field"]``。"""

    def __getattr__(self, name: str) -> Any:
        try:
            return wrap(self[name])
        except KeyError:
            raise AttributeError(name) from None


def wrap(value: Any) -> Any:
    """把读出来的 JSON 递归换成属性可访问的 ``Record``。"""
    if isinstance(value, dict):
        return Record({key: wrap(item) for key, item in value.items()})
    if isinstance(value, list):
        return [wrap(item) for item in value]
    return value


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


def _as_record(data: Any, label: str) -> Record:
    if not isinstance(data, Record):
        raise LocalDataError(f"{label} 不是 JSON 对象")
    return data


@cache
def read_table_cfg(table_cfg_dir: Path, name: str) -> Record:
    """``<table_cfg_dir>/<name>.json``。"""
    return _as_record(wrap(_read_json(table_cfg_dir / f"{name}.json", "TableCfg")), "TableCfg")


@cache
def read_gameplay_config(json_data_dir: Path, name: str) -> Record:
    """``<json_data_dir>/GameplayConfig/<name>.json``。"""
    data = _as_record(
        wrap(_read_json(json_data_dir / "GameplayConfig" / f"{name}.json", "GameplayConfig")),
        "GameplayConfig",
    )
    if name == "WorldEntityRegistry":
        _fill_world_entity_defaults(data)
    return data


@cache
def read_level_map_config(json_data_dir: Path, level_id: str) -> Record:
    """``<json_data_dir>/UILevelMapLoadConfig/<level_id>.json``。"""
    return _as_record(
        wrap(
            _read_json(
                json_data_dir / "UILevelMapLoadConfig" / f"{level_id}.json", "UILevelMapLoadConfig"
            )
        ),
        "UILevelMapLoadConfig",
    )


# ``WorldEntityRegistry`` 里有几十条实体没有 ``detailId``：读进来时补上默认值，
# 免得各份数据的 ``build`` 里取值时分情况。
WORLD_ENTITY_BRIEF_DEFAULTS = {"detailId": "", "position": {}}


def _fill_world_entity_defaults(data: Record) -> None:
    brief_infos = data.get("worldEntityBriefInfos")
    if not isinstance(brief_infos, dict):
        return
    for entity in brief_infos.values():
        if isinstance(entity, dict):
            for field, default in WORLD_ENTITY_BRIEF_DEFAULTS.items():
                entity.setdefault(field, wrap(default))


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


def text_id_of(ref: Any) -> int:
    """``{"id": N, "text": ...}`` 里的文本 id；不是这个形状就是 0。"""
    if isinstance(ref, dict):
        value = ref.get("id")
        if isinstance(value, int):
            return value
    return 0


def as_text(value: Any) -> str:
    """表里的 id/名字字段当字符串用；不是字符串就是空串。"""
    return value if isinstance(value, str) else ""


def texts_of(table_cfg_dir: Path, text_id: int, inline: str = "") -> dict[str, str]:
    """多语言文本：内联 ``text`` 优先，否则查该语言的文本表；key 是语言后缀（CN/TC/...）。"""
    if inline:
        return dict.fromkeys(I18N_TABLE_SUFFIXES.values(), inline)
    return {
        suffix: localized_text(table_cfg_dir, text_id, lang)
        for lang, suffix in I18N_TABLE_SUFFIXES.items()
    }


def ref_texts(table_cfg_dir: Path, ref: Any) -> dict[str, str]:
    """表里 ``{"id", "text"}`` 这类文本引用的多语言文本。"""
    return texts_of(table_cfg_dir, text_id_of(ref), inline=_inline_text(ref))


def ref_text(table_cfg_dir: Path, ref: Any, lang: str = "zh-CN") -> str:
    """表里文本引用的某一语言文本。"""
    return _inline_text(ref) or localized_text(table_cfg_dir, text_id_of(ref), lang)


def _inline_text(ref: Any) -> str:
    """文本引用里直接带的那份文本（没有就是空串）。"""
    inline = ref.get("text") if isinstance(ref, dict) else ""
    return inline if isinstance(inline, str) else ""


# ── 世界坐标 ────────────────────────────────────────────────────────────────


class CoordinateError(ValueError):
    """坐标记录缺字段或不是数值。"""


def _read_axes(pos: Any, axes: tuple[str, ...], label: str) -> tuple[float, ...]:
    if not isinstance(pos, dict):
        raise CoordinateError(f"{label} 不是坐标记录")
    record = cast("dict[str, Any]", pos)
    values: list[float] = []
    for axis in axes:
        number = record.get(axis)
        if not isinstance(number, (int, float)) or isinstance(number, bool):
            raise CoordinateError(f"{label} 缺少或不是数值的 {axis}")
        values.append(float(number))
    return tuple(values)


def read_xz(pos: Any, label: str) -> tuple[float, float]:
    """从坐标记录取 (x, z)；缺字段时报出是哪条数据。"""
    x, z = _read_axes(pos, ("x", "z"), label)
    return x, z


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


def map_of_level(level_id: str) -> str:
    """关卡 ID -> 底图 key。"""
    head = level_id.split("_")[0]
    return level_id if head == INDIE_LEVEL_HEAD else head


def build_level_index(level_basic_info: dict[str, Record]) -> dict[int, str]:
    """``idNum -> levelId``。"""
    levels: dict[int, str] = {}
    for level_id in sorted(level_basic_info):
        id_num = level_basic_info[level_id].idNum
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
