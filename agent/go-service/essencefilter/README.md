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
| `inventory.go` | 兼容库存分组与读取接口 |
| `inventory_scan.go` | 按格位记录三条词条和确认后的标记，复核完整件数 |
| `account_inventory.go` | 账号绑定、更新事务和导出 |
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

普通筛选与只读盘点都在同一趟网格遍历中读取无瑕、高纯和三种标记。品质和跳过选项决定哪些格子可以执行标记操作。每格保留三条词条的池、ID 和等级，以及精确适配的武器 ID。相同图标、相同词条和同池词条都允许重复。最后一屏通过 C++ 复核后，Go 再核对库存总数、发出的格位数与逐格 OCR 件数。

库存文件位于用户数据目录的 `debug/record` 下。

| 文件 | 内容 |
| --- | --- |
| `EssenceInventory/<账号哈希>.json` | 供任务读取的账号库存。`scanned_at` 记录经过完整盘点的品质，`updated_at` 记录最近一次提交，`pending` 表示更新尚未确认。 |
| `EssenceInventoryFull.json` | 最近发布的逐件明细。数组中每项代表一件，`weapon_ids` 可为空。见[明细 Schema](../../../tools/schema/essence_inventory_full.schema.json)。 |
| `EssenceInventory.json` | 同一快照的兼容导出，按适配武器和等级合并未弃置的无瑕基质。`count` 包含 `locked`，无适配项时为 `[]`。见[分组 Schema](../../../tools/schema/essence_inventory.schema.json)。 |

任务先复用 `captureuid` 的缓存。缺失时使用现有控制器采样。UID 无法确认时，本次任务可以继续使用内存库存，只有只读盘点会同时生成便携导出。目标模式在账号、词条数据版本和盘点覆盖均有效时直接规划。手动整理库存后可开启「开始前重新盘点基质库存」。

## 限制相同词条基质的锁定数量

任务选项「限制相同词条基质的锁定数量」默认关闭。开启后任务先用盘点模式建立本账号的库存，再进入筛选流程：两个 Entry 同时启用，盘点成功后才开始筛选。选项的 `Yes` 分支不挂子选项，品质、武器稀有度与「跳过已锁定」「跳过已弃置」随选项树隐藏；`EssenceFilterInit.attach` 把稀有度钉死为 5、6 星，品质只留无瑕。额外规则与未匹配废弃照旧生效。

筛选 Init 读取 `attach.limit_locked_per_combination = true` 时必须加载当前账号的已验证库存：缺失、为空或无法解析都输出错误并让动作失败，不降级为「一颗都不锁」。计划由 `lock_limit.go` 的纯函数模块建立，只按词条组合对齐库存分组（不拿分组的 `weapon_ids` 当键，因为它含四星武器，与「名额只计五六星」的口径不一致）。

每个词条组合的锁定名额等于**适配武器数**：引擎武器表中词条组合完全相同且稀有度为 5 或 6 的武器条数，与筛选侧的稀有度勾选无关（限制名额时稀有度固定为 5、6 星）；只有四星武器使用的组合名额为 0，既不锁定也不报警。持有份按强化到 [6,6,3] 的期望次数升序排名，前 N 份（锁定名额）内的未锁份才会被锁定。已锁定的次优份在扫描到时明确提示「本次扫描到的基质需要手动解锁」并给出词条与等级，任务结束的汇总再按组合与等级列出需要解锁的份数；程序永不解除锁定，因此锁定总数可能超过 N。期望强化次数只在内部用于排名，不出现在用户文案里。

「基质精确命中五六星组合但库存没有该分组」「分组存在但没有该等级三元组的行」以及等级非法的行都按「理应被记录却查不到」处理：输出警告并跳过该基质，不做本轮次数与库存 `count` 的一致性判定。扩展规则（未来可期、技能 3 实用）与未匹配出口不受名额约束。

开启本选项会让任务时长接近翻倍（先全量盘点、再全量筛选）。

## 战后更新与锁定名额

`AutoEssenceConfirmObtain` 在领取前将账号库存标为 `pending`。战后按识别框的顺序读取每件奖励，只在确认标记后更新该件状态。获奖框数量与有完整词条的格位数必须相等。普通单遍和只读单遍都在离开奖励前提交一次。

显式开启原有锁定名额时，只读遍暂存本轮奖励供执行遍计算名额。执行遍复核相同格位、品质和词条，最后一次性提交。锁定名额默认关闭。目标模式关闭战后标记时，仍会读取奖励以更新目标缺口。

游戏明确提示体力不足并拒绝领取时，撤销本次领取记录，保留领取前的库存。领取后中断、跳过奖励识别或某格 OCR 失败都会使库存待复核。下次目标刷取通过完整盘点恢复。进程重启后，磁盘上的 `pending` 仍然有效。缺少仓库基线时只记录已确认的奖励，目标模式会先建立完整基线。

## 匹配数据

资源位于 `assets/data/EssenceFilter/`，运行目录下对应 `data/EssenceFilter/`：

- `matcher_config.json`：多语言归一化、相似字和停用后缀；
- `skill_pools.json`：三槽技能池；
- `weapons_output.json`：武器、稀有度和技能组合（由 `tools/essence_filter/sync_zmdmap.py` 从 zmdmap 的 `tools/zmdmap/data/weapons.json` 同步）；
- `locations.json`：预刻写地点数据。

OCR 文本会先按探测到的（或 attach 强制的）`input_language` 归一化。中文、繁中、日文和韩文会过滤无关标点；英文会进行小写化和常见缩写归一。

Init 语言探测节点见 Pipeline `EssenceFilter/DetectLanguage.json`（库存「愿望单」与战后「离开」锚点）。

## 运行选项

`EssenceFilterOptions` 来自 MaaFramework 已合并的 Init 节点 `attach`（`EssenceFilterInit` / `EssenceFilterInventoryInit` / `EssenceFilterAfterBattleInit` / `EssenceFilterAfterBattleReadonlyInit`），包括 `mode`、`after_battle_pass`、武器稀有度、基质品质、扩展规则、未匹配废弃、导出选项。`mode` 由节点显式声明，Go 不再按节点名推断：`filter` 普通筛选、`inventory` 库存盘点、`after_battle` 战后。`after_battle_pass` 只由战后模式读取：`readonly` 只读遍、`execute`（留空同义）执行遍。`input_language` 可选：省略或 `AUTO` 时由屏幕 OCR 探测；显式 `CN|TC|EN|JP|KR` 则强制该语言。

`flawless_essence` / `pure_essence` / `skip_thumb_lock` / `skip_thumb_discard` 属于 C++ `EssenceGridAdvance.attach`，默认值即库存盘点口径，不在 Go 状态中重复保存；普通筛选通过选项覆盖。

## 开发约束

- 流程留在 Pipeline；Go 不直接发点击或滑动。
- 新增或删除 Custom 组件时同步 `register.go` 和 Custom Schema。
- 展示集中在 `ui.go`，匹配规则集中在 `matchapi`。
- 库存与战后共享匹配逻辑，但 UI 几何和 Pipeline 节点保持独立。
