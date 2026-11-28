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
| `lock_limit.go` | 相同词条组合的锁定名额：已有锁定占额，新增按顺序占用剩余名额 |
| `inventory_after_battle.go` | 战后增量映射、合并与提交 |
| `matchapi/` | 可复用的纯匹配 API：`OCRInput -> MatchResult` |
| `register.go` | 注册本包 CustomAction / CustomRecognition |

## 运行流程

1. `EssenceFilterInitAction` 先读当前 Init 节点 `attach.mode` 决定分支：`filter` 自定义筛选、`screen_and_report` 只读盘点并生成报告、`inventory` 只读盘点、`after_battle` 战后（同一任务内重复进入只清本轮状态），未知值直接判失败。随后从界面 OCR 探测游戏语言（`__EssenceFilterDetectLang*`），或尊重 `attach.input_language` 显式覆盖；再从当前 Init 节点 `attach` 读取其余选项并加载 `data/EssenceFilter`。
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
| 生成基质报告 | 从开头扫描已锁或已装备的格位，对全部已锁无瑕和高纯基质读取词条，包括暂无适配武器的基质。 | 以全部已锁明细重建账号库存；目标计数只使用适配五星、六星武器的已锁无瑕基质。 |
| 目标模式首次盘点或「开始前重新盘点基质库存」 | 复用生成报告的只读扫描流程。 | 按当前账号已锁定的适配件数计算缺口。 |
| 目标、地区、随机刷取实际领取奖励 | 保留已有标记，无瑕按五星、六星武器匹配。「弃置其他基质」默认开启，对未标记的高纯和未适配无瑕执行弃置，关闭后跳过这些基质。 | 保留原有全部明细，确认标记后一次性追加本批已锁的适配无瑕基质；不扩大本批识别范围。 |

同图标、同词条的不同格位分别计件。已装备且未锁定的基质只影响扫描边界。遇到第一枚既未锁定也未装备的基质，核对已处理的前段并结束。

库存文件位于用户数据目录的 `debug/record/EssenceInventory/` 下。

| 文件 | 用途 |
| --- | --- |
| `<账号哈希>.json` | 当前账号的公开库存文件，也是任务读取的缓存。 |
| `unverified.json` | 无法确认 UID 时，「生成基质报告」写出的匿名报告，省略 `account_id`。匿名任务只复用本次任务的内存库存。 |

协议第 1 版以 `format` 和 `version` 标识文件。写入方整份替换快照，用 UTC 时间 `updated_at` 记录最近一次提交。每个 `items` 条目记录一枚实物的品质、实际标记、按显示顺序排列的三条游戏词条 ID 和等级。武器适配、分组与可用数量由读取方按自己的数据计算。见[协议 Schema](../../../tools/schema/essence_inventory.schema.json)。

`coverage.locked_matching_flawless_gold.complete_at` 是最近一次完成已锁适配无瑕基质盘点的时间。已确认的增量保留这个时间。读取旧版全仓报告时，也接受无瑕品质的完整盘点时间。

同一版本只会增加可选字段和枚举值。外部读取方应忽略未知字段、跳过无法理解的条目，遇到更高版本时提示升级。MaaEnd 拥有文件并会整份重写，因此遇到无法表示的品质、标记或词条时，会放弃整份基线并重新盘点，以免遗漏实物。

任务首次使用库存时通过 `captureuid` 绑定账号，提交前再次核对 UID。切号中断的更新保留为待复核，其他账号的库存独立保存。目标模式在当前账号的无瑕品质有 `complete_at` 时直接规划，更新武器数据后会按新数据重新计算适配。运行自定义筛选、手动改标记、分解基质或在其他设备整理仓库后，可开启「开始前重新盘点基质库存」。

## 限制相同词条基质的锁定数量

在「基质刷取 → 目标选择」中，`AutoEssenceTargetPlan` 将本次勾选的全部武器 ID 传给战后初始化的 `attach.lock_limit_weapon_ids`。每把勾选的五星或六星武器为其三词条组合提供一个名额。例如，只勾选「宏愿」时对应组合有一个名额，同时勾选「十二问」时有两个。

已有锁定先占名额。有空位时，按结算页的格位顺序上锁。名额已满时保留新掉落的原标记。仅适配未勾选武器的合格掉落照常锁定并计入库存，供下次切换目标时使用。

## 战后更新与锁定名额

随机、地区和目标模式都在领奖后逐枚处理基质，并保留已有标记。只有无瑕基质读取词条并匹配五星、六星武器。未标记的无瑕基质匹配成功时上锁，目标模式中对应名额已满时跳过。「弃置其他基质」默认开启，对未标记的高纯和未适配无瑕执行弃置，关闭后跳过这些基质。开关通过战后初始化的 `attach.discard_unmatched` 传入。

仓库盘点开始时把旧库存标为待复核。领奖时保留上次提交的库存，逐格识别奖励和品质，核对整批后再一次性追加。只锁定已确认的匹配基质。每格在读取详情时直接执行标记。两把勾选武器若共用同一组词条，需要两枚实物才能同时满足。

领奖未确认时显示提示，并丢弃本批暂存数据。地区、随机和目标模式都回到原有的轮次流程。下一轮目标规划继续使用上次确认的库存；跳过的掉落可在下次仓库盘点时补入。

目标模式开刷时按共用词条组合展示武器名、已锁数量和所需数量，并显示地点与全部目标的剩余需求。刻写参数可以在详情中展开。每批库存提交成功后，按实物数量汇总新锁定、顺带锁定、领取时已有标记、名额已满保留、弃置和跳过的结果。目标模式同时显示本轮补齐数量和剩余需求，已锁定与新锁定分开统计。

## 匹配数据

资源位于 `assets/data/EssenceFilter/`，运行目录下对应 `data/EssenceFilter/`：

- `matcher_config.json`：多语言归一化、相似字和停用后缀；
- `skill_pools.json`：三槽技能池；
- `weapons_output.json`：武器、稀有度和技能组合（由 `tools/essence_filter/sync_zmdmap.py` 从 zmdmap 的 `tools/zmdmap/data/weapons.json` 同步）；
- `locations.json`：预刻写地点数据。

OCR 文本会先按探测到的（或 attach 强制的）`input_language` 归一化。中文、繁中、日文和韩文会过滤无关标点；英文会进行小写化和常见缩写归一。

Init 语言探测节点见 Pipeline `EssenceFilter/DetectLanguage.json`（库存「愿望单」与战后「离开」锚点）。

## 运行选项

`EssenceFilterOptions` 来自 MaaFramework 已合并的 Init 节点 `attach`（`EssenceFilterInit` / `EssenceFilterInventoryInit` / `EssenceFilterAfterBattleInit`），包括 `mode`、武器稀有度、基质品质、扩展规则、未匹配废弃、导出选项。`mode` 由节点显式声明，Go 不再按节点名推断：`filter` 自定义筛选、`screen_and_report` 只读盘点并生成报告、`inventory` 只读盘点、`after_battle` 战后。`input_language` 可选：省略或 `AUTO` 时由屏幕 OCR 探测；显式 `CN|TC|EN|JP|KR` 则强制该语言。

`flawless_essence` / `pure_essence` / `skip_thumb_lock` / `skip_thumb_discard` 属于 C++ `EssenceGridAdvance.attach`，默认值即库存盘点口径，不在 Go 状态中重复保存；普通筛选通过选项覆盖。

## 开发约束

- 流程留在 Pipeline；Go 不直接发点击或滑动。
- 新增或删除 Custom 组件时同步 `register.go` 和 Custom Schema。
- 展示集中在 `ui.go`，匹配规则集中在 `matchapi`。
- 库存与战后共享匹配逻辑，但 UI 几何和 Pipeline 节点保持独立。
