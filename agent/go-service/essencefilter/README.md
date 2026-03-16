# EssenceFilter

基质筛选 Go Service：在库存界面中按「目标武器 + 技能组合」识别每个基质格子的词条，匹配则锁定，否则跳过或废弃；并支持扩展规则（未来可期、实用基质）与预刻写方案推荐。

由 Pipeline 通过 CustomAction 调用，流程与分支由 JSON 控制，本包只提供动作实现与领域逻辑。

## 文件与职责（同一 case 放一起）

| 文件               | 职责                                                                                                                                                                         |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `types.go`         | 数据类型与常量（武器、技能池、组合、选项、基质颜色等）                                                                                                                       |
| `state.go`         | 单次运行状态 `RunState`、`getRunState` / `setRunState`、`Reset()`                                                                                                            |
| `config.go`        | 匹配器配置加载 `LoadMatcherConfig`、`GetMatcherConfig()`                                                                                                                     |
| `loader.go`        | 数据加载（skill_pools / weapons_output / locations）+ DB 访问（`weaponDB`、`GetPoolBySlot`、`SkillNameByID`）+ 展示名转规范名（`cleanDisplayToCanonical`、`weaponTypeToID`） |
| `matcher.go`       | 技能匹配：OCR 文本 → 技能 ID（`MatchEssenceSkills`、`MatchFuturePromising`、`MatchSlot3Level3Practical`），依赖 config 与 loader 的池访问                                    |
| `filter.go`        | 按稀有度过滤武器、提取技能组合、统计过滤后技能分布（写入 state）、`skillCombinationKey`                                                                                      |
| `ui.go`            | 所有展示：MXU 日志、战利品摘要、技能池/统计日志、预刻写方案推荐（同一 case：本次运行的结果展示）                                                                             |
| `actions.go`       | 所有 CustomAction：Init / OCR 库存与 Trace / CheckItem·CheckItemLevel·SkillDecision / RowCollect·RowNextItem·Finish·SwipeCalibrate                                           |
| `options.go`       | 从节点 attach 读取 `EssenceFilterOptions`、 rarity/essence 列表格式化                                                                                                        |
| `resource_path.go` | 监听资源加载路径，供 Init 解析数据目录                                                                                                                                       |
| `register.go`      | 注册 ResourceSink 与各 CustomAction，供上层 `go-service` 统一加载                                                                                                            |

## 数据流概要

1. **Init**：读资源路径 → 加载 matcher 配置 → 加载 DB（skill_pools、weapons_output、locations）→ 读选项 → 按稀有度/基质类型过滤 → 写 `RunState` 并 `setRunState`。
2. **运行中**：Pipeline 依次调用 RowCollect（收集本行格子并 ColorMatch）→ RowNextItem（点击下一格）→ CheckItemSlot1/2/3（OCR 技能）→ CheckItemLevel（OCR 等级）→ SkillDecision（匹配并 OverrideNext 锁定/跳过/废弃）。
3. **Finish**：输出战利品摘要、扩展规则统计，可选输出预刻写方案 → `setRunState(nil)`。

所有运行时可变状态集中在 `RunState`，由 Init 分配、Finish 清空；配置与 DB 为包级单例，由 loader/config 写入、其余只读。

## 外部数据（资源目录下 EssenceFilter）

- `matcher_config.json`：相似字映射、停用后缀（按语言），用于技能名规范化与 OCR 匹配。
- `skill_pools.json`：slot1/2/3 技能池（id、中文名等）。
- `weapons_output.json`：武器列表（internal_id、weapon_type、rarity、names、skills 等），loader 会转成 `WeaponData` 并解析技能为池 ID。
- `locations.json`：刷取地点与可选 slot2/slot3 池 ID，用于预刻写方案按地点推荐。

基准分辨率为 720p（1280×720），坐标与 ROI 均按此设计。

## 开发说明

- 新增/修改 CustomAction 后需在 `register.go` 中注册。
- 匹配与过滤逻辑尽量放在 matcher / filter，actions 只做编排与 state/OverrideNext；UI 文案与 HTML 集中在 ui.go。
- 遵循项目根目录 `AGENTS.md` 中 Go Service 规范：流程由 Pipeline 控制，本包不写大流程，仅提供可复用的动作与领域能力。
