"""MaaEnd 侧的数据读取层。

``data/scripts`` 下与 endfield-workflow 的 ``src/beyond_map/processors/maaend/`` 同名的
文件是那边 ``build_*`` 的逐字副本（只差 import 行）：流水线里那些函数从 ``RunContext``
取数据，这里用同样的接口把本地兄弟仓库读进来：

- TableCfg：``../BeyondTableCfg/TableCfg``（``ctx.table_cfg``）；
- JsonData：``../BeyondMemoryPack/JsonData``（``ctx.json_data``，GameplayConfig 按属性
  名读，``base_path`` 是 JsonData 根目录，LevelData 在它下面）；
- 五语言文本：TableCfg 里的 ``I18nTextTable_{CN,TC,EN,JP,KR}.json``（``ctx.i18n``）。

只有这个文件是 MaaEnd 专有的：那边的上下文来自流水线缓存，这边直接读盘。
"""

from __future__ import annotations

import json
import sys
from enum import StrEnum
from pathlib import Path
from typing import Any


class LocalDataError(ValueError):
    """本地数据缺失或不是预期形状。"""


class Text(str):
    """字符串：与 ``str`` 完全一样，另外支持枚举那样的 ``.value``。

    workflow 侧的模型会把 ``entityType`` 之类字段转成枚举，取值时写的是
    ``entry.entityType.value``；本地读到的就是普通字符串，用这个替身让两份代码一致。
    """

    @property
    def value(self) -> str:
        return str(self)


class Record(dict[str, Any]):
    """属性可访问的 JSON 记录：``record.field`` 等价于 ``record["field"]``。"""

    def __getattr__(self, name: str) -> Any:
        try:
            return wrap(self[name])
        except KeyError:
            raise AttributeError(name) from None


def wrap(value: Any) -> Any:
    """把读出来的 JSON 递归换成属性可访问的 ``Record`` / ``Text``。"""
    if isinstance(value, dict):
        return Record({key: wrap(item) for key, item in value.items()})
    if isinstance(value, list):
        return [wrap(item) for item in value]
    if isinstance(value, str):
        return Text(value)
    return value


class _Logger:
    """workflow 侧用 loguru；这里把同样的调用打到标准输出/标准错误。"""

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


class LanguageCode(StrEnum):
    """与 workflow ``models/common.py`` 的枚举同名，值也一样。"""

    PT_BR = "pt-BR"
    ZH_CN = "zh-CN"
    DE_DE = "de-DE"
    EN_US = "en-US"
    FR_FR = "fr-FR"
    ID_ID = "id-ID"
    IT_IT = "it-IT"
    JA_JP = "ja-JP"
    KO_KR = "ko-KR"
    ES_MX = "es-MX"
    RU_RU = "ru-RU"
    ZH_TW = "zh-TW"
    TH_TH = "th-TH"
    VI_VN = "vi-VN"


class DomainDepotTargetType(StrEnum):
    """与 workflow ``models/delivery.py`` 的枚举同名，值也一样。"""

    NONE = "None"
    NpcProxy = "NpcProxy"
    RecycleBin = "RecycleBin"


# 这几个名字在 build_* 里只出现在类型标注上（数据本身是 Record）。
I18nText = Record
MapMarkBasicData = Record
RecycleBinData = Record
WorldEntityRegistryData = Record
LevelBasicInfoEntry = Record

# LanguageCode -> I18nTextTable_<后缀>.json。
I18N_TABLE_SUFFIXES = {
    LanguageCode.ZH_CN: "CN",
    LanguageCode.ZH_TW: "TC",
    LanguageCode.EN_US: "EN",
    LanguageCode.JA_JP: "JP",
    LanguageCode.KO_KR: "KR",
}


def load_json_dict(file_path: str | Path) -> dict[str, Any]:
    """与 workflow ``utils.load_json_dict`` 同义：原样返回 JSON 对象。"""
    with open(file_path, encoding="utf-8") as handle:
        return json.load(handle)


def _read_json(path: Path, label: str) -> Any:
    if not path.is_file():
        raise LocalDataError(f"缺少 {label} 文件：{path}")
    try:
        return load_json_dict(path)
    except json.JSONDecodeError as error:
        raise LocalDataError(f"{label} 文件不是合法 JSON：{path}（{error}）") from error


class _LazyDir:
    """按属性名读目录下的 ``<Name>.json``，对应 workflow 的 ``ctx.table_cfg``。"""

    def __init__(self, path: Path, label: str) -> None:
        self._path = path
        self._label = label
        self._cache: dict[str, Record] = {}

    def __getattr__(self, name: str) -> Record:
        if name.startswith("_"):
            raise AttributeError(name)
        cached = self._cache.get(name)
        if cached is None:
            cached = wrap(_read_json(self._path / f"{name}.json", self._label))
            if not isinstance(cached, Record):
                raise LocalDataError(f"{self._label}/{name}.json 不是 JSON 对象")
            self._cache[name] = cached
        return cached


class _JsonData:
    """对应 workflow 的 ``ctx.json_data``：``base_path`` + GameplayConfig 里的文件。"""

    def __init__(self, base_path: Path) -> None:
        self.base_path = base_path
        self._gameplay_config = _LazyDir(base_path / "GameplayConfig", "GameplayConfig")

    def __getattr__(self, name: str) -> Record:
        if name.startswith("_"):
            raise AttributeError(name)
        data = getattr(self._gameplay_config, name)
        if name == "WorldEntityRegistry":
            _fill_world_entity_defaults(data)
        return data


# workflow 的 ``WorldEntityBriefInfo`` 给这几个字段留了默认值：本地 JSON 里有几十条
# 实体没有 ``detailId``，读进来时补上，免得两份代码走岔。
WORLD_ENTITY_BRIEF_DEFAULTS = {"detailId": "", "position": {}}


def _fill_world_entity_defaults(data: Record) -> None:
    brief_infos = data.get("worldEntityBriefInfos")
    if not isinstance(brief_infos, dict):
        return
    for entity in brief_infos.values():
        if isinstance(entity, dict):
            for field, default in WORLD_ENTITY_BRIEF_DEFAULTS.items():
                entity.setdefault(field, wrap(default))


class _I18n:
    """对应 workflow 的 ``ctx.i18n``：按语言取 ``I18nTextTable_<后缀>.json`` 里的文本。"""

    def __init__(self, table_cfg_dir: Path) -> None:
        self._table_cfg_dir = table_cfg_dir
        self._tables: dict[LanguageCode, dict[str, Any]] = {}

    def _table(self, lang_code: LanguageCode) -> dict[str, Any]:
        table = self._tables.get(lang_code)
        if table is None:
            suffix = I18N_TABLE_SUFFIXES.get(lang_code)
            if suffix is None:
                raise LocalDataError(f"MaaEnd 不读 {lang_code} 的文本表")
            table = _read_json(
                self._table_cfg_dir / f"I18nTextTable_{suffix}.json",
                f"文本表 {suffix}",
            )
            self._tables[lang_code] = table
        return table

    def get_localized_text(self, text_id: int, lang_code: LanguageCode) -> str:
        if text_id == 0:
            return ""
        text = self._table(lang_code).get(str(text_id), "")
        if not text:
            logger.debug(f"No text found for ID {text_id} in {lang_code}")
        return text if isinstance(text, str) else ""


class RunContext:
    """workflow ``RunContext`` 的本地替身：``table_cfg`` / ``json_data`` / ``i18n``。"""

    def __init__(self, table_cfg_dir: Path, json_data_dir: Path) -> None:
        self.table_cfg = _LazyDir(table_cfg_dir, "TableCfg")
        self.json_data = _JsonData(json_data_dir)
        self.i18n = _I18n(table_cfg_dir)
