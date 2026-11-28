# zmdmap 注意事项

`tools/zmdmap/` 把游戏解包数据精简成 `data/*.json`，供 `tools/pipeline-generate/`、`tools/essence_filter/`、`tools/icon_recognition/` 等读取。数据先生成到 `data/raw/`（只有游戏世界坐标，不进 git），再「入库」到 `data/`：带坐标的换算成底图像素，其余原样复制。原理和全部命令见 [README.md](README.md)。

## 数据文件

- **不手改 `data/*.json`**：它是生成结果，手改会被下次 `fetch` / `generate` 覆盖，开头的 `sha256` 也会对不上。要改内容就改 `datasets/` 里的代码再重新生成。
- 日常跟游戏版本更新由 `.github/workflows/sync-zmdmap.yml` 自动下载并开 PR，不需要手动 `fetch` 再提交。
- 文件开头的 `version`（客户端版本 + 数据构建号）和 `sha256`（原始数据的内容指纹）只由 `generate` 写。数据本体必须是对象，顶层不要用这两个键名。
- Python 里读数据用 `from tools.zmdmap import read_payload`，它会去掉这两个字段；JS 生成器直接读文件，遍历顶层键时要跳过它们。
- 语言键有两套，不要混改：`weapons`、`item_mini_table` 用文本表后缀 `CN` / `TC` / `EN` / `JP` / `KR`，给 Pipeline 生成器用的数据用 `zh_cn` 等 locale 名（见 `utils/config.py`）。
- `data/*.json` 在 `.prettierignore` 里，不要用 Prettier 格式化它。

## 运行 `generate`

- 输入来自放在同一个目录下的兄弟仓库：所有数据都要 `BeyondTableCfg`；带坐标的四份（`collect_points`、`delivery_destinations`、`energy_points`、`teleport_anchors`）还要 `BeyondMemoryPack`；`region3d` 还要 `BeyondLua`、`BeyondImage`。动手前先确认它们在本机存在，缺哪个就明确告诉用户缺了哪个、该放在哪个目录，然后停下等用户处理，**不要**改用 `fetch` 下载的数据顶替。
- 兄弟仓库默认在 MaaEnd 仓库目录的上一级。在 git worktree（如 `.agents/worktrees/…`）里运行时上一级不是那个目录，要用 `--workspace <放兄弟仓库的目录>` 或环境变量 `ZMDMAP_WORKSPACE` 指定。
- 写进数据的 `version` 取自兄弟仓库。生成前对比 `data/<名字>.json` 开头的 `version`：兄弟仓库更旧就会把数据退回旧版本，这时提醒用户先更新兄弟仓库（`--sync` 只做快进合并），不要直接提交。
- 带坐标的数据入库时要读 `assets/resource/model` 子模块里的 navmesh；子模块没拉取时会联网下载，没网就会失败。
- 生成完不要跑 `pnpm fetch:zmdmap` 或 `pnpm generate:*`：它们会先 `fetch`，版本相同而指纹不同时会用上游的数据覆盖你刚生成的结果。要重跑下游生成器，就直接跑 `package.json` 里对应 `generate:*` 脚本 `&&` 后面的 `node …` 命令。

## 写数据集代码

- 一份数据对应 `datasets/<名字>.py`（模块多时用文件夹 `datasets/<名字>/`，子类写在 `__init__.py`），名字就是产物文件名，扫包自动发现，没有登记表。辅助模块用 `_` 开头命名。
- 定义一个 `Dataset` 子类，`build` 写成 `@classmethod`，参数只写要用的输入，例如 `def build(cls, table_cfg: TableCfg, json_data: JsonData)`；没写的输入不会传入，也不会检查。
- 内容只从游戏数据推出来：不写人工名单、不读网页、不拼文件路径，表、JsonData、Lua、解包资源都通过注入的对象读（见 `utils/inputs.py`）。
- 读表用 `table_cfg.table("XxxTable", XxxRow)`。要用的字段不在 `utils/tables.py` 的 `Struct` 里就去补：带默认值，类型按数据里真实出现的形态写（枚举常见 `str | int` 两种写法都会出现），不要退回 `dict`。
- 五种语言的名字用基类的 `localized_names` / `plain_names`，不要自己查 `I18nTextTable`。
- 引用对不上、缺文本时抛 `DatasetError`（本地文件缺失或形状不对用 `LocalDataError`），不要静默填默认值；其他类型的异常不会被整理成错误提示，会直接打出调用栈。有意跳过的条目用 `utils/log.py` 的 `logger.warning` 写明哪条、为什么，不要用 `print`。
- 输出顺序要固定（按 id 或表里的排序字段排），否则每次生成都会出现无意义的改动。
- 带坐标的数据设 `needs_projection = True`，并在 `projection_containers` 列出条目数组所在的键；每个条目必须有 `map`（地图 id）和 `x`、`z`（世界坐标），最好带 `id`，出错时报错信息里会显示它。
- 模块开头的注释写清数据来自哪几张表、筛选规则、顶层字段；改了要同步。

## 改完之后

1. `uv run zmdmap generate --only <名字>`，看日志里的「跳过」提示和 `git diff tools/zmdmap/data/<名字>.json`，确认只多出或少了预期的条目、字段。
2. 搜读这份数据的下游，字段名或结构变了就一起改，再按上文重跑下游生成器。
3. 新增的数据在上游发布之前 `fetch` 拿不到，提交本地生成的 `data/<名字>.json`。
4. `uv run ruff check tools/zmdmap`、`uv run pyright`（本目录按 strict 类型检查）。
5. 不写单元测试，生成结果就是验证。要确认某条规则时临时造一份假表跑一次，跑完删掉。
