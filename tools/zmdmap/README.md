# zmdmap：MaaEnd 用的精简游戏数据

MaaEnd 的几个任务要读游戏里的物品名、商店货架、采集点、传送点这些东西。它们被整理成一组 JSON，放在
`tools/zmdmap/data/`，这个工具负责把 JSON 产出来。

该文档更新于 2026 年 10 月 29 日。

产出有两条路，最后都落到同一个地方：

```text
本地解包出来的游戏数据 ──generate──┐
                                  ├──> data/raw/（原始形态）──> data/（入库，任务读的就是它）
上游发布好的数据 ──────fetch───────┘
```

- `uv run zmdmap generate`：从本地的游戏数据生成（每份数据都读 TableCfg；`collect_points` / `delivery_destinations` / `energy_points` / `teleport_anchors` 还要 JsonData，`region3d` 还要 Lua 脚本与解包资源）。
- `uv run zmdmap fetch`：从上游下载已经发布好的数据（数据 CI 用 `generate` 生成并发布的就是它）。

`python -m tools.zmdmap` 和 `uv run zmdmap` 等价。

## 本地生成与远端同步的约定

本地已经具备生成所需的游戏数据时，新增或调整导出字段直接运行 `generate`，再用生成结果更新下游任务。发布端尚未更新这些字段时，不运行 `fetch` 拉取旧产物；等对应数据发布后，再使用正常的下载流程。

不为发布端尚未更新的过渡期增加兼容逻辑，包括补齐旧字段、合并旧数据或特殊下载保护。本地信息已经齐全，应直接从输入重新生成，而不是迁就尚未更新的发布产物。

## 一次 `fetch` 到底做了什么

1. **取清单。** 上游发布的 `manifest.json` 写着「这批数据是哪一版」和「每份文件的内容指纹」。清单每次都带
   时间戳重新取（它只有几百字节），因为「要不要重新下」全看它，拿旧的判断会整批错。
2. **比版本和指纹。** 清单里的 `version` 是必填项：没写就直接报错、不下，不退回「只比指纹」（判断不了本地这份是不是比上游新，可能把新内容换成旧内容）。有版本时先看版本：本地 `data/` 里那份文件自称的 `version` 比清单新时保持本地原样（上游这份是旧的：发布端回滚过，或者本地已经按当前代码生成过、上游还没重新发布），要覆盖得显式 `--force`。版本相同或更旧时看指纹：本地自称的 `sha256` 跟清单一样就跳过下载，不一样才下。所以本地已经最新时，这一步只剩一次清单请求，什么都不会下载。清单里没有的文件算「这批没发它」：本地有就原样留着，本地也没有就不下 —— 需要投影的那几份直接报错，其余提示一句跳过。
3. **下载并核对。** 下载地址只有 `?t=<清单里的完整指纹>` —— 同一份内容永远是同一个地址，内容一变就是新地址，
   远端缓存没机会拿旧的顶替；版本号不进地址（它用于比较新旧，CDN 缓存仍只按指纹区分内容）。每份下回来先算内容指纹跟清单
   比，对不上就报错、不写文件 —— 免得坏内容配着好指纹进仓库，之后再也比不出来。这里不重试：地址都由指纹决定，
   再取还是同一份。
4. **写进 `data/raw/`。** 这一步落的还是「原始形态」（只有游戏世界坐标）。
5. **入库。** 带坐标的按 BaseNav 的 zone 表把世界坐标换成底图像素，其余原样复制，写进 `data/`。

如果第 2 步发现一份都不用下，第 5 步直接跳过：`data/` 里本来就是那一批，拿空的 `data/raw/` 重做反而会把
文件改回旧内容。

`--dry-run` 只走前两步，列出这次会下哪几份就停，不下载也不写文件。

## 一次 `generate` 到底做了什么

1. **确认输入。** TableCfg（表格）默认取 MaaEnd 旁边同级的兄弟仓库 `BeyondTableCfg`，也可以用
   `--table-cfg-dir` 指到别处；要读关卡地图与交互模板的那四份（`collect_points` /
   `delivery_destinations` / `energy_points` / `teleport_anchors`）还要 JsonData，默认取 `BeyondMemoryPack/JsonData`，
   用 `--json-data-dir` 覆盖。`region3d` 还要解包出的 Lua 脚本与 Unity 资源，默认取
   `BeyondLua/LuaScripts` / `BeyondImage/assets`，用 `--lua-dir` / `--assets-dir` 覆盖。
   用不到的输入既不检查也不索要：缺什么、怎么补，都由这次选中的数据集声明决定。
2. **定版本。** 每份数据最前面写的 `version` 必须是完整的「客户端版本 + 数据构建号」，比如
   `1.6.4+11262943-8`；凑不齐就直接报错，不生成版本不全的数据。发布端从版本接口拿到这两个号，用 `--version`
   显式传进来；本地不给 `--version` 时，客户端版本看兄弟仓库的提交标题（`Client:` 那段），构建号优先看
   `.beyond/ledger`（revert 提交也骗不过它），没有 ledger 就看提交标题的 `Data:`。数据目录路径里的
   `initial_…_main_…` 只有构建号，单独用不了。
3. **生成原始形态。** 12 份数据各自从输入里取数，结果写进 `data/raw/`，同时算好 `sha256`。
   顺手写一份 `manifest.json`，说明这批数据的版本、这次发的是哪几份和每份的内容指纹 —— 就是 `fetch` 第 1 步看的那份。版本必填，`fetch` 靠它判断本地那份比上游新还是旧。
4. **入库。** 和 `fetch` 第 5 步完全一样。

`--raw-only` 只做前 3 步（发布上游要的就是这一份）；`--sync` 会先把四个兄弟仓库快进到最新——`BeyondTableCfg`、
`BeyondMemoryPack`，以及 `region3d` 要读的 `BeyondLua`、`BeyondImage`（只快进，不会 stash 或者覆盖本地改动；
资源仓库 20 GB 以上，这一步会连它的 LFS 内容一起更新，慢是正常的）；`--dry-run` 打印完计划就退出。

## 数据文件长什么样

```json
{
    "version": "1.6.4+11262943-8",
    "sha256": "…",
    "…": "数据本体"
}
```

前面两个字段只在生成原始形态时定下来：

- `version`：完整游戏版本（客户端版本 + 数据构建号，同一版客户端下可能发多批）。`fetch` 用它比较本地与清单的新旧；下载和入库保留原值。同一游戏版本新增导出字段时仍用真实版本，不另编版本号。
- `sha256`：**原始形态**的内容指纹，由 `with_metadata()` 根据数据本体计算；新增或修改字段后必须重算。`fetch` 拿它跟清单比，决定要不要重新下载，并核对下载内容。

入库时，带坐标的本体会转换成底图像素，这两个字段仍原样保留，用于追溯入库数据来自哪份原始数据。

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
uv run zmdmap generate --only region3d \
  --lua-dir ../BeyondLua/LuaScripts --assets-dir ../BeyondImage/assets  # 只要地区总览
```

两个子命令都支持 `--only`（可重复给）、`--dry-run`、`--force` 和 `--nav`。

## 目录里有什么

| 文件 | 干什么 |
| --- | --- |
| `cli.py` | 命令入口，挂上 `generate` / `fetch` 两个子命令 |
| `generate.py` | `generate` 子命令：生成原始形态、写版本与清单，再入库 |
| `fetch.py` | `fetch` 子命令：读清单、下载、核对内容，再入库 |
| `materialize.py` | 入库：带坐标的投影，其余原样复制 |
| `datasets/` | 12 份数据，一个模块或一个文件夹一份；`base.py` 是它们的基类 |
| `datasets/region3d/` | 地区总览：模块多，收进同一个文件夹；说明见里面的 `README.md` |
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

## 有哪 13 份数据

| 数据 | 内容 | 入库方式 |
| --- | --- | --- |
| `collect_points.json` | 可拾取 / 可打碎的采集物 | 投影 |
| `delivery_destinations.json` | 送货目的地：收货 NPC、回收站、仓储节点 | 投影 |
| `energy_points.json` | 基质刷取路线的激发点 | 投影 |
| `teleport_anchors.json` | 营地（传送锚点） | 投影 |
| `delivery_jobs.json` | 运送委托：地区、仓储节点、能装的物品、各节点最大基础报价；字段与算法见[数据集注释](datasets/delivery_jobs.py) | 复制 |
| `energy_point_gems.json` | 基质筛选的能量淤积点 | 复制 |
| `environment_monitoring.json` | 环境监测的终端与观察点 | 复制 |
| `item_mini_table.json` | 背包物品 → 图标、分类、五语名字 | 复制 |
| `operator_growth.json` | 干员培养到目标状态要的素材 | 复制 |
| `region3d.json` | 地区总览：每个子区域的 720p 多边形与点击点（另需 Lua / 解包资源） | 复制 |
| `sell_product.json` | 据点售卖物品与匹配干员 | 复制 |
| `stock_staple.json` | 地区商店会周期补货的物资 | 复制 |
| `weapons.json` | 武器（含还没上线的前瞻武器） | 复制 |

`uv run zmdmap generate --list` 打印每个数据集的名字、产物文件名和入库方式（要额外输入的会注明）。

`region3d` 的生成时机、输入和产物形状见 [`datasets/region3d/README.md`](datasets/region3d/README.md)。

## 想加一份新数据

1. 在 `datasets/` 下新建一个模块，名字就是数据名（也决定产物文件名，如 `datasets/foo.py` → `foo.json`）。
   一份数据要拆成好几个模块时改建成文件夹（如 `datasets/region3d/`），文件夹名就是数据名。
2. 在里面定义 `Dataset` 的子类，实现 `@classmethod build`：参数写要用的输入服务，`generate` 按签名注入。
   写类型注解（`table_cfg: TableCfg`、`json_data: JsonData`、`lua: LuaScripts`、`assets: Assets`）或直接
   用同名参数都行，参数名可以和类型不同（`tables: TableCfg` 也认）；只声明自己要的，没写的不会被传进来。
   读表、查文本、读资源都由这些对象负责，数据集里不拼文件系统路径（要换来源就给个同接口的对象）。
   文件夹数据集把子类写在 `__init__.py` 里。
3. 需要投影就把 `needs_projection` 设成真，并用 `projection_containers` 列出条目数组在数据里的键。
   读不读 JsonData / Lua / 解包资源由 `build` 的参数决定：声明了 `json_data` 就会检查 JsonData 目录，
   声明了 `lua` / `assets` 才会检查那两个目录，不用另写开关。

`datasets/__init__.py` 会自己扫到这个模块或文件夹，没有需要手工维护的登记表；`build` 的参数写错了会在
扫包时报出来。多语言名字在基类上（`localized_names`），关卡索引在 `json_data.level_index()`，不用各抄一份。

## 出问题的时候

日志每行都带 `[zmdmap]`，失败和警告走标准错误，退出码是 1。常见提示和对策：

| 提示 | 怎么处理 |
| --- | --- |
| `缺少 TableCfg 目录` / `缺少 JsonData 目录` | 用 `--table-cfg-dir` / `--json-data-dir`（或环境变量）指到游戏数据解包后的目录 |
| `缺少 LuaScripts 目录` / `缺少解包资源目录` | 只有 `region3d` 要它们；用 `--lua-dir` / `--assets-dir`（或环境变量）指到解包结果 |
| `build 参数 'xxx' 没有对应的输入服务` | 数据集自己写错了：改注解（`TableCfg` / `JsonData` / `LuaScripts` / `Assets`）或改成同名参数 |
| `旧 RAW 导出格式不再支持` | `region3d` 只认新包导出（嵌套 GameObject 的 JSON），把 `assets/` 重新解包成新格式 |
| `上游还没有发布 xxx.json，跳过` | 那一份上游还没发，本地可以先 `uv run zmdmap generate --only xxx` 生成 |
| `清单（…）里没有这一份，本地也没有` | 上游这批没发它，等发完再 fetch；本地也可以先 `uv run zmdmap generate --only xxx` |
| `与清单不符` | 上游发布的那份和清单写的不一致（地址由指纹决定，所以不是缓存问题）；确认内容没问题后可以加 `--force` 照收，或者等上游重新发布 |
| `上游还没有发布 manifest.json` | 上游这一批还没发完，先别 fetch |
| `manifest.json 里没有 version` | 上游这批没写版本，判断不了本地新旧：等发布端带上 `--version` 重新生成清单；确认要覆盖本地就加 `--force` 重下 |
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
- `version` 用完整游戏版本比较本地与上游新旧，`sha256` 用来判断和核对内容；同一版本补字段时更新指纹，不伪造游戏版本。
- `data/raw/` 是本地缓存，不进 git；`data/` 才是入库产物。
