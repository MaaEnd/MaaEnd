# 精简游戏数据生成脚本

这里的脚本与 `endfield-workflow` 的 `src/beyond_map/processors/maaend/` 一一对应，是同一批
`build_*` 的两份落地：那边在流水线里跑、输出 zmdmap 的发布数据；这边在本地跑，用同样的输入
把 `data/raw/` 重新算一遍，做本地/CI 等价校验。

**改哪边都要同步另一处。** 对应关系：

| 文件 | 说明 |
| --- | --- |
| `coords.py` | 坐标读取与 `coord` 文案，逐字对应 |
| `levels.py` | ID 前缀 → 关卡 → 底图，逐字对应 |
| `delivery_jobs.py` | `delivery_jobs.json`，逐字对应 |
| `environment_monitoring.py` | `environment_monitoring.json`，逐字对应 |
| `sell_product.py` | `sell_product.json`，逐字对应 |
| `delivery_destinations.py` | `delivery_destinations.json`，逐字对应 |
| `teleport_anchors.py` | `teleport_anchors.json`，逐字对应 |
| `collect_points.py` | `collect_points.json`，逐字对应 |
| `local_context.py` | MaaEnd 专有：读取本地仓库，提供同样的 `RunContext` |
| `generate.py` | MaaEnd 专有：本地入口，跑上面六个 `build_*` 写 `data/raw/` |

workflow 侧的 `__init__.py` / `processor.py` / `context.py` 是流水线自身的接线，这边没有对应
文件。

## 同步方式

逐字对应 = 除了 import 行以外完全一样（函数名、返回值、注释、文案都不许各写一份）。搬过来时
只改 import：

| workflow 侧 | 这里 |
| --- | --- |
| `from beyond_map.context import RunContext` | `from local_context import RunContext` |
| `from loguru import logger` | `from local_context import logger` |
| `from beyond_map.processors.maaend.coords import ...` | `from coords import ...` |
| `from beyond_map.processors.maaend.levels import ...` | `from levels import ...` |
| `from beyond_map.models.*` / `from beyond_map.utils import load_json_dict` | `from local_context import ...` |

数据来源的区别都收在 `local_context.py` 里：那边由流水线缓存提供 `ctx.table_cfg` /
`ctx.json_data` / `ctx.i18n`，这边读 `../BeyondTableCfg/TableCfg` 与
`../BeyondMemoryPack/JsonData`，并用属性可访问的 `Record` 顶掉 pydantic 模型（`Text` 顶掉
枚举，支持 `.value`）。

## 产出与校验

```shell
uv run data/scripts/generate.py                 # 六份都写到 ../data/raw/
uv run data/scripts/generate.py --only collect_points
```

六份产物都只带游戏世界坐标，落在不入库的 `../data/raw/`；底图像素由
`../fetch-data.mjs` 按 BaseNav 的 zone 表投影进入库的 `../data/*.json`。

校验方式是比对产物而不是比对代码：`endfield-workflow` 的 `tools/check_maaend_parity.py`
会分别跑 workflow 侧的 `build_*` 与这里的 `generate.py --out-dir <临时目录>`，逐项比对六份
JSON（解析后的结构，不看缩进与换行）；改了任何一边的 `build_*` 都要让它通过。
