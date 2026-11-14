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
| `actions.go` | 库存/战后 Init、技能 OCR 写入、等级写入、匹配决策和 Finish |
| `actionsAfterBattle.go` | 战后品质门禁、匹配决策与库存提交动作 |
| `recognitionAfterBattle.go` | 战后按顺序返回识别框 |
| `integration.go` | 匹配调用、OverrideNext、统计与展示适配 |
| `state.go` | 单线程 Agent 的当前运行状态 `currentRun` |
| `options.go` | 从合并后的节点 `attach` 读取运行选项 |
| `ui.go` | MXU 展示、匹配摘要和预刻写输出 |
| `plan_export.go` | 可选的 `debug/record/EssencePlan.html` 导出 |
| `inventory.go` | 无瑕库存分组计数、`EssenceInventory.json` 读写与读取接口 |
| `lock_limit.go` | 相同词条组合的锁定名额：强化期望次数、逐份决策与次优超限报账 |
| `inventory_after_battle.go` | 战后增量映射、合并与提交 |
| `matchapi/` | 可复用的纯匹配 API：`OCRInput -> MatchResult` |
| `register.go` | 注册本包 CustomAction / CustomRecognition |

## 运行流程

1. `EssenceFilterInitAction` 先读当前 Init 节点 `attach.mode` 决定分支：`filter` 普通筛选、`inventory` 库存盘点、`after_battle` 战后（同一任务内重复进入只清本轮状态），未知值直接判失败。随后从界面 OCR 探测游戏语言（`__EssenceFilterDetectLang*`），或尊重 `attach.input_language` 显式覆盖；再从当前 Init 节点 `attach` 读取其余选项并加载 `data/EssenceFilter`。
2. Pipeline 对 C++ 选中的格子依次 OCR 三个技能和等级。
3. `runUnifiedSkillDecision` 调用 `matchapi.Engine.MatchOCR`。
4. Go 根据 `MatchResult` 覆盖下一 Pipeline 节点。
5. Pipeline 识别按钮、点击并确认状态。
6. `EssenceFilterFinishAction` 输出统计并把 `currentRun` 置空。

Agent 回调按当前框架模型串行执行，因此 `currentRun` 不加锁。新的任务先经过 Init，正常 Finish 清理状态；失败或中断时由任务结束事件按 TaskID 清理。战后仅在同一任务内保留累计统计。

## 输出基质库存

任务顶层的「输出基质库存」默认关闭。开启时 UI 隐藏原筛选选项，关闭后重新显示。游戏语言由 Init 时界面 OCR 自动识别。开启后走 `EssenceFilterInventoryInit`（`attach.mode = inventory`），固定预设直接写在该节点的 `attach`：只读取未标记弃置的无瑕基质（包括已锁定的基质），匹配全部四至六星武器，不执行锁定、弃置、扩展规则和预刻写推荐。弃置项在缩略图阶段排除，不点击、不调用 OCR、不计入库存。盘点的品质与缩略图口径就是 `EssenceGridAdvance.attach` 的默认值（无瑕、跳过弃置、不跳过已锁定），普通筛选由 `FlawlessEssence` / `PureEssence` / `SkipThumbLock` / `SkipThumbDiscard` 选项覆盖；ROI 与滑动配置保留控制器对应的值。

输出为用户数据目录（`PI_USER_DATA_DIR`，未注入时为安装根目录）下 `debug/record/EssenceInventory.json`，结构见 [库存 Schema](../../../tools/schema/essence_inventory.schema.json)。每个词条组合一组，武器共享该组的 `essences` 等级与数量分布；数量不乘以武器数。等级依次为基础属性、附加属性、技能属性。`count` 包含已锁定的基质，`locked` 只统计其中已锁定的部分，不会超过 `count`。无匹配结果输出 `[]`。

`Engine.MatchInventoryOCR` 区分已识别但不匹配的基质与识别失败。网格遍历、品质过滤与跨页去重交给 C++ 和框架；Go 每收到一份完整识别结果就汇总一次，不回查网格状态或校验扫描编号。

库存模式通过 Pipeline 预设把 OCR fallback、点击失败及入口失败改为 `StopTask`，不跳格继续。合法但不适配的基质照常略过；OCR 文本无法解析或等级非法则由 Go 返回动作失败。盘点模式只在正常到达 Finish 时覆盖写全量快照，并在临时文件写完、关闭后替换旧文件；失败或中断保留旧导出。普通筛选仍使用原有 fallback。

## 限制相同词条基质的锁定数量

任务选项「限制相同词条基质的锁定数量」默认关闭。开启后任务先用盘点模式全量扫描无瑕基质、写出 `EssenceInventory.json`，再进入筛选流程：两个 Entry 同时启用，盘点在前，任一步失败即 `StopTask`，筛选不会在库存缺失时开始。选项的 `Yes` 分支不挂子选项，品质与「跳过已锁定」「跳过已弃置」随选项树隐藏；武器稀有度、额外规则与未匹配废弃照旧生效。

筛选 Init 读取 `attach.limit_locked_per_combination = true` 时必须加载库存文件：缺失、为空或无法解析都输出错误并让动作失败，不降级为「一颗都不锁」。计划由 `lock_limit.go` 的纯函数模块建立，只按词条组合对齐库存分组（不拿分组的 `weapon_ids` 当键，因为它含四星武器，也会被稀有度勾选裁掉）。

每个词条组合的锁定名额等于**适配武器数**：引擎武器表中词条组合完全相同且稀有度为 5 或 6 的武器条数，与勾选的武器稀有度无关；只有四星武器使用的组合名额为 0，既不锁定也不报警。持有份按强化到 [6,6,3] 的期望次数升序排名，前 N 份（锁定名额）内的未锁份才会被锁定；已锁定的次优份只会提醒，永不解除锁定，因此锁定总数可能超过 N。提醒按组合每轮最多一条，任务结束的汇总再按组合重列一次。

「基质精确命中五六星组合但库存没有该分组」「分组存在但没有该等级三元组的行」以及等级非法的行都按「理应被记录却查不到」处理：输出警告并跳过该基质，不做本轮次数与库存 `count` 的一致性判定。扩展规则（未来可期、技能 3 实用）与未匹配出口不受名额约束。

开启本选项会让任务时长接近翻倍（先全量盘点、再全量筛选），战后筛选与 AutoEssence 的选项透传本轮不变。

## 战后写入库存

战后筛选在离开战利品界面之前经过 `EssenceFilterAfterBattleCommitInventory`（`EssenceFilterAfterBattleInventoryCommitAction`），把本轮决策合并进同一份 `EssenceInventory.json`。决策动作只把条目追加到 `RunState.PendingInventory`：精确匹配与「技能 3 实用」按决策累加 `count`，其中上锁的同时累加 `locked`；未来可期、无匹配与弃置不产生条目。战后 Init 进入时清空增量，提交成功后清空。

合并按 `weapon_ids` 排序后组成组键，组内按 `levels` 找行累加，既有的组和行原样保留，输出按组与等级排序。文件不存在或为空时从零开始合并；文件无法解析或写入失败时返回动作失败并保留旧文件，由提交节点的 `on_error` 路由到退出节点，不卡在战利品界面。每轮只提交一次，进程被杀或中途停止时丢失的本轮增量不做兜底，由下一次盘点覆盖纠正。

## 匹配数据

资源位于 `assets/data/EssenceFilter/`，运行目录下对应 `data/EssenceFilter/`：

- `matcher_config.json`：多语言归一化、相似字和停用后缀；
- `skill_pools.json`：三槽技能池；
- `weapons_output.json`：武器、稀有度和技能组合（由 `tools/essence_filter/sync_zmdmap.py` 从 zmdmap 的 `tools/zmdmap/data/weapons.json` 同步）；
- `locations.json`：预刻写地点数据。

OCR 文本会先按探测到的（或 attach 强制的）`input_language` 归一化。中文、繁中、日文和韩文会过滤无关标点；英文会进行小写化和常见缩写归一。

Init 语言探测节点见 Pipeline `EssenceFilter/DetectLanguage.json`（库存「愿望单」与战后「离开」锚点）。

## 运行选项

`EssenceFilterOptions` 来自 MaaFramework 已合并的 Init 节点 `attach`（`EssenceFilterInit` / `EssenceFilterInventoryInit` / `EssenceFilterAfterBattleInit`），包括 `mode`、武器稀有度、基质品质、扩展规则、未匹配废弃、导出选项。`mode` 由节点显式声明，Go 不再按节点名推断：`filter` 普通筛选、`inventory` 库存盘点、`after_battle` 战后。`input_language` 可选：省略或 `AUTO` 时由屏幕 OCR 探测；显式 `CN|TC|EN|JP|KR` 则强制该语言。

`flawless_essence` / `pure_essence` / `skip_thumb_lock` / `skip_thumb_discard` 属于 C++ `EssenceGridAdvance.attach`，默认值即库存盘点口径，不在 Go 状态中重复保存；普通筛选通过选项覆盖。

## 开发约束

- 流程留在 Pipeline；Go 不直接发点击或滑动。
- 新增或删除 Custom 组件时同步 `register.go` 和 Custom Schema。
- 展示集中在 `ui.go`，匹配规则集中在 `matchapi`。
- 库存与战后共享匹配逻辑，但 UI 几何和 Pipeline 节点保持独立。
