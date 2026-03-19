# EssenceFilter

基质筛选 Go Service：在库存界面中按「目标武器 + 技能组合」识别每个基质格子的词条，匹配则锁定，否则跳过或废弃；并支持扩展规则（未来可期、实用基质）与预刻写方案推荐。

由 Pipeline 通过 CustomAction 调用，流程与分支由 JSON 控制，本包只提供动作实现与领域逻辑。

## 文件与职责（同一 case 放一起）

| 文件               | 职责                                                                                                                                                                         |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `types.go`         | 数据类型与常量（运行选项、基质颜色等）；匹配所需数据结构由 `matchapi` 提供                                                                                                |
| `state.go`         | 单次运行状态 `RunState`、`getRunState` / `setRunState`、`Reset()`；持有 `matchapi.Engine` 与统计结果                                                                 |
| `filter.go`        | 小工具：`skillCombinationKey`（用于 UI 统计聚合）                                                                                                                         |
| `ui.go`            | 所有展示：MXU 日志、战利品摘要、技能池/统计日志、预刻写方案推荐（结果来自 `matchapi`）                                                                                   |
| `actions.go`       | 所有 CustomAction：Init / OCR 库存与 Trace / CheckItem·CheckItemLevel·SkillDecision / RowCollect·RowNextItem·Finish·SwipeCalibrate                                           |
| `options.go`       | 从节点 attach 读取 `EssenceFilterOptions`、 rarity/essence 列表格式化                                                                                                        |
| `resource_path.go` | 监听资源加载路径，供 Init 解析数据目录                                                                                                                                       |
| `register.go`      | 注册 ResourceSink 与各 CustomAction，供上层 `go-service` 统一加载                                                                                                            |
| `matchapi/`        | 纯匹配 API：`OCRInput -> MatchResult`，默认加载 `assets/data/EssenceFilter/*`，可供外部 go module 复用                                                                        |

## 数据流概要

1. **Init**：读资源路径 → 创建 `matchapi.Engine`（加载 `assets/data/EssenceFilter/*`）→ 读选项 → 按稀有度构建目标组合 → 写 `RunState` 并 `setRunState`。
2. **运行中**：Pipeline 依次调用 RowCollect（收集本行格子并 ColorMatch；若开启 `skip_locked_row` 则对缩略图跑 `EssenceThumbMarked`，已标记格不进入本行列表）→ RowNextItem（点击下一格）→ CheckItemSlot1/2/3（OCR 技能）→ CheckItemLevel（OCR 等级）→ SkillDecision（匹配并 OverrideNext 锁定/跳过/废弃）。
3. **Finish**：输出战利品摘要、扩展规则统计，可选输出预刻写方案 → `setRunState(nil)`。

所有运行时可变状态集中在 `RunState`，由 Init 分配、Finish 清空；匹配数据由 `matchapi.Engine` 管理与缓存。

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
