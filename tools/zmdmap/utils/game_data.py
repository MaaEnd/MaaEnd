"""读取本地游戏数据。

各份数据的 ``Dataset.build`` 只从本地游戏数据的表目录取数据：

- TableCfg：``<workspace>/BeyondTableCfg/TableCfg``，一批 ``<表名>.json``，另有各语言文本表
  ``I18nTextTable_{CN,TC,EN,JP,KR}.json``；
- JsonData：``<workspace>/BeyondMemoryPack/JsonData``，数据在 ``GameplayConfig/<表名>.json``。

目录默认取与 MaaEnd 平级的兄弟仓库（见 :mod:`tools.zmdmap.paths`），可用 CLI 参数或环境变量
覆盖。读出来的 JSON 换成属性可访问的 ``Record``，``build`` 里就能直接写 ``entry.position.x``。
"""

from __future__ import annotations

import json
import sys
from functools import lru_cache
from pathlib import Path
from typing import Any

from ..constants import I18N_TABLE_SUFFIXES


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


@lru_cache(maxsize=None)
def read_table_cfg(table_cfg_dir: Path, name: str) -> Record:
    """``<table_cfg_dir>/<name>.json``。"""
    return _as_record(wrap(_read_json(table_cfg_dir / f"{name}.json", "TableCfg")), "TableCfg")


@lru_cache(maxsize=None)
def read_gameplay_config(json_data_dir: Path, name: str) -> Record:
    """``<json_data_dir>/GameplayConfig/<name>.json``。"""
    data = _as_record(
        wrap(_read_json(json_data_dir / "GameplayConfig" / f"{name}.json", "GameplayConfig")),
        "GameplayConfig",
    )
    if name == "WorldEntityRegistry":
        _fill_world_entity_defaults(data)
    return data


# ``WorldEntityRegistry`` 里有几十条实体没有 ``detailId``：读进来时补上默认值，
# 免得 ``build_*`` 里取值时分情况。
WORLD_ENTITY_BRIEF_DEFAULTS = {"detailId": "", "position": {}}


def _fill_world_entity_defaults(data: Record) -> None:
    brief_infos = data.get("worldEntityBriefInfos")
    if not isinstance(brief_infos, dict):
        return
    for entity in brief_infos.values():
        if isinstance(entity, dict):
            for field, default in WORLD_ENTITY_BRIEF_DEFAULTS.items():
                entity.setdefault(field, wrap(default))


@lru_cache(maxsize=None)
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
        return {suffix: inline for suffix in I18N_TABLE_SUFFIXES.values()}
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
