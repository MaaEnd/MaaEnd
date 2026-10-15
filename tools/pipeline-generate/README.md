# Pipeline 生成数据

MaaEnd 使用六份由 zmdmap 数据 CI 生成并发布的精简游戏数据：

- `data/delivery_jobs.json`：地区、仓储节点及其可装箱物品；
- `data/delivery_destinations.json`：仓储和送货终点的多语言文本、世界坐标、底图像素与归属关系；
- `data/environment_monitoring.json`：监测终端、观察点与五语言名称；
- `data/sell_product.json`：据点、可售物品、据点特性与匹配干员；
- `data/teleport_anchors.json`：营地（地图传送锚点）的世界坐标、底图像素与传送 ID；
- `data/collect_points.json`：可交互采集物的世界坐标与底图像素。

这些文件都是按任务需要裁剪出的游戏数据，不是完整的上游数据快照，也不包含数据生产端的版本或来源元数据。MaaEnd 通过 `data/version.txt` 单独记录当前数据版本。

## 数据流

上游发布的数据**只带游戏世界坐标**，不含底图像素：投影参数（`zone` / `sx` / `tx` / `sy` / `ty`）属于 BaseNav 数据包，只有 MaaEnd 手里有。所以下载下来的原始数据先落在不入库的 `data/raw/`，再由 `fetch-data.mjs` 按 BaseNav 的 zone 表投影/材料化进入库的 `data/*.json` —— 各任务生成器只读这一份。

```text
BeyondTableCfg/TableCfg ─┐
                        ├→ data/scripts/generate.py → data/raw/*.json（世界坐标，不入库）
BeyondMemoryPack/JsonData┘
                                     ↓ fetch-data.mjs（按 BaseNav zone 表投影）
                        data/*.json（含 u/v 与 maps，入库）→ 各任务生成器 → assets/…

BeyondTableCfg + BeyondMemoryPack → endfield-workflow（CI）→ zmdmap 发布精简游戏数据
                                                        → fetch:zmdmap（MaaEnd）→ data/raw/*.json
                                                        → 同一个 fetch-data.mjs → data/*.json
```

`fetch-data.mjs` 按 zmdmap 版本接口判断要不要重新下载（版本未变化且 `data/raw/` 完整时跳过下载，网络检查失败时保留现有缓存），**下载与否都会跑一次投影**：

```shell
pnpm fetch:zmdmap                                   # 下载（必要时）+ 投影到 data/*.json
node tools/pipeline-generate/fetch-data.mjs --force        # 忽略版本缓存，强制重新下载
node tools/pipeline-generate/fetch-data.mjs --cache-bust   # 同上，并绕过远端缓存
node tools/pipeline-generate/fetch-data.mjs --project-only # 不下载，只用 data/raw/ 重新投影
node tools/pipeline-generate/fetch-data.mjs --nav path/to/base.nav.gz   # 指定 nav 数据
node tools/pipeline-generate/fetch-data.mjs --only collect_points       # 只投影其中几份
```

完整生成某个任务时使用对应的 pnpm 命令。这些命令都会先 `fetch:zmdmap`，再执行当前任务自己的生成链路：

```shell
pnpm generate:AutoDelivery
pnpm generate:DeliveryJobs
pnpm generate:EnvironmentMonitoring
pnpm generate:OutpostTrading
pnpm generate:SeizeDeliveryJobs
```

## 投影

`tools/pipeline-generate/project.mjs` 做的事就是换算：`u = sx*x + tx`、`v = -sy*z + ty`，加上 `maps` 块（本次用到的底图及其 zone 参数），并把不带坐标的三份数据与 `version.txt` 原样带到 `data/`。

- nav 数据缺省读 `assets/resource/model` 子模块的 `map/navmesh/base.nav.gz`；本地没有时按该子模块当前版本从远端取，`--nav` 可指定别的本地文件或 URL。
- 没有对应 zone 的底图（如 `blackbox`）整图跳过并打印一行。
- nav 数据与精简数据不是同一版本时，坐标会落在底图外，直接报错而不是静默丢弃。

## 本地生成脚本

`data/scripts/` 下的脚本与 `endfield-workflow` 的 `src/beyond_map/processors/maaend/` 一一对应，改哪边都要同步另一处；`coords.py` / `levels.py` / 六个 `build_*` 逐字相同，只有 import 行不同——那边从流水线的 `RunContext` 取数据，这边由 `local_context.py` 读本地兄弟仓库。细节见 [`data/scripts/README.md`](data/scripts/README.md)。

默认目录按当前 MaaEnd 仓库的兄弟目录解析，因此三个仓库位于同一 `Endfield` 目录时无需传路径：

```shell
uv run tools/pipeline-generate/data/scripts/generate.py            # 六份都生成到 data/raw/
uv run tools/pipeline-generate/data/scripts/generate.py --list     # 列出数据集名
uv run tools/pipeline-generate/data/scripts/generate.py --only collect_points
```

仓库不在默认相对位置时用 `--table-cfg-dir` / `--json-data-dir` 覆盖；`--out-dir` 可把结果写到临时目录做本地/CI 等价校验（生成结果与已有文件相同时跳过写入，`--force` 强制重写）。生成完还要跑一次投影，才是生成器读取的 `data/*.json`。

`tools/pipeline-generate/sync_and_generate.py` 把「同步兄弟仓库 → 本地生成 → 投影」串成一条命令：

```shell
uv run python tools/pipeline-generate/sync_and_generate.py --dry-run          # 只报告会做什么
uv run python tools/pipeline-generate/sync_and_generate.py --update-model     # 顺带更新 navmesh 子模块
uv run python tools/pipeline-generate/sync_and_generate.py --no-sync --only sell_product
```

它只对干净的工作区做快进合并，绝不 stash / reset / 覆盖本地改动；本地重算不会改 `data/version.txt`（那是「已发布数据版本」）。

## 鸣谢

感谢 [zmdmap](https://zmdmap.com/) 为 MaaEnd 提供游戏数据整理与发布支持。
