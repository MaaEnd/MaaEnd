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
