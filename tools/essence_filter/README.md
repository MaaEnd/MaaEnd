# EssenceFilter 工具

## extract_skill_pools.py

从 `weapons_output.json` 提取技能池（skill_pools），五语（cn/tc/en/jp/kr）直接取自同文件的 skills 数组，无需 i18n。

### 用法

```bash
python tools/essence_filter/extract_skill_pools.py
```

### 参数

| 参数         | 默认值                                          | 说明                            |
| ------------ | ----------------------------------------------- | ------------------------------- |
| `--input`    | `assets/data/EssenceFilter/weapons_output.json` | 输入的 weapons_output.json 路径 |
| `--output`   | `assets/data/EssenceFilter/skill_pools.json`    | 输出的 skill_pools.json 路径    |
| `--base-dir` | 当前目录                                        | 仓库根目录                      |

### 提取规则

- 从每个武器的 `skills.CN` 按位置归入 slot：`[0]` → slot1，`[1]` → slot2（若长度为 3）或 slot3（若长度为 2），`[2]` → slot3。
- 技能名取「基名」：按 `·`、`・`、`:`、`：`、`[` 分割，取第一段（如 `力量提升·小` → `力量提升`，`Strength Boost [S]` → `Strength Boost`）。
- 五语从同武器的 skills.CN/TC/EN/JP/KR 同位置提取基名。
- 每 slot 用 set 去重后按排序赋 id 1..n。

## build_locations.py

从 `energy_point_gems.json` 解析地点词条并映射到 `skill_pools.json`，生成 `locations.json`。

### 用法

```bash
python tools/essence_filter/build_locations.py
```

### 参数

| 参数              | 默认值                                             | 说明                                      |
| ----------------- | -------------------------------------------------- | ----------------------------------------- |
| `--energy-points` | `assets/data/EssenceFilter/energy_point_gems.json` | 输入的能量淤积点数据                      |
| `--skill-pools`   | `assets/data/EssenceFilter/skill_pools.json`       | 当前技能池                                |
| `--output` / `-o` | `assets/data/EssenceFilter/locations.json`         | 输出 locations                            |
| `--debug`         | 关闭                                               | 打印未匹配项以便排查                      |

### 解析规则

- 仅处理 `costStamina > 0` 的条目（重度能量淤积点）。
- 按 `pointName` 去重；同地点多个 `worldLevel` 只取第一条。
- `secAttrTermNames` 作为 slot2 来源：先去后缀（`伤害提升`/`效率提升`/`强度提升`/`提升`），再按别名映射（如 `源石技艺` -> `源石技艺强度`、`终结技` -> `终结技充能`）。
- `skillTermNames` 作为 slot3 来源：按中文名直接匹配。
- 输出格式保持不变：`name`、`slot2_ids`、`slot3_ids`、`slot2`、`slot3`。
