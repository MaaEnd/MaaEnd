# Pipeline 生成数据

MaaEnd 使用由 zmdmap 数据 CI 生成并发布的精简游戏数据，落在 [`tools/zmdmap/data/`](../zmdmap/data/)：

- `delivery_jobs.json`：地区、仓储节点及其可装箱物品；
- `delivery_destinations.json`：仓储和送货终点的多语言文本、底图像素 `u` / `v`、世界高度 `y` 与归属关系；
- `environment_monitoring.json`：监测终端、观察点与各语言名称；
- `sell_product.json`：据点、可售物品、据点特性与匹配干员；
- `teleport_anchors.json`：营地（地图传送锚点）的底图像素 `u` / `v` 与传送 ID；
- `collect_points.json`：可交互采集物的底图像素 `u` / `v`。

这些文件都是按任务需要裁剪出的游戏数据，不是完整的上游数据快照：除了最前面的两个元数据字段（`version`：这份数据来自哪一版游戏数据；`sha256`：这份数据**原始形态**（只带世界坐标的那一份）的内容指纹，入库投影后照抄、不重算），不放别的来源元数据，字段说明也不放进 JSON（字段含义见 [`tools/zmdmap`](../zmdmap/README.md) 与各数据集模块）。

## 数据流

zmdmap 的数据树就在 `tools/zmdmap/`：`data/raw/` 放原样产物（不入库），`data/` 放可以直接用的数据（入库）。上游发布的数据**只带游戏世界坐标**，不含底图像素：投影参数（`zone` / `sx` / `tx` / `sy` / `ty`）属于 BaseNav 数据包，只有 MaaEnd 手里有。所以下载下来的原始数据先落在不入库的 `tools/zmdmap/data/raw/`，再由投影步骤（`uv run zmdmap fetch` 的一部分）按 BaseNav 的 zone 表把 `x` / `z` 原位换成 `u` / `v` 进入库的 `tools/zmdmap/data/*.json` —— 各任务生成器只读这一份。

```text
BeyondTableCfg/TableCfg ─┐
                         ├→ uv run zmdmap generate → tools/zmdmap/data/raw/*.json（世界坐标，不入库）
BeyondMemoryPack/JsonData┘
                                      ↓ 投影（按 BaseNav zone 表）
                    tools/zmdmap/data/*.json（含 u/v 与 maps，入库）→ 各任务生成器 → assets/…

zmdmap 发布精简游戏数据（只有世界坐标）
                                  ↓
             uv run zmdmap fetch（下载发布数据）→ tools/zmdmap/data/raw/*.json
                                  ↓ 同一个投影步骤
                         tools/zmdmap/data/*.json
```

`uv run zmdmap fetch`（也就是 `pnpm fetch:zmdmap`）按上游的发布清单 `manifest.json` 逐份判断要不要重新下载（清单给出这批数据是哪一版、每份原始形态的内容指纹，以及这一批都发了哪几份：指纹一致就跳过，本地没有或指纹对不上的才下；清单里没有的那几份不在这批里，本地有就保持原样；清单还没发布时不做比对、直接下载），本地与 CI 同一套判断；**下了的才重新投影入库**，本地已是最新时连投影都不跑：

```shell
pnpm fetch:zmdmap                                # 下载（必要时）+ 投影到 tools/zmdmap/data/*.json
uv run zmdmap fetch --force                      # 忽略清单判断直接重下，并带时间戳不走远端缓存
uv run zmdmap fetch --no-download                # 不下载，只用 data/raw/ 重新投影
uv run zmdmap fetch --nav path/to/base.nav.gz    # 指定 nav 数据
uv run zmdmap fetch --only collect_points        # 只下载/投影其中几份
```

`fetch` 还会把不需要投影的那几份（EssenceFilter 用的 `energy_point_gems.json`、`weapons.json`，IconRecognition 用的 `item_mini_table.json`）直接下载到 `tools/zmdmap/data/`。

完整生成某个任务时使用对应的 pnpm 命令。这些命令各自只取自己用得到的那几份数据（`fetch:zmdmap --only <数据集>`），再执行当前任务自己的生成链路，所以不必先整包下载：

```shell
pnpm generate:AutoDelivery
pnpm generate:DeliveryJobs
pnpm generate:EnvironmentMonitoring
pnpm generate:OutpostTrading
pnpm generate:SeizeDeliveryJobs
```

## 投影

`tools/zmdmap/utils/project.py` 做的事就是换算：把条目里的 `x` / `z` 按原位置换成 `u = sx*x + tx`、`v = -sy*z + ty`（`y` 是世界高度，不参与换算、原样保留），加上 `maps` 块（本次用到的底图及其 zone 参数），并把不需要投影的数据原样带到入库目录。

- nav 数据缺省读 `assets/resource/model` 子模块的 `map/navmesh/base.nav.gz`；本地没有时按该子模块当前版本从远端取，`--nav` 可指定别的本地文件或 URL。
- 没有对应 zone 的底图（如 `blackbox`）整图跳过并打印一行。
- nav 数据与精简数据不是同一版本时，坐标会落在底图外，直接报错而不是静默丢弃。

## 本地生成脚本

`datasets/`（一份数据一个模块）与下载/入库统一放在 [`tools/zmdmap`](../zmdmap/README.md)，入口是 `uv run zmdmap`（子命令 `generate` / `fetch`）。生成只读本地兄弟仓库：各数据集的 `Dataset.build` 通过 `utils/game_data.py` 读 `BeyondTableCfg/TableCfg` 与 `BeyondMemoryPack/JsonData`（记录取值、关卡索引也在那里）；路径与语言表这类静态配置在 `utils/config.py`，其余共用件在 `utils/`。

默认按当前 MaaEnd 仓库的兄弟目录解析输入，因此各仓库位于同一 `Endfield` 目录时无需传路径：

```shell
uv run zmdmap generate                        # 生成到 data/raw/ 并入库到 data/
uv run zmdmap generate --list                 # 列出数据集名、文件名与入库方式
uv run zmdmap generate --only collect_points  # 只生成一份（可重复给）
```

仓库不在默认相对位置时用 `--table-cfg-dir` / `--json-data-dir` 覆盖，或改用环境变量 `ZMDMAP_TABLE_CFG_DIR` / `ZMDMAP_JSON_DATA_DIR`（命令行 > 环境变量 > 默认）；`--out-dir` / `--data-dir` 可把结果写到临时目录做本地/CI 等价校验（生成结果与已有文件相同时跳过写入，`--force` 强制重写）。生成默认接着入库：带坐标的投影、其余原样复制，生成器读的就是入库后的那几份。

`uv run zmdmap generate` 同时是「同步兄弟仓库 → 本地生成 → 入库」的总入口：

```shell
uv run zmdmap generate --dry-run                           # 只报告会做什么
uv run zmdmap generate --sync                              # 先同步兄弟仓库，再生成并入库
uv run zmdmap generate --sync --raw-only                   # 同步、生成，只要原始形态
```

它只对干净的工作区做快进合并，绝不 stash / reset / 覆盖本地改动；本地重算不会凭空改掉版本号（版本写在每份数据里）。

## 鸣谢

感谢 [zmdmap](https://zmdmap.com/) 为 MaaEnd 提供游戏数据整理与发布支持。
