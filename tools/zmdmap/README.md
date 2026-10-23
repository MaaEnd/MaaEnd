# zmdmap：MaaEnd 用的精简游戏数据

MaaEnd 的几个任务要读游戏里的物品名、商店货架、采集点、传送点这些东西。它们被整理成一组 JSON，放在
`tools/zmdmap/data/`，这个工具负责把 JSON 产出来。

产出有两条路，最后都落到同一个地方：

```text
本地解包出来的游戏数据 ──generate──┐
                                  ├──> data/raw/（原始形态）──> data/（入库，任务读的就是它）
上游发布好的数据 ──────fetch───────┘
```

- `uv run zmdmap generate`：从本地的游戏数据生成（需要 TableCfg / JsonData 两个目录）。
- `uv run zmdmap fetch`：从上游下载已经发布好的数据（数据 CI 用 `generate` 生成并发布的就是它）。

`python -m tools.zmdmap` 和 `uv run zmdmap` 等价。

## 一次 `fetch` 到底做了什么

1. **取清单。** 上游发布的 `manifest.json` 写着「这批数据是哪一版」和「每份文件的内容指纹」。清单每次都带
   时间戳重新取（它只有几百字节），因为「要不要重新下」全看它，拿旧的判断会整批错。
2. **比指纹。** 拿本地 `data/` 里那份文件自称的 `sha256` 跟清单比：一样就跳过下载，不一样才下。所以本地已经是
   最新时，这一步只剩一次清单请求，什么都不会下载。清单里没有的文件算「这批没发它」：本地有就原样留着，本地
   也没有就不下 —— 需要投影的那几份直接报错，其余提示一句跳过。
3. **下载并核对。** 下载地址只有 `?t=<清单里的完整指纹>` —— 同一份内容永远是同一个地址，内容一变就是新地址，
   远端缓存没机会拿旧的顶替；版本号不进地址（CDN 不认它，只用来写日志和标签）。每份下回来先算内容指纹跟清单
   比，对不上就报错、不写文件 —— 免得坏内容配着好指纹进仓库，之后再也比不出来。这里不重试：地址都由指纹决定，
   再取还是同一份。
4. **写进 `data/raw/`。** 这一步落的还是「原始形态」（只有游戏世界坐标）。
5. **入库。** 带坐标的按 BaseNav 的 zone 表把世界坐标换成底图像素，其余原样复制，写进 `data/`。

如果第 2 步发现一份都不用下，第 5 步直接跳过：`data/` 里本来就是那一批，拿空的 `data/raw/` 重做反而会把
文件改回旧内容。

`--dry-run` 只走前两步，列出这次会下哪几份就停，不下载也不写文件。

## 一次 `generate` 到底做了什么

1. **确认输入。** TableCfg（表格）和 JsonData（关卡地图、交互模板）默认取 MaaEnd 旁边同级的兄弟仓库
   `BeyondTableCfg` / `BeyondMemoryPack`，也可以用 `--table-cfg-dir` / `--json-data-dir` 指到别处。
2. **定版本。** 每份数据最前面写的 `version` 必须是完整的「客户端版本 + 数据构建号」，比如
   `1.6.4+11262943-8`；凑不齐就直接报错，不生成版本不全的数据。发布端从版本接口拿到这两个号，用 `--version`
   显式传进来；本地不给 `--version` 时，客户端版本看兄弟仓库的提交标题（`Client:` 那段），构建号优先看
   `.beyond/ledger`（revert 提交也骗不过它），没有 ledger 就看提交标题的 `Data:`。数据目录路径里的
   `initial_…_main_…` 只有构建号，单独用不了。
3. **生成原始形态。** 11 份数据各自从表格里取数，结果写进 `data/raw/`（只有世界坐标），同时算好 `sha256`。
   顺手写一份 `manifest.json`，说明这次发的是哪几份、每份的内容指纹 —— 就是 `fetch` 第 1 步看的那份。
4. **入库。** 和 `fetch` 第 5 步完全一样。

`--raw-only` 只做前 3 步（发布上游要的就是这一份）；`--sync` 会先把兄弟仓库快进到最新（只快进，不会 stash
或者覆盖本地改动）；`--dry-run` 打印完计划就退出。

## 数据文件长什么样

```json
{
    "version": "1.6.4+11262943-8",
    "sha256": "…",
    "…": "数据本体"
}
```

前面两个字段只在生成原始形态时定下来：

- `version`：给人看的版本标签（客户端版本 + 数据构建号，同一版客户端下可能发多批）。下载和入库都只照抄。
- `sha256`：**原始形态**的内容指纹。`fetch` 就是拿它跟清单比，决定要不要重新下载。

入库会把本体改成底图像素，但这两个字段一个字不改 —— 所以 `data/raw/` 和 `data/` 里同名文件的 `sha256`
是同一个值，能看出「这份入库数据是从哪一份原始数据来的」。

别的工具要读这些数据，用 `from tools.zmdmap import read_payload`，它会去掉这两个字段只给本体，不要自己解析。

## 原始形态和入库为什么分两步

游戏里给的是世界坐标（`x` / `z`，另有一个世界高度 `y`），MaaEnd 识别用的是底图像素。两边的换算参数
（`zone` / `sx` / `tx` / `sy` / `ty`）在 BaseNav 数据包里，只有 MaaEnd 手上有，所以上游只发布世界坐标，投影由
MaaEnd 自己完成：

```text
u = sx * x + tx
v = -sy * z + ty
```

`y`（世界高度）原样保留，任务可以拿它当终点的高度。投影之后，用到的 zone 参数会写进这份数据的 `maps` 里，
方便反查。

分成两步的好处是：上游不用管 MaaEnd 的底图尺寸；MaaEnd 换底图时只要重跑入库
（`uv run zmdmap fetch --no-download`），不必重新下载，也不必麻烦上游重新发布。

## 常用命令

```shell
uv run zmdmap                                     # 看帮助
uv run zmdmap fetch                                # 下载最新数据并入库（CI 就是这么跑的）
uv run zmdmap fetch --only sell_product            # 只要这一份
uv run zmdmap fetch --no-download                  # 不下载，拿现有 data/raw/ 重新入库
uv run zmdmap generate                             # 从本地游戏数据生成并入库
uv run zmdmap generate --raw-only                  # 只出原始形态（发布用的那一份）
uv run zmdmap generate --version 1.6.4+11262943-8  # 手动指定版本
uv run zmdmap generate --list                      # 列出有哪些数据、各自怎么入库
```

两个子命令都支持 `--only`（可重复给）、`--dry-run`、`--force` 和 `--nav`。

## 目录里有什么

| 文件 | 干什么 |
| --- | --- |
| `cli.py` | 命令入口，挂上 `generate` / `fetch` 两个子命令 |
| `generate.py` | `generate` 子命令：生成原始形态、写版本与清单，再入库 |
| `fetch.py` | `fetch` 子命令：读清单、下载、核对内容，再入库 |
| `materialize.py` | 入库：带坐标的投影，其余原样复制 |
| `datasets/` | 11 份数据，一个模块一份；`base.py` 是它们的基类 |
| `utils/` | 共用件，见下表 |
| `data/raw/` | 原始形态，本地缓存（不进 git），删掉不影响判断 |
| `data/` | 入库后的数据，任务读的就是这里，进 git |

`utils/` 里各件的分工：

| 文件 | 干什么 |
| --- | --- |
| `config.py` | 路径、语言代号映射、发布地址 |
| `log.py` | 命令行输出：每行带 `[zmdmap]` 前缀，`section` 打横幅，终端里自动上色 |
| `http.py` | 带重试的 GET；下载文件（来源没变就跳过） |
| `navmesh.py` | 从 BaseNav 的二进制数据里读出投影参数 |
| `project.py` | 投影本身：换坐标、查越界、汇总用到的 zone |
| `datafile.py` | 数据文件的读写、元数据、内容指纹 |
| `version.py` | 版本标签怎么拼、怎么比新旧、从哪儿读 |
| `workspace.py` | 兄弟仓库的同步、生成前的检查、看 `data/` 有哪些改动 |
| `tables.py` | 游戏表格的结构定义与类型化解码 |
| `game_data.py` | 多语言文本、世界坐标、关卡归属 |
| `errors.py` | `LocalDataError`：本地数据缺失或形状不对 |
| `shapes.py` | 少数「必须原样保留未知字段」的地方用的收窄助手 |

## 有哪 11 份数据

| 数据 | 内容 | 入库方式 |
| --- | --- | --- |
| `collect_points.json` | 可拾取 / 可打碎的采集物 | 投影 |
| `delivery_destinations.json` | 送货目的地：收货 NPC、回收站、仓储节点 | 投影 |
| `teleport_anchors.json` | 营地（传送锚点） | 投影 |
| `delivery_jobs.json` | 运送委托：地区、仓储节点、能装的物品 | 复制 |
| `energy_point_gems.json` | 基质筛选的能量淤积点 | 复制 |
| `environment_monitoring.json` | 环境监测的终端与观察点 | 复制 |
| `item_mini_table.json` | 背包物品 → 图标、分类、五语名字 | 复制 |
| `operator_growth.json` | 干员培养到目标状态要的素材 | 复制 |
| `sell_product.json` | 据点售卖物品与匹配干员 | 复制 |
| `stock_staple.json` | 地区商店会周期补货的物资 | 复制 |
| `weapons.json` | 武器（含还没上线的前瞻武器） | 复制 |

`uv run zmdmap generate --list` 打印每个数据集的名字、产物文件名和入库方式。

## 想加一份新数据

1. 在 `datasets/` 下新建一个模块，名字就是数据名（也决定产物文件名）。
2. 在里面定义 `Dataset` 的子类，实现 `build(table_cfg_dir, json_data_dir)` —— 只用得到一个目录的也照这个签名写。
3. 需要投影就把 `needs_projection` 设成真，并用 `projection_containers` 列出条目数组在数据里的键；只读
   TableCfg 的话不用动 `requires_json_data`（默认就是「不读 JsonData」）。

`datasets/__init__.py` 会自己扫到这个模块，没有需要手工维护的登记表。多语言名字、关卡索引这些各家都要的
手法写在基类上（`localized_names` / `level_index`），不用各抄一份。

## 出问题的时候

日志每行都带 `[zmdmap]`，失败和警告走标准错误，退出码是 1。常见提示和对策：

| 提示 | 怎么处理 |
| --- | --- |
| `缺少 TableCfg 目录` / `缺少 JsonData 目录` | 用 `--table-cfg-dir` / `--json-data-dir`（或环境变量）指到游戏数据解包后的目录 |
| `上游还没有发布 xxx.json，跳过` | 那一份上游还没发，本地可以先 `uv run zmdmap generate --only xxx` 生成 |
| `清单（…）里没有这一份，本地也没有` | 上游这批没发它，等发完再 fetch；本地也可以先 `uv run zmdmap generate --only xxx` |
| `与清单不符` | 上游发布的那份和清单写的不一致（地址由指纹决定，所以不是缓存问题）；确认内容没问题后可以加 `--force` 照收，或者等上游重新发布 |
| `上游还没有发布 manifest.json` | 上游这一批还没发完，先别 fetch |
| `没带 version 字段` | 上游发布时就没写版本，只能照抄；要版本标签就得让发布端生成时带上 `--version` |
| `拿不到版本` | 兄弟仓库与数据目录路径里都没有版本，用 `--version` 指定完整标签（如 `1.6.4+11262943-8`） |
| `版本不完整` | 只凑到构建号或客户端版本，用 `--version` 把两半补全 |
| `投影落在 … 底图外` | 几份输入来自不同版本，先 `uv run zmdmap generate --sync` 或重新解包 |
| `没有 xxx 对应的 zone` | 那份数据里有新底图，本地 BaseNav 还没更新（`assets/resource/model` 子模块） |

`data/*.json` 的更新由数据 CI 负责：本地跑完之后 `git status` 里看到的改动，先确认是不是你这次要提交的，
别把顺手重跑的结果带进无关的 PR。

## 约定

- 数据文件的 JSON 写法：缩进 4、`ensure_ascii=False`、LF 换行；整型浮点会写成 `520.0`（值没有变）。
- 命令行输出的每一行都带 `[zmdmap]` 前缀，方便在 CI 日志里筛出来；终端里自动上色，管道和 CI 里是纯文本。
  排查时可以设 `ZMDMAP_DEBUG=1` 看细节，`ZMDMAP_COLOR=1` / `NO_COLOR` 强制开关颜色。
- `version` 只给人看，不参与任何比较；「是不是同一批数据」只看 `sha256`。
- `data/raw/` 是本地缓存，不进 git；`data/` 才是入库产物。
