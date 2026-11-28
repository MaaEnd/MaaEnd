"""地区总览：直接生成 720p 的 polygon 和 click（``region3d.json``）。

产物是「地区 -> 子地图 -> 多边形 + 点击点」，坐标基于 1280×720、左上为原点，任务拿 ``click`` 就能点进
地区总览里的每个子区域。它不做世界坐标投影：生成时已经按客户端的面板、相机和 Canvas 参数算到了屏幕
像素，所以入库时原样复制（:attr:`Region3d.needs_projection` 不打开）。

输入比其他数据集多两样，都在 ``build`` 的参数上声明（``table_cfg`` / ``lua`` / ``assets``）：

- ``table_cfg``：``DomainDataTable`` 枚举地区，``domainName.id`` 查 ``I18nTextTable_CN``；
- ``lua``：读当版路径、动画名模板与 FOV；只接受字面量与简单常量拼接，不执行 Lua；
- ``assets``：读 prefab、相机和动画 JSON。子地图来自 ``RegionMapSetting._cfg``，包括独立地图。

新地区不需要维护清单：跟着当版表、Lua 和资源引用发现，缺东西、重名或结构不支持都直接报错，不会拿默认值
糊过去。任一地区失败就整份失败，不发布半份结果。固定数值与运行公式在 ``binary_rules.py``，生成设置（输出
尺寸、面板状态、点击安全距离）在 ``options.py``；细节见同目录的 ``README.md``。
"""

from typing import Any

from ...utils.inputs import Assets, LuaScripts, TableCfg
from ..base import Dataset, DatasetError
from .discovery import discover_registry
from .errors import Region3dError
from .generator import generate
from .options import GenerationOptions
from .provider import LocalDataProvider


class Region3d(Dataset):
    """地区总览的 720p 点击区域（``region3d.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg, lua: LuaScripts, assets: Assets) -> dict[str, Any]:
        """``region -> {name, area: 子地图 -> {polygon, click}}``，坐标已裁剪到 720p 屏幕。"""
        provider = LocalDataProvider(table_cfg=table_cfg, lua=lua, assets=assets)
        try:
            registry = discover_registry(provider)
        except (Region3dError, ValueError) as error:
            raise DatasetError(f"地区总览的当版输入不完整：{error}") from error

        missing = provider.missing_asset_paths(registry.all_resources())
        if missing:
            raise DatasetError(
                f"{len(missing)} 个地区总览输入资源缺失（先把解包资源水合好）：\n  " + "\n  ".join(missing)
            )

        try:
            return generate(provider, registry, GenerationOptions())
        except (Region3dError, ValueError) as error:
            raise DatasetError(f"地区总览生成失败：{error}") from error
