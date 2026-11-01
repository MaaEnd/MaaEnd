"""把注入的服务读成地区总览要的东西：当版表、Lua 源码、导出的 Unity JSON。

地区总览的其它模块只看这个类，不碰文件系统；这里顺带把「读不到」和「格式不对」都翻成
:class:`~tools.zmdmap.datasets.region3d.errors.InputError`，上层按「这一份输入不完整」处理。
"""

from dataclasses import dataclass, field
from os import walk
from pathlib import Path
from typing import TYPE_CHECKING, Any

from ...utils.errors import LocalDataError
from ...utils.inputs import Assets, LuaScripts, TableCfg
from .errors import InputError
from .unity import (
    TransformCurve,
    UnityDocument,
    load_json_document,
    parse_animation,
    parse_unity_document,
)

if TYPE_CHECKING:
    from collections.abc import Iterable


@dataclass(slots=True)
class LocalDataProvider:
    """地区总览的读取层。"""

    table_cfg: TableCfg
    lua: LuaScripts
    assets: Assets
    _by_stem: dict[str, list[Path]] | None = field(default=None, init=False, repr=False)
    _documents: dict[Path, UnityDocument] = field(default_factory=dict[Path, UnityDocument], init=False, repr=False)

    # ── 表与 Lua ─────────────────────────────────────────────────────────

    def get_table(self, logical_name: str) -> Any:
        try:
            return self.table_cfg.raw_table(logical_name)
        except LocalDataError as error:
            raise InputError(str(error)) from error

    def get_lua(self, logical_path: str) -> str:
        try:
            return self.lua.script(logical_path)
        except LocalDataError as error:
            raise InputError(str(error)) from error

    # ── 资源 ─────────────────────────────────────────────────────────────

    def asset_path(self, unity_path: str) -> Path | None:
        return self.assets.resolve(unity_path)

    def missing_asset_paths(self, unity_paths: Iterable[str]) -> list[str]:
        return self.assets.missing(unity_paths)

    def get_asset(self, unity_path: str) -> UnityDocument:
        path = self.asset_path(unity_path)
        if path is None:
            raise InputError(f"{self.assets.root} 下没有资源 '{unity_path}'")
        return self._document(path)

    def get_asset_data(self, unity_path: str) -> Any:
        path = self.asset_path(unity_path)
        if path is None:
            raise InputError(f"{self.assets.root} 下没有资源 '{unity_path}'")
        return load_json_document(path)

    def find_prefab(self, name: str, component_type: str) -> str:
        """按资源内容查裸名字引用：文件移动或改名都不改变资源身份。

        导出时文件名通常就是资源名，所以先只解析同名文件（几十个），够用就不碰别的；真遇上改了名的
        资源（名字与文件名对不上）才回头把整棵资源树扫一遍 —— 那是几十万个 JSON，很慢。
        """
        matches = self._prefabs_named(self._stem_index().get(name.lower(), ()), name, component_type)
        if not matches:
            matches = self._prefabs_named(self._all_prefab_paths(), name, component_type)
        if len(matches) != 1:
            raise InputError(
                f"名为 '{name}' 且带 {component_type} 的资源应当恰好一份，实际找到 {len(matches)} 份：{matches}"
            )
        return matches[0]

    def get_animation(self, unity_path: str) -> list[TransformCurve]:
        path = self.asset_path(unity_path)
        if path is None:
            raise InputError(f"{self.assets.root} 下没有动画 '{unity_path}'")
        return parse_animation(path)

    # ── prefab 查找 ──────────────────────────────────────────────────────

    def _document(self, path: Path) -> UnityDocument:
        """同一个文件只解析一次：一份 prefab 常被地区、面板与相机链一起用到。"""
        document = self._documents.get(path)
        if document is None:
            document = self._documents[path] = parse_unity_document(path)
        return document

    def _stem_index(self) -> dict[str, list[Path]]:
        """文件名（去掉 ``.json`` / ``.prefab``）-> 磁盘文件；只走目录，不解析内容。"""
        if self._by_stem is None:
            index: dict[str, list[Path]] = {}
            for directory, _subdirs, files in walk(self.assets.root):
                for file in files:
                    if file.endswith(".json"):
                        stem = file.removesuffix(".json").removesuffix(".prefab").lower()
                        index.setdefault(stem, []).append(Path(directory) / file)
            self._by_stem = index
        return self._by_stem

    def _prefabs_named(self, paths: Iterable[Path], name: str, component_type: str) -> list[str]:
        """这些文件里，根节点叫 ``name`` 且带 ``component_type`` 的那几份（逻辑路径）。"""
        found: list[str] = []
        for path in paths:
            try:
                document = self._document(path)
            except InputError:
                continue  # 同目录还有纹理、材质和动画，它们不是 prefab
            if document.root.name != name:
                continue
            if any(node.component(component_type) is not None for node in document.games.values()):
                found.append(self.assets.logical_path(path))
        return found

    def _all_prefab_paths(self) -> list[Path]:
        """整棵资源树里的 JSON（去重、只留 ``assets/`` 逻辑路径）；只在按文件名找不到时用。"""
        paths: list[Path] = []
        seen: set[str] = set()
        for logical, path in self.assets.json_files():
            if not logical.startswith("assets/") or logical in seen:
                continue
            seen.add(logical)
            paths.append(path)
        return paths
