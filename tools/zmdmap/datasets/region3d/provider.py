"""把注入的服务读成地区总览要的东西：当版表、Lua 源码、导出的 Unity JSON。

地区总览的其它模块只看这个类，不碰文件系统；这里顺带把「读不到」和「格式不对」都翻成
:class:`~tools.zmdmap.datasets.region3d.errors.InputError`，上层按「这一份输入不完整」处理。
"""

from dataclasses import dataclass, field
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
    from pathlib import Path


@dataclass(slots=True)
class LocalDataProvider:
    """地区总览的读取层。"""

    table_cfg: TableCfg
    lua: LuaScripts
    assets: Assets
    _prefab_names: dict[str, list[Path]] | None = field(default=None, init=False, repr=False)

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
        return parse_unity_document(path)

    def get_asset_data(self, unity_path: str) -> Any:
        path = self.asset_path(unity_path)
        if path is None:
            raise InputError(f"{self.assets.root} 下没有资源 '{unity_path}'")
        return load_json_document(path)

    def find_prefab(self, name: str, component_type: str) -> str:
        """按资源内容查裸名字引用：文件移动或改名都不改变资源身份。"""
        if self._prefab_names is None:
            index: dict[str, list[Path]] = {}
            seen: set[str] = set()
            for logical, path in self.assets.json_files():
                if not logical.startswith("assets/") or logical in seen:
                    continue
                seen.add(logical)
                try:
                    # 扫的就是实际文件；用转换后的逻辑名会先按小写找文件，Linux 上 #Highlighted
                    # 这类动画会在判断格式之前就报缺失。
                    document = parse_unity_document(path)
                except InputError:
                    # 同目录还有纹理、材质和动画，它们不是 prefab。
                    continue
                index.setdefault(document.root.name, []).append(path)
            self._prefab_names = index
        matches: list[str] = []
        for path in self._prefab_names.get(name, []):
            document = parse_unity_document(path)
            if any(node.component(component_type) is not None for node in document.games.values()):
                matches.append(self.assets.logical_path(path))
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
