# EssenceFilter Go Service

本包负责基质的技能 OCR 归一化、目标组合匹配、锁定/废弃决策、运行统计和结果展示。它不扫描库存网格，也不直接点击锁定或废弃按钮。

完整架构见 [RecoGrid / GridTracker / EssenceGrid 架构指南](../../../docs/zh_cn/developers/components/recogrid-engine.md)。

## 职责边界

| 层 | 职责 |
| --- | --- |
| C++ `RecoGrid` | 单帧网格识别、占用判断和格子特征 |
| C++ `GridTracker` | 跨帧对齐、顺序、去重和结束确认 |
| C++ `EssenceGrid` | 品质与缩略图筛选、待处理格子队列 |
| Go `essencefilter` | OCR 归一化、技能匹配、Lock/Discard/Skip 决策、统计 |
| Pipeline | 点击、滑动、等待和操作后确认 |

Go 只通过 `ctx.OverrideNext` 选择 Pipeline 分支：

- 库存：`EssenceFilterLockItem` / `EssenceFilterDiscardItem` / `EssenceGridAdvance`；
- 战后：`EssenceFilterAfterBattleLockItem` / `EssenceFilterAfterBattleDiscardItem` / `EssenceFilterAfterBattleCloseDetail`。

动作入口必须指向实际的 `LockItem` / `DiscardItem`，不能直接指向操作后的 `CheckLocked` / `CheckDiscarded`。

## 文件与职责

| 文件 | 职责 |
| --- | --- |
| `actions.go` | 库存 Init、技能 OCR 写入、等级写入、匹配决策和 Finish |
| `actionsAfterBattle.go` | 战后品质门禁与匹配决策 |
| `recognitionAfterBattle.go` | 战后按顺序返回识别框 |
| `integration.go` | 匹配调用、OverrideNext、统计与展示适配 |
| `state.go` | 单线程 Agent 的当前运行状态 `currentRun` |
| `options.go` | 从合并后的节点 `attach` 读取运行选项 |
| `ui.go` | MXU 展示、匹配摘要和预刻写输出 |
| `plan_export.go` | 可选的 `EssencePlan.html` 导出 |
| `inventory.go` | 无暇库存分组计数和 `EssenceInventory.json` 导出 |
| `matchapi/` | 可复用的纯匹配 API：`OCRInput -> MatchResult` |
| `register.go` | 注册本包 CustomAction / CustomRecognition |

## 运行流程

1. `EssenceFilterInitAction` 从 `EssenceFilterInit.attach` 读取选项并加载 `data/EssenceFilter`。
2. Pipeline 对 C++ 选中的格子依次 OCR 三个技能和等级。
3. `runUnifiedSkillDecision` 调用 `matchapi.Engine.MatchOCR`。
4. Go 根据 `MatchResult` 覆盖下一 Pipeline 节点。
5. Pipeline 识别按钮、点击并确认状态。
6. `EssenceFilterFinishAction` 输出统计并把 `currentRun` 置空。

Agent 回调按当前框架模型串行执行，因此 `currentRun` 不加锁。新的任务先经过 Init，正常 Finish 清理状态；失败或中断时由任务结束事件按 TaskID 清理。战后仅在同一任务内保留累计统计。

## 输出基质库存

任务顶层的「输出基质库存」默认关闭。开启时 UI 仅显示游戏语言和库存输出开关，原筛选选项放在关闭分支下，关闭后重新显示。盘点使用固定预设：只读取未标记弃置的无暇基质（包括已锁定的基质），匹配全部四至六星武器，不执行锁定、弃置、扩展规则和预刻写推荐。弃置项在缩略图阶段排除，不点击、不调用 OCR、不计入库存。网格参数只覆盖品质与跳过标志，保留控制器对应的 ROI 和滑动配置。

输出为工作目录下的 `EssenceInventory.json`，结构见 [库存 Schema](../../../tools/schema/essence_inventory.schema.json)。每个词条组合一组，武器共享该组的 `essences` 等级与数量分布；数量不乘以武器数。等级依次为基础属性、附加属性、技能属性。无匹配结果输出 `[]`。

`Engine.MatchInventoryOCR` 区分已识别但不匹配的基质与识别失败。网格遍历、品质过滤与跨页去重交给 C++ 和框架；Go 每收到一份完整识别结果就汇总一次，不回查网格状态或校验扫描编号。

库存模式通过 Pipeline 预设把 OCR fallback、点击失败及入口失败改为 `StopTask`，不跳格继续。合法但不适配的基质照常略过；OCR 文本无法解析或等级非法则由 Go 返回动作失败。只有正常到达 Finish 才写入 `EssenceInventory.json`，并在临时文件写完、关闭后替换旧文件；失败或中断保留旧导出。普通筛选仍使用原有 fallback。

## 匹配数据

资源位于 `assets/data/EssenceFilter/`，运行目录下对应 `data/EssenceFilter/`：

- `matcher_config.json`：多语言归一化、相似字和停用后缀；
- `skill_pools.json`：三槽技能池；
- `weapons_output.json`：武器、稀有度和技能组合；
- `locations.json`：预刻写地点数据。

OCR 文本会先按 `input_language` 归一化。中文、繁中、日文和韩文会过滤无关标点；英文会进行小写化和常见缩写归一。

## 运行选项

`EssenceFilterOptions` 来自 MaaFramework 已合并的 `EssenceFilterInit.attach`，包括武器稀有度、基质品质、扩展规则、未匹配废弃、导出选项和 OCR 语言。

`skip_thumb_lock` / `skip_thumb_discard` 属于 C++ `EssenceGridAdvance.attach`，不在 Go 状态中重复保存。

## 开发约束

- 流程留在 Pipeline；Go 不直接发点击或滑动。
- 新增或删除 Custom 组件时同步 `register.go` 和 Custom Schema。
- 展示集中在 `ui.go`，匹配规则集中在 `matchapi`。
- 库存与战后共享匹配逻辑，但 UI 几何和 Pipeline 节点保持独立。
