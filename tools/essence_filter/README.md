# EssenceFilter 工具

基质筛选的数据流：zmdmap 生成/发布精简数据 → `tools/zmdmap/data/` → 本目录的生成步骤 → `assets/`。

| 文件 | 从哪来 | 去哪 |
| --- | --- | --- |
| `energy_point_gems.json` | zmdmap（`tools/zmdmap/data/`） | 只当构建期输入，被 `build_locations.py` 读 |
| `weapons.json` | zmdmap（`tools/zmdmap/data/`） | 构建期输入（技能池、任务选项），并同步成运行时资源 |
| `weapons_output.json` | `sync_zmdmap.py` 从上面那份复制 | `assets/data/EssenceFilter/`，Go 侧匹配引擎运行时读 |
| `skill_pools.json` / `locations.json` | 本目录的生成步骤 | `assets/data/EssenceFilter/`，运行时读 |

## sync_zmdmap.py

把 zmdmap 的武器表同步成运行时资源：Go 侧匹配引擎在运行时从资源目录读
`assets/data/EssenceFilter/weapons_output.json`，所以这里把 `tools/zmdmap/data/weapons.json` 复制过去
（结构一致，只做校验与写入，不联网）。`tools/zmdmap/data/` 里的数据由 `uv run zmdmap fetch` 下载或
`uv run zmdmap generate` 本地生成。

```bash
uv run tools/essence_filter/sync_zmdmap.py
```

## extract_skill_pools.py

从 `weapons.json` 提取技能池（skill_pools），各语言（cn/tc/en/jp/kr）直接取自同文件的 skills 数组，无需 i18n。

### 用法

```bash
uv run tools/essence_filter/extract_skill_pools.py
```

### 参数

| 参数 | 默认值 | 说明 |
| ------------ | ------------------------------------- | ------------------------------- |
| `--input` | `tools/zmdmap/data/weapons.json` | zmdmap 的武器表路径 |
| `--output` | `assets/data/EssenceFilter/skill_pools.json` | 输出的 skill_pools.json 路径 |
| `--base-dir` | 当前目录 | 仓库根目录 |

### 提取规则

- 从每个武器的 `skills.CN` 按位置归入 slot：`[0]` → slot1，`[1]` → slot2（若长度为 3）或 slot3（若长度为 2），`[2]` → slot3。
- 技能名取「基名」：按 `·`、`・`、`:`、`：`、`[` 分割，取第一段（如 `力量提升·小` → `力量提升`，`Strength Boost [S]` → `Strength Boost`）。
- 多语言从同武器的 skills.CN/TC/EN/JP/KR 同位置提取基名。
- 每 slot 去重后保留已有 `(slot, cn)` 的 id；新增词条从该 slot 的最大 id 加一分配，最后按 id 排序。

## build_locations.py

从 `energy_point_gems.json` 解析地点词条并映射到 `skill_pools.json`，生成 `locations.json`（每个地点带 `level_id`，即源数据里的 `levelId`，运行时按它认地区）。写回默认位置且内容有变化时，把本轮同步时间（UTC，形如 `2026-10-08 08:40:17 UTC`）写进 `matcher_config.json` 的 `data_version`，给用户看这份数据是哪天同步的；内容没变就跳过写入，`data_version` 保持原值；用 `--output` 输出到别处时不动仓库里的 `matcher_config.json`。

### 用法

```bash
uv run tools/essence_filter/build_locations.py
```

### 参数

| 参数 | 默认值 | 说明 |
| ----------------- | -------------------------------------------------- | -------------------- |
| `--energy-points` | `tools/zmdmap/data/energy_point_gems.json` | zmdmap 的能量淤积点数据 |
| `--skill-pools` | `assets/data/EssenceFilter/skill_pools.json` | 当前技能池 |
| `--output` / `-o` | `assets/data/EssenceFilter/locations.json` | 输出 locations |
| `--debug` | 关闭 | 打印未匹配项以便排查 |

### 解析规则

- 仅处理 `costStamina > 0` 的条目（重度能量淤积点）。
- 按 `pointName` 去重；同地点多个 `worldLevel` 只取第一条。
- `secAttrTermNames` 作为 slot2 来源：先去后缀（`伤害提升`/`效率提升`/`强度提升`/`提升`），再按别名映射（如 `源石技艺` -> `源石技艺强度`、`终结技` -> `终结技充能`）。
- `skillTermNames` 作为 slot3 来源：按中文名直接匹配。
- 输出：`name`、`level_id`、`slot2_ids`、`slot3_ids`、`slot2`、`slot3`。

## build_autoessence_tasks.py

从 `weapons.json`、`skill_pools.json`、`locations.json` 生成基质刷取的目标模式和地区模式。

```bash
uv run tools/essence_filter/build_autoessence_tasks.py
```

- 目标模式只收录 5、6 星武器，按稀有度从高到低、武器 ID 升序排列，6 星作为默认勾选。
- 武器图标写成 `resource/image/UI/Item/<武器 ID>.png`。这个路径对应已发布的 UI 图标，不使用站点 `weapons.json` 里的 `icon_path`。缺少对应 PNG 时生成失败。
- 地区模式按 `locations.json` 的顺序生成地区列表、每个地区的附加/技能词条，以及随机模式的地区多选。地区 `level_id` 到节点 key 的对照在 `agent/go-service/autoessence/locations.go` 的 `locationLevelToKey`，key 到 interface 文案（`$global.region.*`）的对照在同文件的 `locationLabelKeys`。新地区要先把这两张表补上、五语言文案补齐，并运行 `pnpm generate:AutoEssence` 生成 `RegionNodes` 下的节点（见 [`tools/pipeline-generate/AutoEssence`](../pipeline-generate/AutoEssence/README.md)），再运行生成器（缺哪一项就报哪一项）。
- 地区列表同时同步到 `CommonNodes/AutoEssence.json` 的两处：设锚点节点的 `sub` 列表，以及 `AutoEssenceDispatcher` 每轮清零各地区传送节点命中次数的 `nodes` 列表（按区域 key 排序）。
- 武器显示名 `★稀有度 名称` 会同步到各语言的 interface locale。词条显示名仍使用已有的 `option.AutoEssenceSkill.*`，缺文案时生成失败。
