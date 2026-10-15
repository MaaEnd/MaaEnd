# zmdmap 精简游戏数据（`uv run zmdmap`）

`tools/zmdmap` 是 MaaEnd 六个任务所需精简游戏数据的统一入口：既能从本地游戏数据**生成**，也能从
zmdmap**下载**已发布数据，并投影进入库的 `data/*.json`。原 Node 实现（`fetch-data.mjs` /
`project.mjs`）已全部移植到这里的 Python 模块，不再需要 Node。

## 子命令

| 子命令 | 作用 |
| --- | --- |
| `generate` | 从本地 TableCfg / JsonData 生成六份精简数据到 `data/raw/` |
| `fetch` | 从 zmdmap 下载已发布数据到 `data/raw/`，再投影进入库的 `data/*.json` |

不带子命令时只打印帮助。`fetch` 的两个步骤各由一个开关控制：`--download/--no-download`、
`--project/--no-project`；`generate` 只带可选的 `--project`。

```shell
uv run zmdmap                                     # 打印帮助
uv run zmdmap generate                            # 六份都生成到 tools/pipeline-generate/data/raw/
uv run zmdmap generate --list                     # 列出数据集名（每行一个）
uv run zmdmap generate --only collect_points      # 只生成一份（可重复给：--only a --only b）
uv run zmdmap generate --out-dir /tmp/x --force
uv run zmdmap fetch                               # 下载已发布数据并投影入库
uv run zmdmap fetch --no-download                 # 不下载，只用现有 data/raw/ 重新投影入库
```

`python -m tools.zmdmap` 与 `uv run zmdmap` 等价。

## 文件职责

### 生成：六份精简游戏数据（`datasets/`）

`datasets/__init__.py` 的 `DATASETS` 登记了六份数据的顺序与文件名，`uv run zmdmap generate` 按这个
顺序逐个调用 `build_*`；每个 `build_*` 的入参都是 `(table_cfg_dir, json_data_dir)`，只从本地兄弟仓库
取数据（只用到其中一个目录的也照这个签名收）。

| 文件 | 说明 |
| --- | --- |
| `datasets/delivery_jobs.py` | `delivery_jobs.json` |
| `datasets/environment_monitoring.py` | `environment_monitoring.json` |
| `datasets/sell_product.py` | `sell_product.json` |
| `datasets/delivery_destinations.py` | `delivery_destinations.json` |
| `datasets/teleport_anchors.py` | `teleport_anchors.json` |
| `datasets/collect_points.py` | `collect_points.json` |

### 共用件（`utils/`）

| 文件 | 说明 |
| --- | --- |
| `utils/game_data.py` | 读 TableCfg / GameplayConfig 与五语言文本表 |
| `utils/coords.py` | 世界坐标读取与 `coord` 文案 |
| `utils/levels.py` | ID 前缀 → 关卡 → 底图 |

### 下载与投影

| 文件 | 说明 |
| --- | --- |
| `fetch.py` | 版本接口 + 发布 CDN 的下载与缓存（原 `fetch-data.mjs`） |
| `project.py` | 按 BaseNav zone 表投影 / 材料化入库数据（原 `project.mjs`） |
| `navmesh.py` | 从 `BNAV` / `BGEO` 二进制 nav 数据里读出 zone 表 |
| `paths.py` | 输入 / 输出路径的默认值与环境变量名 |
| `cli.py` | `uv run zmdmap` 的入口，跑上面六个 `build_*` / 下载 / 投影 |

入库 JSON 用 Python 的 `json` 序列化（`ensure_ascii=False`、缩进 4）。与原 Node 实现只在数字写法上
有差别（整型浮点写成 `520.0` 而不是 `520`），值与键顺序一致——移植时已用原 JS 投影器逐份比对过解析
后的结构。

## `generate` 的开关

生成之外的两步默认都**不**执行，各由一个开关打开：

| 开关 | 作用 |
| --- | --- |
| `--sync` | 生成前先同步兄弟仓库（只对干净工作区做快进合并） |
| `--update-model` | 同步时顺带更新 `assets/resource/model` 子模块（navmesh / zone 投影参数） |
| `--project` | 生成后把 `data/raw/` 投影进入库的 `data/*.json` |
| `--dry-run` | 只报告会做什么（不合并、不生成、不投影） |

只对干净的工作区做快进合并，绝不 stash / reset / 覆盖本地改动；本地重算不会改
`data/version.txt`（那是「已发布数据版本」的标记）。

## 输入路径（`generate`）

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
uv run zmdmap generate --workspace /data/endfield
uv run zmdmap generate --table-cfg-dir /data/BeyondTableCfg/TableCfg --json-data-dir /data/BeyondMemoryPack/JsonData

# CI 里常用环境变量
ZMDMAP_WORKSPACE=/data/endfield uv run zmdmap generate
```

CI 只需要出数据时用 `uv run --no-default-groups zmdmap generate`：`click` 在核心依赖里，这样不会顺带
安装 `scripts` 组（maafw / fastapi / numpy 等）。

## 产出

六份产物都只带游戏世界坐标，落在**不入库**的 `tools/pipeline-generate/data/raw/`；底图像素由投影
步骤按 BaseNav 的 zone 表算出来，进入库的 `tools/pipeline-generate/data/*.json`。上游发布的数据不含
底图像素，所以 `fetch` 下载完必须先投影才能被各任务生成器读取。

整条链路（同步兄弟仓库 → 本地生成 → 投影）也在本入口，用开关控制后两步：

```shell
uv run zmdmap generate --sync                              # 先同步兄弟仓库，再生成
uv run zmdmap generate --sync --update-model --project     # 连 navmesh 子模块一起更新并投影入库
uv run zmdmap generate --dry-run                           # 只报告会做什么
uv run zmdmap fetch                                        # 下载 → 投影（等同 pnpm fetch:zmdmap）
```

## 读取本地数据

数据入口只有 `utils/game_data.py` 里的三个函数：

| 函数 | 读什么 |
| --- | --- |
| `read_table_cfg(dir, name)` | `<table_cfg_dir>/<name>.json` |
| `read_gameplay_config(dir, name)` | `<json_data_dir>/GameplayConfig/<name>.json` |
| `localized_text(dir, text_id, lang)` | `<table_cfg_dir>/I18nTextTable_<后缀>.json` |

读出来的 JSON 会换成属性可访问的 `Record`，`build_*` 里直接写 `entry.position.x`；三个函数都按
「目录 + 文件名」缓存，六份数据共用同一次读取。

## 校验

参照物是入库的 `tools/pipeline-generate/data/raw/`：拿同一份本地兄弟仓库重算一遍，产物应当与它
逐字节一致。

```shell
uv run zmdmap generate --force --out-dir <临时目录>
```

再逐份比对 `tools/pipeline-generate/data/raw/` 下的同名文件。只改注释、重命名这类不动输出的改动，
应当零差异；动了取数逻辑就看差异是否符合预期。注意 `data/raw/` 是**上一次本地生成**的快照，与已发布
版本不一定是同一版（`data/version.txt` 记的是发布版本）。
