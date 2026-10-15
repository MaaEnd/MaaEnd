# zmdmap 精简游戏数据（`uv run zmdmap`）

`tools/zmdmap` 是 MaaEnd 各任务所需精简游戏数据的统一入口：既能从本地游戏数据**生成**，也能从 zmdmap **下载**已发布数据，再入库成可以直接用的形式。原 Node 实现（`fetch-data.mjs` / `project.mjs`）已全部移植到这里的 Python 模块，不再需要 Node。

数据都先出**原始形态**（只带游戏世界坐标）进 `data/raw/`，再由**入库**步骤进 `data/`：带坐标的按 BaseNav 的 zone 表投影，其余原样复制。原始形态就是上游发布的形态——数据 CI 生成并发布的就是它，MaaEnd 这边下载后自己完成投影与复制。

数据树也在本目录下：

| 目录 | 放什么 | 入不入库 |
| --- | --- | --- |
| `data/raw/` | 原始形态：下载来的（只有世界坐标）与本地生成的结果 | 不入库 |
| `data/` | 可以直接用的数据：投影后的，以及原样复制的 | 入库 |

## 子命令

| 子命令 | 作用 |
| --- | --- |
| `generate` | 从本地 TableCfg / JsonData 生成到 `data/raw/`，再入库（`--raw-only` 时只出原始形态） |
| `fetch` | 从 zmdmap 下载已发布的数据到 `data/raw/`，再入库（`--raw-only` 时只下载） |

不带子命令时只打印帮助。`fetch` 的两个步骤各由一个开关控制：`--download/--no-download`、`--raw-only`。

```shell
uv run zmdmap                                     # 打印帮助
uv run zmdmap generate                            # 生成到 data/raw/ 并入库到 data/
uv run zmdmap generate --raw-only                 # 只出原始形态（发布用的就是这一份）
uv run zmdmap generate --list                     # 列出数据集名、文件名与入库方式（每行一个）
uv run zmdmap generate --only collect_points      # 只生成一份（可重复给：--only a --only b）
uv run zmdmap generate --out-dir /tmp/x --force
uv run zmdmap fetch                               # 下载已发布数据并入库
uv run zmdmap fetch --no-download                 # 不下载，只用现有 data/raw/ 重新入库
```

`python -m tools.zmdmap` 与 `uv run zmdmap` 等价。

## 文件职责

### 生成：精简游戏数据（`datasets/`）

`datasets/__init__.py` 的 `DATASETS` 登记了每份数据的顺序、文件名与**入库方式**（`materialize`）；`uv run zmdmap generate` 按这个顺序逐个调用 `build_*`，把原始形态写进 `--out-dir`。每个 `build_*` 的入参都是 `(table_cfg_dir, json_data_dir)`，只从本地兄弟仓库取数据（只用到其中一个目录的也照这个签名收）。

| 文件 | 数据 | 入库 |
| --- | --- | --- |
| `datasets/delivery_destinations.py` | `delivery_destinations.json` | 投影 |
| `datasets/teleport_anchors.py` | `teleport_anchors.json` | 投影 |
| `datasets/collect_points.py` | `collect_points.json` | 投影 |
| `datasets/delivery_jobs.py` | `delivery_jobs.json` | 复制 |
| `datasets/environment_monitoring.py` | `environment_monitoring.json` | 复制 |
| `datasets/sell_product.py` | `sell_product.json` | 复制 |
| `datasets/essence.py` | `energy_point_gems.json`（基质筛选的能量淤积点） | 复制 |
| `datasets/weapons.py` | `weapons.json`（基质筛选与图标识别共用的武器表） | 复制 |
| `datasets/items.py` | `item_mini_table.json`（图标识别的物品表，带多语言名字） | 复制 |

要加一份新数据，就在 `datasets/` 加一个同签名的 `build_*`，再在 `DATASETS` 里登记一行并选好入库方式。

### 常量（`constants.py`）

跨模块共用的常量：各语言的 key 写法（文本表后缀 `CN` / MaaEnd locale 名 `zh_cn`）、zmdmap 版本接口与发布地址。语言只写映射表，其余由它们推出来，避免各处各抄一份。

### 共用件（`utils/`）

| 文件 | 说明 |
| --- | --- |
| `utils/game_data.py` | 读 TableCfg / GameplayConfig 与各语言文本表 |
| `utils/coords.py` | 世界坐标读取与 `coord` 文案 |
| `utils/levels.py` | ID 前缀 → 关卡 → 底图 |

### 下载与入库

| 文件 | 说明 |
| --- | --- |
| `fetch.py` | 版本接口 + 发布 CDN 的下载与缓存（原 `fetch-data.mjs`） |
| `project.py` | 带坐标的数据按 BaseNav zone 表投影、其余原样复制（原 `project.mjs`） |
| `navmesh.py` | 从 `BNAV` / `BGEO` 二进制 nav 数据里读出 zone 表 |
| `paths.py` | 输入 / 输出路径的默认值与环境变量名 |
| `cli.py` | `uv run zmdmap` 的入口，跑 `datasets/` 里的 `build_*` / 下载 / 入库 |

入库 JSON 用 Python 的 `json` 序列化（`ensure_ascii=False`、缩进 4），并且统一写 LF（仓库的 `.gitattributes` 是 `eol=lf`）。与原 Node 实现只在数字写法上有差别（整型浮点写成 `520.0` 而不是 `520`），值与键顺序一致——移植时已用原 JS 投影器逐份比对过解析后的结构。

## `generate` 的开关

生成默认接着入库，用开关提前收尾或做别的事：

| 开关 | 作用 |
| --- | --- |
| `--sync` | 生成前先同步兄弟仓库（只对干净工作区做快进合并） |
| `--raw-only` | 只出原始形态（只写 `--out-dir`），不做入库 |
| `--dry-run` | 只报告会做什么（不合并、不生成、不入库） |

只对干净的工作区做快进合并，绝不 stash / reset / 覆盖本地改动；本地重算不会改 `data/version.txt`（那是「已发布数据版本」的标记）。

`assets/resource/model` 子模块不参与同步：投影用的 navmesh 一律取当前仓库里检出的那一版。

## 输入路径（`generate`）

TableCfg 与 JsonData 默认取**与 MaaEnd 仓库平级的兄弟仓库**（由 `--workspace` 定位），可用环境变量或 CLI 参数覆盖。优先级：**命令行 > 环境变量 > 默认**（由 click 的 `envvar` / `default` 保证，实际生效的来源会在日志第一行打印出来）。

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

CI 只需要出数据时用 `uv run --no-default-groups zmdmap generate --raw-only`：`click` 在核心依赖里，这样不会顺带安装 `scripts` 组（maafw / fastapi / numpy 等）。

## 产出

产物都先落在**不入库**的 `tools/zmdmap/data/raw/`，只带游戏世界坐标；带坐标的由入库步骤按 BaseNav 的 zone 表算出底图像素，进入库的 `tools/zmdmap/data/*.json`，其余原样复制过去。上游发布的数据就是这个原始形态，`fetch` 下载完必须先入库才能被各任务生成器读取。

整条链路（同步兄弟仓库 → 本地生成 → 入库）也在本入口：

```shell
uv run zmdmap generate --sync                     # 先同步兄弟仓库，再生成并入库
uv run zmdmap generate --sync --raw-only          # 同步、生成，只要原始形态
uv run zmdmap generate --dry-run                  # 只报告会做什么
uv run zmdmap fetch                               # 下载 → 入库（等同 pnpm fetch:zmdmap）
```

## 读取本地数据

数据入口只有 `utils/game_data.py` 里的几个函数：

| 函数 | 读什么 |
| --- | --- |
| `read_table_cfg(dir, name)` | `<table_cfg_dir>/<name>.json` |
| `read_gameplay_config(dir, name)` | `<json_data_dir>/GameplayConfig/<name>.json` |
| `localized_text(dir, text_id, lang)` | `<table_cfg_dir>/I18nTextTable_<后缀>.json` |
| `ref_texts(dir, ref)` / `ref_text(dir, ref, lang)` | 表里 `{"id", "text"}` 文本引用的各语言 / 单语文本 |

读出来的 JSON 会换成属性可访问的 `Record`，`build_*` 里直接写 `entry.position.x`；几个函数都按「目录 + 文件名」缓存，多份数据共用同一次读取。

## 校验

参照物是入库的 `tools/zmdmap/data/`：拿同一份本地兄弟仓库重算一遍，产物应当与它逐字节一致。

```shell
uv run zmdmap generate --force --out-dir <临时目录> --data-dir <临时目录>
```

再逐份比对 `tools/zmdmap/data/` 下的同名文件。只改注释、重命名这类不动输出的改动，应当零差异；动了取数逻辑就看差异是否符合预期。注意入库数据是「上一次生成（或下载）的快照」，与已发布版本不一定是同一版（`data/version.txt` 记的是发布版本）。
