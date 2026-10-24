# 地区总览（`region3d`）

对本地游戏数据跑：

```shell
uv run zmdmap generate --only region3d \
  --lua-dir ../BeyondLua/LuaScripts \
  --assets-dir ../BeyondImage/assets
```

产物先落到 `data/raw/region3d.json`，入库时原样复制到 `data/region3d.json`（不做世界坐标投影）：

```json
{
    "region01": {
        "name": "四号谷地",
        "area": {
            "map01_lv001": {
                "polygon": [[266.2912135642216, 321.9298085437866], [681.1502004606575, 321.9298085437866],
                            [690.2096211299646, 624.5326606090639], [184.01737124418668, 624.5326606090639]],
                "click": [455, 478]
            }
        }
    }
}
```

坐标基于 1280×720，左上为原点。`polygon` 用于绘图，是投影算出来的浮点像素（按浮点画，取整会错位）；
`click` 用于点击，是整数像素：先把多边形裁剪到屏幕、减去同地区其它地图的重叠面积，再取面积重心并挑一个
离边界有安全距离的整点。生成失败会报错，整份结果成功后才写出，不会留下半份。

输入由注入的服务提供（`Region3d.build` 的参数上声明，`generate` 按签名传进来）：

- `table_cfg`（`--table-cfg-dir`）：`DomainDataTable` 枚举地区，`domainName.id` 查询 `I18nTextTable_CN`。
- `lua`（`--lua-dir`）：读取当版路径、动画名模板及 FOV；只接受字面量与简单常量拼接，不执行 Lua。
- `assets`（`--assets-dir`）：读取 prefab、相机和动画 JSON。子地图来自 `RegionMapSetting._cfg`，包括独立地图。
  只认新包导出（嵌套 GameObject 的 JSON）：旧 RAW 导出（`$tree` / `$objects`）不再支持，遇到会报错，重新解包即可。

新地区不需维护清单；跟随当版表、Lua 和资源引用发现。缺失、重名或不支持的结构会报错。
`Region3d.build` 的顺序是：`discover_registry(provider)` 发现地区 → 核对资源齐不齐 →
`generate(provider, registry, GenerationOptions())` 返回上述字典；哪一步出错都折成数据集错误，不返回半份。

目录里的分工：`__init__.py` 是数据集入口（`Region3d.build`），`provider.py` 负责读当版输入，
`discovery.py` 发现地区清单（`registry.py` 存发现出来的地区与它们的资源引用），`generator.py` 逐地区
算像素，`lua.py` 从 Lua 源码里读字面量，`layout.py` / `geometry.py` / `projection.py` 算变换与投影，
`screen.py` 裁剪并选点击点，`unity.py` 解析 prefab / 动画 JSON，`binary_rules.py` 是固定运行规则，
`options.py` 是生成设置，`errors.py` 是三种错误类型（读输入 / 看结构 / 算结果）。

客户端的固定数值与运行公式集中在 `binary_rules.py`：标准分辨率与缩放系数、画布缩放、面板初始化、
样式状态筛选。输出尺寸、面板状态和点击安全距离在 `options.py`，属于生成器设置而不是客户端规则。
运行行为随版本变化时同步更新 `binary_rules.py`。
