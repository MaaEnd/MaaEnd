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
| `inventory.go` | 在内存中按适配武器和等级分组，计算可用件数与目标缺口 |
| `inventory_scan.go` | 按格位记录三条词条和确认后的标记，复核完整件数 |
| `account_inventory.go` | 账号绑定、库存协议读写和更新事务 |
| `lock_limit.go` | 相同词条组合的锁定名额：强化期望次数、逐份决策与次优超限报账 |
| `inventory_after_battle.go` | 战后增量映射、合并与提交 |
| `matchapi/` | 可复用的纯匹配 API：`OCRInput -> MatchResult` |
| `register.go` | 注册本包 CustomAction / CustomRecognition |

## 运行流程

1. `EssenceFilterInitAction` 先读当前 Init 节点 `attach.mode` 决定分支：`filter` 自定义筛选、`screen_and_report` 全仓筛选并生成报告、`inventory` 只读盘点、`after_battle` 战后（同一任务内重复进入只清本轮状态），未知值直接判失败。随后从界面 OCR 探测游戏语言（`__EssenceFilterDetectLang*`），或尊重 `attach.input_language` 显式覆盖；再从当前 Init 节点 `attach` 读取其余选项并加载 `data/EssenceFilter`。
2. Pipeline 对 C++ 选中的格子依次 OCR 三个技能和等级。
3. `decideSkillMark` 调用 `matchapi.Engine.MatchOCR`。
4. Go 根据 `MatchResult` 覆盖下一 Pipeline 节点。
5. Pipeline 识别按钮、点击并确认状态。
6. `EssenceFilterFinishAction` 输出统计并把 `currentRun` 置空。

Agent 回调按当前框架模型串行执行，因此 `currentRun` 不加锁。新的任务先经过 Init，正常 Finish 清理状态；失败或中断时由任务结束事件按 TaskID 清理。战后仅在同一任务内保留累计统计。

## 筛选与库存报告

「贵重品库 → 基质筛选锁定」默认使用自定义筛选。用户选择武器星级、品质、跳过和弃置规则。此次筛选只打开需要处理的格位，库存缓存和报告只由以下入口维护。

| 入口 | 读取和标记 | 库存更新 |
| --- | --- | --- |
| 筛选全部五星、六星武器适配的无瑕基质并生成报告 | 一趟扫描整个仓库。三条词条精确适配任一五星或六星武器的未标记无瑕基质上锁，已有标记保留。 | 用操作后的标记重建库存文件，并显示路径与件数。 |
| 目标模式首次盘点或「开始前重新盘点基质库存」 | 遍历仓库，读取当前标记。 | 重建当前账号库存，按已锁件数计算缺口。 |
| 目标、地区、随机刷取实际领取奖励 | 按相同的五星、六星规则筛选本批掉落。 | 确认标记后一次性追加本批奖励。 |

同图标、同词条的不同格位分别计件。扫描结束时，Go 核对库存总数、格位数和已处理件数。已确认是高纯但未读全词条的格位只计入 `coverage.high_purity_purple.unread`。缺少格位、品质未知或无瑕词条未确认时，下次目标刷取会重新盘点。

库存文件位于用户数据目录的 `debug/record/EssenceInventory/` 下。

| 文件 | 用途 |
| --- | --- |
| `<账号哈希>.json` | 当前账号的公开库存文件，也是任务读取的缓存。 |
| `unverified.json` | 无法确认 UID 时，「筛选并生成报告」写出的匿名报告，省略 `account_id`。匿名任务只复用本次任务的内存库存。 |

协议第 1 版以 `format` 和 `version` 标识文件。写入方整份替换快照，用 UTC 时间 `updated_at` 记录最近一次提交。每个 `items` 条目记录一枚实物的品质、实际标记、按显示顺序排列的三条游戏词条 ID 和等级。武器适配、分组与可用数量由读取方按自己的数据计算。见[协议 Schema](../../../tools/schema/essence_inventory.schema.json) 和[一致性样例](./testdata/inventory/)。

`coverage.<品质>.complete_at` 表示该品质的每一枚基质都已列出且词条读全，值为那次完整盘点的 UTC 时间。后续已确认的增量保留这个时间。缺少该字段时，文件只承诺包含已知的部分。`unread` 单独计数已确认存在但未读全的基质，与 `complete_at` 互斥。

同一版本只会增加可选字段和枚举值。外部读取方应忽略未知字段、跳过无法理解的条目，遇到更高版本时提示升级。MaaEnd 拥有文件并会整份重写，因此遇到无法表示的品质、标记或词条时，会放弃整份基线并重新盘点，以免遗漏实物。

任务首次使用库存时通过 `captureuid` 绑定账号，提交前再次核对 UID。切号中断的更新保留为待复核，其他账号的库存独立保存。目标模式在当前账号的无瑕品质有 `complete_at` 时直接规划，更新武器数据后会按新数据重新计算适配。运行自定义筛选、手动改标记、分解基质或在其他设备整理仓库后，可开启「开始前重新盘点基质库存」。

## 限制相同词条基质的锁定数量

「基质刷取 → 目标选择」提供此开关，默认关闭。开启后，`AutoEssenceTargetPlan` 将本次勾选的全部武器 ID 传给战后两遍的 `attach.lock_limit_weapon_ids`。每把勾选的五星或六星武器为其三词条组合提供一个名额。例如，只勾选「宏愿」时对应组合有一个名额，同时勾选「十二问」时有两个。

已有锁定先占名额。有空位时，在本批新掉落中按强化至 `[6, 6, 3]` 的期望次数由少到多上锁。名额已满时保留新掉落的原标记。仅适配未勾选武器的合格掉落照常锁定并计入库存，供下次切换目标时使用。

## 战后更新与锁定名额

随机、地区和目标模式都在实际领奖后逐枚读取奖励。未标记的无瑕基质精确适配任一五星或六星武器时上锁，并遵守已开启的目标组合名额。其余掉落与已有标记保持原状。

领取或盘点开始前，先在磁盘上撤下所有品质的 `complete_at`，内存保留原始基线。结算页入口和逐枚扫描共用金色、紫色两种外壳模板，模板遮住会随词条变化的内核图案。执行遍在详情中核对每个奖励框的品质、三条词条与最终标记，再将整批奖励一次性提交。目标模式开启锁定名额时先暂存一遍奖励，再执行标记。计算刷取缺口时，每枚已确认锁定的精确匹配无瑕基质可分配给一把武器。两把勾选武器共用同一组词条时，库存需有两枚才能结束。

游戏明确提示体力不足并拒绝领取时，撤销本次领取记录，保留领取前的库存。领取后中断、遗漏已定位的奖励格、品质未知或无瑕词条识别失败会使库存待复核。下次目标刷取通过完整盘点恢复。进程重启后，缺少 `complete_at` 的无瑕库存仍需盘点。缺少仓库基线时只记录已确认的奖励，目标模式会先建立完整基线。

## 匹配数据

资源位于 `assets/data/EssenceFilter/`，运行目录下对应 `data/EssenceFilter/`：

- `matcher_config.json`：多语言归一化、相似字和停用后缀；
- `skill_pools.json`：三槽技能池；
- `weapons_output.json`：武器、稀有度和技能组合（由 `tools/essence_filter/sync_zmdmap.py` 从 zmdmap 的 `tools/zmdmap/data/weapons.json` 同步）；
- `locations.json`：预刻写地点数据。

OCR 文本会先按探测到的（或 attach 强制的）`input_language` 归一化。中文、繁中、日文和韩文会过滤无关标点；英文会进行小写化和常见缩写归一。

Init 语言探测节点见 Pipeline `EssenceFilter/DetectLanguage.json`（库存「愿望单」与战后「离开」锚点）。

## 运行选项

`EssenceFilterOptions` 来自 MaaFramework 已合并的 Init 节点 `attach`（`EssenceFilterInit` / `EssenceFilterInventoryInit` / `EssenceFilterAfterBattleInit` / `EssenceFilterAfterBattleReadonlyInit`），包括 `mode`、`after_battle_pass`、武器稀有度、基质品质、扩展规则、未匹配废弃、导出选项。`mode` 由节点显式声明，Go 不再按节点名推断：`filter` 自定义筛选、`screen_and_report` 全仓筛选并生成报告、`inventory` 只读盘点、`after_battle` 战后。`after_battle_pass` 只由战后模式读取：`readonly` 只读遍、`execute`（留空同义）执行遍。`input_language` 可选：省略或 `AUTO` 时由屏幕 OCR 探测；显式 `CN|TC|EN|JP|KR` 则强制该语言。

`flawless_essence` / `pure_essence` / `skip_thumb_lock` / `skip_thumb_discard` 属于 C++ `EssenceGridAdvance.attach`，默认值即库存盘点口径，不在 Go 状态中重复保存；普通筛选通过选项覆盖。

## 开发约束

- 流程留在 Pipeline；Go 不直接发点击或滑动。
- 新增或删除 Custom 组件时同步 `register.go` 和 Custom Schema。
- 展示集中在 `ui.go`，匹配规则集中在 `matchapi`。
- 库存与战后共享匹配逻辑，但 UI 几何和 Pipeline 节点保持独立。
