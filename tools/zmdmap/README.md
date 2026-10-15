# zmdmap 精简游戏数据生成（`uv run zmdmap`）

`tools/zmdmap` 生成 MaaEnd 六个任务所需的精简游戏数据。这些 `build_*` 与
`endfield-workflow` 的 `src/beyond_map/processors/maaend/` 一一对应，是同一批代码的两份落地：
那边在流水线里跑、输出 zmdmap 的发布数据；这边是 MaaEnd 仓库内的同一份实现，可在本地或 CI
直接跑，用同样的输入重算一遍，做本地 / CI 等价校验。

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
| `local_context.py` | MaaEnd 专有：读取本地兄弟仓库，提供同样的 `RunContext` |
| `paths.py` | MaaEnd 专有：输入 / 输出路径的默认值与环境变量名 |
| `cli.py` | MaaEnd 专有：`uv run zmdmap` 的入口，跑上面六个 `build_*` |

workflow 侧的 `__init__.py` / `processor.py` / `context.py` 是流水线自身的接线，这里没有对应文件。

## 用法

```shell
uv run zmdmap                        # 六份都生成到 tools/pipeline-generate/data/raw/
uv run zmdmap --list                 # 列出数据集名（每行一个）
uv run zmdmap --only collect_points  # 只生成一份（可重复给：--only a --only b）
uv run zmdmap --out-dir /tmp/x --force
```

`python -m tools.zmdmap` 与 `uv run zmdmap` 等价。

后两步（同步输入仓库、投影进入库数据）默认都**不**执行，各由一个开关打开：

| 开关 | 作用 |
| --- | --- |
| `--sync` | 生成前先同步兄弟仓库（只对干净工作区做快进合并） |
| `--update-model` | 同步时顺带更新 `assets/resource/model` 子模块（navmesh / zone 投影参数） |
| `--project` | 生成后调用 `fetch-data.mjs --project-only`，投影进入库的 `data/*.json` |
| `--dry-run` | 只报告会做什么（不合并、不生成、不投影） |

### 输入路径

TableCfg 与 JsonData 默认取**与 MaaEnd 仓库平级的兄弟仓库**（由 `--workspace` 定位），可用环境变量
或 CLI 参数覆盖。优先级：**命令行 > 环境变量 > 默认**（由 click 的 `envvar` / `default` 保证，
实际生效的来源会在日志第一行打印出来）。

| 输入 | CLI 参数 | 环境变量 | 默认 |
| --- | --- | --- | --- |
| 兄弟仓库目录 | `--workspace` | `ZMDMAP_WORKSPACE` | MaaEnd 的平级目录 |
| TableCfg | `--table-cfg-dir` | `ZMDMAP_TABLE_CFG_DIR` | `<workspace>/BeyondTableCfg/TableCfg` |
| JsonData | `--json-data-dir` | `ZMDMAP_JSON_DATA_DIR` | `<workspace>/BeyondMemoryPack/JsonData` |

```shell
# 数据仓库不放在平级目录时
uv run zmdmap --workspace /data/endfield
uv run zmdmap --table-cfg-dir /data/BeyondTableCfg/TableCfg --json-data-dir /data/BeyondMemoryPack/JsonData

# CI 里常用环境变量
ZMDMAP_WORKSPACE=/data/endfield uv run zmdmap
```

CI 只需要出数据时用 `uv run --no-default-groups zmdmap`：`click` 在核心依赖里，这样不会顺带
安装 `scripts` 组（maafw / fastapi / numpy 等）。

## 产出

六份产物都只带游戏世界坐标，默认落在**不入库**的 `tools/pipeline-generate/data/raw/`；底图像素由
`tools/pipeline-generate/fetch-data.mjs` 按 BaseNav 的 zone 表投影进入库的
`tools/pipeline-generate/data/*.json`：

```shell
uv run zmdmap                 # 只出原始形态（世界坐标）
uv run zmdmap --project       # 再投影进入库数据（调 node fetch-data.mjs --project-only）
```

整条链路（同步兄弟仓库 → 本地生成 → 投影）都在本入口，用开关控制后两步：

```shell
uv run zmdmap --sync                              # 先同步兄弟仓库，再生成
uv run zmdmap --sync --update-model --project     # 连 navmesh 子模块一起更新并投影入库
uv run zmdmap --dry-run                           # 只报告会做什么
```

只对干净的工作区做快进合并，绝不 stash / reset / 覆盖本地改动；本地重算不会改
`data/version.txt`（那是「已发布数据版本」的标记）。

## 同步方式

逐字对应 = 除了 import 行以外完全一样（函数名、返回值、注释、文案都不许各写一份）。搬过来时
只改 import：

| workflow 侧 | 这里 |
| --- | --- |
| `from beyond_map.context import RunContext` | `from .local_context import RunContext` |
| `from loguru import logger` | `from .local_context import logger` |
| `from beyond_map.processors.maaend.coords import ...` | `from .coords import ...` |
| `from beyond_map.processors.maaend.levels import ...` | `from .levels import ...` |
| `from beyond_map.models.*` / `from beyond_map.utils import load_json_dict` | `from .local_context import ...` |

数据来源的区别都收在 `local_context.py` 里：那边由流水线缓存提供 `ctx.table_cfg` /
`ctx.json_data` / `ctx.i18n`，这边读兄弟仓库的 `BeyondTableCfg/TableCfg` 与
`BeyondMemoryPack/JsonData`，并用属性可访问的 `Record` 顶掉 pydantic 模型（`Text` 顶掉枚举，
支持 `.value`）。

## 校验

校验方式是比对产物而不是比对代码：`endfield-workflow` 的 `tools/check_maaend_parity.py` 会分别跑
workflow 侧的 `build_*` 与 MaaEnd 侧的 `uv run zmdmap --out-dir <临时目录>`，逐项比对六份 JSON
（解析后的结构，不看缩进与换行）；改了任何一边的 `build_*` 都要让它通过。
