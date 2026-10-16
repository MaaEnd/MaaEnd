# zmdmap 精简游戏数据（`uv run zmdmap`）

`tools/zmdmap` 是 MaaEnd 各任务所需精简游戏数据的统一入口：既能从本地游戏数据**生成**，也能从 zmdmap **下载**已发布数据，再入库成可以直接用的形式。整套工具是 Python：入口是 `uv run zmdmap`（等价 `python -m tools.zmdmap`）。

数据都先出**原始形态**（只带游戏世界坐标）进 `data/raw/`，再由**入库**步骤进 `data/`：带坐标的把世界坐标按 BaseNav 的 zone 表**原位**换成底图像素（`x` 的位置给 `u`、`z` 的位置给 `v`，其余字段与键序都不动），其余数据原样复制。原始形态就是上游发布的形态——数据 CI 生成并发布的就是它，MaaEnd 这边下载后自己完成投影与复制。

每份数据的**最前面**写着两个元数据字段：`version`（来自哪一版游戏数据）与 `sha256`（内容指纹，见[产出](#产出)）。所以不必另记「本地是哪一版、内容是哪一份」：下载时按发布清单里的内容指纹逐份判断要不要重下，本地重算沿用原有版本、`--version` 或兄弟仓库能给出版本时就更新它。

数据树也在本目录下：

| 目录 | 放什么 | 入不入库 |
| --- | --- | --- |
| `data/raw/` | 原始形态：下载来的（只有世界坐标）与本地生成的结果 | 不入库 |
| `data/` | 可以直接用的数据：投影后的（x/z 原位换成 u/v），以及原样复制的 | 入库 |

## 子命令

| 子命令 | 作用 |
| --- | --- |
| `generate` | 从本地 TableCfg / JsonData 生成到 `data/raw/`，再入库（`--raw-only` 时只出原始形态） |
| `fetch` | 从 zmdmap 下载已发布的数据到 `data/raw/`，再入库（`--raw-only` 时只下载） |

不带子命令时只打印帮助。`fetch` 的两个步骤各由一个开关控制：`--download/--no-download`、`--raw-only`。两个子命令的 `--only` 都只处理列出的数据集；对 `fetch` 来说，它同时决定**下哪几份**与入库哪几份（缺省全部），各任务生成器就靠它只取自己用得到的数据，不必每次把整包拖下来。

```shell
uv run zmdmap                                     # 打印帮助
uv run zmdmap generate                            # 生成到 data/raw/ 并入库到 data/
uv run zmdmap generate --raw-only                 # 只出原始形态（发布用的就是这一份）
uv run zmdmap generate --version 1.6.4            # 指定这批数据的版本（默认从兄弟仓库读）
uv run zmdmap generate --list                     # 列出数据集名、文件名与入库方式（每行一个）
uv run zmdmap generate --only collect_points      # 只生成一份（可重复给：--only a --only b）
uv run zmdmap generate --out-dir /tmp/x --force
uv run zmdmap fetch                               # 下载已发布数据并入库
uv run zmdmap fetch --only sell_product            # 只下载 → 只入库这一份
uv run zmdmap fetch --no-download                 # 不下载，只用现有 data/raw/ 重新入库
```

`python -m tools.zmdmap` 与 `uv run zmdmap` 等价。

## 文件职责

### 生成：精简游戏数据（`datasets/`）

`datasets/` 下**一个模块就是一份数据**：模块名既是数据集名，也是产物文件名（`datasets/collect_points.py` 生成 `collect_points.json`）。每个模块定义一个 `datasets/base.py` 的 `Dataset` 子类，实现 `build(table_cfg_dir, json_data_dir)`（只用到其中一个目录的也照这个签名收），并用 `needs_projection` 声明入库时要不要投影；`datasets/__init__.py` 扫包把这些子类收成 `DATASETS`（需要投影的在前，其余按名字排），`uv run zmdmap generate` 按这个顺序逐个调用 `build`，把原始形态写进 `--out-dir`——没有需要手工维护的登记表。需要投影的数据在子类上用 `projection_containers` 列出条目数组所在的键，`project.py` 直接读这份声明，两边不各存一份。

| 文件 | 数据 | 入库 |
| --- | --- | --- |
| `datasets/collect_points.py` | `collect_points.json` | 投影 |
| `datasets/delivery_destinations.py` | `delivery_destinations.json` | 投影 |
| `datasets/teleport_anchors.py` | `teleport_anchors.json` | 投影 |
| `datasets/delivery_jobs.py` | `delivery_jobs.json` | 复制 |
| `datasets/energy_point_gems.py` | `energy_point_gems.json`（基质筛选的能量淤积点） | 复制 |
| `datasets/environment_monitoring.py` | `environment_monitoring.json` | 复制 |
| `datasets/item_mini_table.py` | `item_mini_table.json`（图标识别的物品表，带多语言名字） | 复制 |
| `datasets/sell_product.py` | `sell_product.json` | 复制 |
| `datasets/weapons.py` | `weapons.json`（基质筛选与图标识别共用的武器表） | 复制 |

要加一份新数据，就在 `datasets/` 加一个同名模块、定义 `Dataset` 子类并实现 `build`，再声明要不要投影（需要投影的用 `projection_containers` 列出条目数组所在的键）。

各份数据共用的手法收在 `Dataset` 上，不用各抄一份：`localized_names`（表里的文本引用 → 五语字典，五语缺一即报错）、`level_index`（读 `LevelBasicInfoTable` 建 `idNum -> levelId`），组装错误统一用 `DatasetError`（cli 会补上数据集名）。取数逻辑在各份数据的模块里。

### 常量（`constants.py`）

跨模块共用的常量：各语言的 key 写法（文本表后缀 `CN` / MaaEnd locale 名 `zh_cn`）、zmdmap 版本接口与发布地址。语言只写映射表，其余由它们推出来，避免各处各抄一份。

### 共用件（`utils/`）

| 文件 | 说明 |
| --- | --- |
| `utils/game_data.py` | 读 TableCfg / GameplayConfig 与各语言文本表 |
| `utils/coords.py` | 世界坐标读取 |
| `utils/levels.py` | ID 前缀 → 关卡 → 底图 |

### 下载与入库

| 文件 | 说明 |
| --- | --- |
| `fetch.py` | 读发布清单（没有就问版本接口）+ 按数据集下载与缓存 |
| `project.py` | 带坐标的数据按 BaseNav zone 表把 x/z 原位换成 u/v、其余原样复制 |
| `navmesh.py` | 从 `BNAV` / `BGEO` 二进制 nav 数据里读出 zone 表 |
| `paths.py` | 输入 / 输出路径的默认值与环境变量名 |
| `cli.py` | `uv run zmdmap` 的入口，跑 `datasets/` 里的数据集 / 下载 / 入库 |

入库 JSON 用 Python 的 `json` 序列化（`ensure_ascii=False`、缩进 4），并且统一写 LF（仓库的 `.gitattributes` 是 `eol=lf`）。整型浮点会带小数点（`520.0` 而不是 `520`），解析出来的值与键顺序不受影响。

要下哪几份由发布清单（`<DATA_BASE_URL>/manifest.json`）说了算：清单给出「这批数据是哪一版」与「每份原始形态的内容指纹」，`fetch` 逐份比指纹——一致的跳过下载（只是版本号变了就按清单改写文件里的 `version`），不一致的才下，下完再核对一遍拿到的内容。清单还没发布时退回版本接口，按文件里的 `version` 逐份比。于是既不必问版本接口，也认得「同一个版本号下重新发布过」；请求上的 `t` 参数优先用清单给的指纹，同一份内容的 URL 稳定、内容一变就是新 URL，自然绕开远端缓存。

清单本身每次都**带一个时间戳**去取（它只有几百字节，回源不心疼）：它决定了后面下哪几份，被远端缓存住就等于拿旧索引判断，整批都得跟着错。实测这个 CDN 只看 URL —— 请求头带 `Cache-Control: no-cache` 也照样命中缓存（`Age` 不变），换个带时间戳的 URL 才真的回源。数据文件不需要靠它区分新老：它们的 URL 自带清单给的内容指纹，内容一变就是新 URL。

所以本地与 CI 是同一套判断：先取清单，清单里的指纹变了才下，且只下变了的那些。`--force` 是唯一的手动手段：不看清单直接重下这几份，并把请求 URL 上的指纹换成时间戳，把「不走远端缓存」贯彻到底。

## `generate` 的开关

生成默认接着入库，用开关提前收尾或做别的事：

| 开关 | 作用 |
| --- | --- |
| `--sync` | 生成前先同步兄弟仓库（只对干净工作区做快进合并） |
| `--version` | 指定写进每份数据的版本（默认从兄弟仓库的提交标题 `Client:x.y.z` 读，兄弟仓库不在工作区里时读 TableCfg 所在仓库） |
| `--raw-only` | 只出原始形态（只写 `--out-dir`），不做入库 |
| `--dry-run` | 只报告会做什么（不合并、不生成、不入库） |

只对干净的工作区做快进合并，绝不 stash / reset / 覆盖本地改动；本地重算不会凭空改掉版本号（版本写在每份数据里，见上）。

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

产物都先落在**不入库**的 `tools/zmdmap/data/raw/`，只带游戏世界坐标；带坐标的由入库步骤按 BaseNav 的 zone 表把 `x` / `z` 原位换成底图像素 `u` / `v`，进入库的 `tools/zmdmap/data/*.json`，其余原样复制过去。上游发布的数据就是这个原始形态，`fetch` 下载完必须先入库才能被各任务生成器读取。

数据文件里只放数据字段，不带字段说明：字段含义写在 `datasets/` 的模块与各任务文档里。例外是最前面的两个元数据字段：

- `version`：这份数据来自哪一版游戏数据。`generate` 从兄弟仓库的提交标题读（或 `--version` 指定），本地重算沿用原有值；`fetch` 用它逐份判断要不要重下。
- `sha256`：**数据本体**（不含这两个字段）的内容指纹。写入时按这一份的本体重算——本地生成按生成结果、下载的按下载到的内容、入库投影后按投影后的本体重算，所以带坐标的那几份入库前后的指纹不同。算法固定：本体按键名排序后紧凑序列化（`ensure_ascii=False`、`separators=(",", ":")`）再取 UTF-8 字节的 sha256，于是重新排版、调整键序都不换指纹，内容或数组顺序一变就换。它只用来说明「内容是不是同一份」，不参与业务；`fetch` 拿到带指纹的发布文件时会顺手核对一遍。

有了这两个字段，就不必再另外维护「本地是哪一版、内容是哪一份」的记录。

原始形态的目录里还多一份 **发布清单 `manifest.json`**（版本 + 每份原始形态的指纹），由 `generate` 顺手写出、跟着原始形态一起发布，不入库：它是发布端与 `fetch` 之间的约定，消费端要版本 / 指纹直接看数据文件自己的那两个字段即可。

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

读出来的 JSON 会换成属性可访问的 `Record`，`Dataset.build` 里直接写 `entry.position.x`；几个函数都按「目录 + 文件名」缓存，多份数据共用同一次读取。

## 给其他工具读数据

数据文件最前面的 `version` / `sha256` 是元数据，别的工具不必自己跳过它们：`tools.zmdmap` 直接提供两个入口。

| 入口 | 作用 |
| --- | --- |
| `read_payload(path)` | 读数据文件并去掉 `version` / `sha256`，返回数据本体；文件缺失或不是合法 JSON 时给出明确报错 |
| `payload_of(data)` | 只做「去掉 `version` / `sha256`」这一步；非对象的数据原样返回 |

两者都从包上直接 import（`from tools.zmdmap import read_payload`）。EssenceFilter 与 IconRecognition 的生成脚本就是这么读 `weapons.json` / `item_mini_table.json` / `energy_point_gems.json` 的。

## 校验

参照物是入库的 `tools/zmdmap/data/`：拿同一份本地兄弟仓库重算一遍，产物应当与它逐字节一致。

```shell
uv run zmdmap generate --force --out-dir <临时目录> --data-dir <临时目录>
```

再逐份比对 `tools/zmdmap/data/` 下的同名文件。只改注释、重命名这类不动输出的改动，应当零差异；动了取数逻辑就看差异是否符合预期。注意入库数据是「上一次生成（或下载）的快照」：各份数据可能停在不同版本上，要确认某一份是哪一版就看它自己的 `version` 字段。
