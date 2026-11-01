"""生成时注入给数据集的输入服务。

数据集不拼文件系统路径，也不自己读 JSON：要表就从 :class:`TableCfg` 取，要关卡/交互数据从
:class:`JsonData` 取，要 Lua 或解包资源从 :class:`LuaScripts` / :class:`Assets` 取。默认实现直接读本地
解包目录（路径由命令行给出）；要换来源就给个同接口的对象，不必自己碰磁盘。

``Dataset.build`` 在参数上声明自己要哪几份（``table_cfg: TableCfg`` 之类），``generate`` 按签名注入；
规则见 :mod:`tools.zmdmap.utils.injection`，没声明的服务不会传进来。
"""

from collections.abc import Iterable, Iterator
from pathlib import Path
from typing import Any

from .datafile import read_json
from .errors import LocalDataError
from .game_data import build_level_index, localized_text, ref_text, ref_texts, texts_of
from .log import logger
from .tables import (
    InteractiveTemplate,
    LevelBasicInfoRow,
    LevelData,
    TextRef,
    load_config,
    load_interactive_data,
    load_level,
    load_level_data,
    load_table,
)

#: 解包导出的 Unity 资源后缀：新包把 ``x.prefab`` / ``x.anim`` 都导出成 ``x.json``。
ASSET_SUFFIXES = (".prefab", ".anim")


class TableCfg:
    """``TableCfg`` 目录：一批 ``<表名>.json`` 与各语言文本表 ``I18nTextTable_<后缀>.json``。"""

    def __init__(self, root: Path) -> None:
        self._root = root

    def table[T](self, name: str, model: type[T]) -> dict[str, T]:
        """按结构解码一张表：``{行 id: 行}``。"""
        return load_table(self._root, name, model)

    def raw_table(self, name: str) -> Any:
        """不按结构解码、原样读一张表（地区总览只用到少数几个字段）。"""
        path = self._root / f"{name}.json"
        if not path.is_file():
            raise LocalDataError(f"缺少 TableCfg 文件：{path}")
        return read_json(path)

    def text(self, text_id: int, lang: str) -> str:
        """某一种语言的文本；没有就是空串。"""
        return localized_text(self._root, text_id, lang)

    def texts(self, text_id: int, inline: str = "") -> dict[str, str]:
        """五语文本（``zh_cn`` 等 locale -> 文本）。"""
        return texts_of(self._root, text_id, inline)

    def ref_texts(self, ref: TextRef | None) -> dict[str, str]:
        """表里 ``{"id", "text"}`` 这类文本引用的五语文本。"""
        return ref_texts(self._root, ref)

    def ref_text(self, ref: TextRef | None, lang: str = "zh-CN") -> str:
        """文本引用在某一语言的文本；没有内联文本时查表。"""
        return ref_text(self._root, ref, lang)


class JsonData:
    """``JsonData`` 目录：关卡数据在 ``GameplayConfig``，交互模板在 ``Interactive``。"""

    def __init__(self, root: Path) -> None:
        self._root = root

    def config[T](self, name: str, model: type[T]) -> T:
        """``GameplayConfig/<name>.json`` 类型化解码。"""
        return load_config(self._root, name, model)

    def level[T](self, level_id: str, model: type[T]) -> T:
        """``UILevelMapLoadConfig/<level_id>.json`` 类型化解码。"""
        return load_level(self._root, level_id, model)

    def interactive_templates(self, prefix: str = "") -> Iterator[InteractiveTemplate]:
        """``Interactive/InteractiveData/data_<prefix>*.json`` 里的交互模板。"""
        directory = self._root / "Interactive" / "InteractiveData"
        for path in sorted(directory.glob(f"data_{prefix}*.json")):
            yield load_interactive_data(path)

    def level_index(self) -> dict[int, str]:
        """读 ``LevelBasicInfoTable`` 建 ``idNum -> levelId``（ID 前缀 → 关卡 → 底图）。"""
        return build_level_index(load_config(self._root, "LevelBasicInfoTable", dict[str, LevelBasicInfoRow]))

    def level_data(self, level_id: str) -> list[LevelData]:
        """``LevelData/<level_id>/*.json`` 的关卡数据，按文件名排序；目录不存在时为空。

        单个文件读不出来只记一条警告、跳过它：一份坏数据不该拖垮整份产物。
        """
        directory = self._root / "LevelData" / level_id
        levels: list[LevelData] = []
        for path in sorted(directory.glob("*.json")):
            try:
                levels.append(load_level_data(path))
            except (OSError, ValueError) as error:
                logger.warning(f"读取关卡文件失败 {path}: {error}")
        return levels


class LuaScripts:
    """解包出的 ``LuaScripts`` 目录：只按逻辑路径读源码，不执行。"""

    def __init__(self, root: Path) -> None:
        self._root = root

    def script(self, logical_path: str) -> str:
        """``Const/MapConst.lua`` 这类逻辑路径对应的源码。"""
        path = self._root / logical_path
        if not path.is_file():
            raise LocalDataError(f"缺少 Lua 脚本：{path}")
        return path.read_text(encoding="utf-8")


class Assets:
    """解包出的 Unity 资源根（目录名通常是 ``assets``，也可以是它的上一层快照根）。"""

    def __init__(self, root: Path) -> None:
        self._root = root

    @property
    def root(self) -> Path:
        """目录本身；只在写日志或诊断时用。"""
        return self._root

    def resolve(self, unity_path: str) -> Path | None:
        """把 ``assets/...`` 逻辑路径落到磁盘文件；找不到返回 ``None``。"""
        for candidate in candidate_asset_files(self._root, unity_path):
            if candidate.is_file():
                return candidate
        return None

    def missing(self, unity_paths: Iterable[str]) -> list[str]:
        """这些逻辑路径里哪些找不到。"""
        return sorted(path for path in unity_paths if self.resolve(path) is None)

    def logical_path(self, path: Path) -> str:
        """磁盘文件 -> 逻辑 unity 路径（``assets/...``、``.prefab`` 后缀）。"""
        if not path.is_relative_to(self._root):
            raise ValueError(f"{path} 不在资源根 {self._root} 里")
        relative = path.relative_to(self._root).as_posix()
        logical = ("assets/" if self._root.name.lower() == "assets" else "") + relative
        logical = logical.removesuffix(".json")
        return logical if logical.endswith(".prefab") else logical + ".prefab"

    def json_files(self) -> Iterator[tuple[str, Path]]:
        """按逻辑路径排序的全部 JSON：``(逻辑路径, 磁盘文件)``。"""
        for path in sorted(self._root.rglob("*.json")):
            yield self.logical_path(path), path


def candidate_asset_files(root: Path, unity_path: str) -> list[Path]:
    """同一份逻辑资源可能落成的文件，按优先顺序。

    新包把 ``region02.prefab`` 导出成 ``region02.json``；老导出的 ``region02.prefab.json`` 也顺手找一下，
    这样遇到旧格式时由解析器报「旧 RAW 不再支持」，而不是当成资源缺失。两者之外不做猜测。"""
    stem, suffix = unity_path, ".json"
    for known in ASSET_SUFFIXES:
        if unity_path.endswith(known):
            stem = unity_path[: -len(known)]
            break
    # root 可为快照根或 assets 目录本身；逻辑路径始终保留 assets/。
    logical_paths = [stem + suffix, unity_path + suffix, unity_path]
    if root.name.lower() == "assets" and unity_path.startswith("assets/"):
        logical_paths = [path[len("assets/") :] for path in logical_paths]
    return list(dict.fromkeys(root / path for path in logical_paths))
