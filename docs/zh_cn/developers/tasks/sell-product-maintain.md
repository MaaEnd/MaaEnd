# 开发手册 - SellProduct 售卖产品

本文介绍 `SellProduct` 的主流程、自动选择干员规则、自动售卖规则和生成器维护方式。

任务采用“Pipeline 管流程，Go 管算法”的结构：Pipeline 负责进入界面、识别状态和逐步点击；`agent/go-service/sellproduct/` 负责干员规划、物品优先级、会话状态和保留数量计算。

> [!IMPORTANT]
>
> `assets/data/SellProduct/selection_data.json`、`assets/tasks/SellProduct.json`、`assets/resource/pipeline/SellProduct/OperatorSession.json`、两套 `Outposts/*.json` 和 `assets/resource/pipeline/SellProduct/Sell.json` 都是生成产物。不要直接修改，应更新 `tools/pipeline-generate/SellProduct/` 下的模型、数据投影或模板后重新生成。

## 主流程

任务从 `SellProductSchedule` 开始，命中用户配置的执行周期后进入 `SellProductMain`。初始化阶段先进入地区建设界面、捕获哈希 UID，再注册本次任务启用的保留规则和据点：

```text
SellProductSchedule                                  （Task 入口，按星期门控）
  └─ SellProductMain                                 （售卖流程入口）
       └─ SellProductEnterRegionalDevelopment        （SceneManager：进入地区建设）
            └─ SellProductCaptureUid                 （捕获哈希 UID，隔离账号缓存）
                 └─ SellProductInitializeReserveSession （清空上次任务的保留/选品状态）
                      └─ SellProductRegisterReserveRule{1..6} （固定串联，空槽位直接跳过）
                           └─ SellProductRegisterPriorityItem{1..6} （固定串联，空槽位直接跳过）
                                └─ SellProductInitializeOperatorSession （初始化干员规划与恢复锁）
                                     └─ SellProductRegisterLocation{LocationId} × N （固定串联，非活跃据点直接跳过）
                                          └─ SellProductOperatorSessionReady
                                               └─ SellProductLoop         （进入地区遍历）
```

保留规则和优先物品的 12 个注册节点始终启用，并按槽位顺序固定串联。任务选项只覆盖已配置槽位的稳定 `itemId`；未配置槽位保留空 `item_id`，Custom Action 将其作为 no-op 成功跳过。据点注册节点同样固定串联，任务选项只把启用据点的 `active` 参数设为 `true`，非活跃据点直接 no-op。两段初始化流程均无需为任意启用组合维护逐层缩短的 `next` 候选列表。

`SellProductLoop` 始终按“四号谷地 → 武陵”的固定地区顺序执行；地区内据点也按生成模型中的固定顺序执行。未启用地区或据点由对应入口直接跳过，因此相同的启用组合总会得到相同的售卖顺序。地区入口通过 SceneManager 进入据点管理页，准备干员缓存，再用 `[JumpBack]` 依次执行该地区的据点：

```text
SellProductLoop                                      （地区建设主循环）
  ├─ SellProductValleyIVSell                         （进入四号谷地据点管理）
  │    ├─ [Anchor]SellProductValleyIVPrepareOperatorCache （准备干员缓存）
  │    └─ [JumpBack]SellProductRefugeeCamp → SellProductInfraStation
  │       → SellProductReconstructionHQ
  │         （通过 JumpBack 依次执行三个据点）
  ├─ SellProductWulingSell                           （进入武陵据点管理）
  │    ├─ [Anchor]SellProductWulingPrepareOperatorCache （准备/复用干员缓存）
  │    └─ [JumpBack]SellProductSkyKingFlatsConstructionSite
  │       → SellProductCardiacRemediationStation → SellProductXiranflowCloudseederStation
  │         （通过 JumpBack 依次执行三个据点）
  └─ SellProductTaskEnd                              （所有启用地区处理完成）
```

每个据点先设置当前据点专用的 anchor，按计划切换联络干员，然后进入通用售卖循环。循环结束后，通过恢复 anchor 派驻全局方案选出的生产干员，再返回地区节点继续下一个据点。

```text
SellProduct{LocationId}                              （识别/进入目标据点）
  └─ [Anchor]SellProductBeforeSellOperator           （检查并切换售卖干员）
       └─ SellProductSellLoop                        （按优先级循环选择并售卖货品）
            └─ SellProductSellLoopEnd                （券不足或候选耗尽）
                 └─ [Anchor]SellProductAfterSellOperator （恢复干员并返回地区流程）
```

据点管理未解锁时，`SellProductOutpostLocked` 返回地区主循环；超出据点可兑换调度券上限时，`SellProductAidQuotaExceededStop` 停止整个任务。超限弹窗不自动确认。

## 自动选择干员规则

`assets/data/SellProduct/selection_data.json` 包含生成器从 `tools/pipeline-generate/data/settlement_trade.json` 提取的候选干员、加成类型、多语言名称和稳定顺序。Go 根据该文件为启用据点规划售卖干员和恢复干员，Pipeline 执行对应的列表操作。

自动选择分为售前派驻和售后恢复两个阶段，两者共用“检查当前干员 → 打开列表 → 逐页扫描 → 按完整拥有集合重规划”的闭环：

```text
[Anchor]SellProductBeforeSellOperator                    （进入当前据点的售前派驻流程）
  └─ SellProduct{LocationId}BeforeSellOperator
       ├─ SellProduct{LocationId}CurrentTargetOperator   （计划第一候选已在岗，直接售卖）
       │    └─ SellProductSellLoop
       └─ SellProduct{LocationId}OpenTargetOperatorList  （打开联络干员列表）
            └─ SellProduct{LocationId}InTargetOperatorList
                 ├─ SelectTargetOperator → ConfirmTargetOperator
                 │    → CloseTargetOperatorLiaison → TargetOperatorDone
                 │      （命中计划候选，派驻并返回售卖循环）
                 ├─ TargetOperatorManagedConflict → CloseTargetOperatorLiaison
                 │      （来源据点已启用，确认调至当前据点）
                 ├─ TargetOperatorProtectedConflict → CancelTargetOperatorAlreadyAssigned
                 │    → CloseTargetOperatorLiaisonAfterAlreadyAssigned → OpenTargetOperatorList
                 │      （来源据点未启用或无法识别，取消、临时排除并从顶部重新选择）
                 ├─ SwipeTargetOperatorList → InTargetOperatorList
                 │      （当前页未命中，继续向下扫描）
                 ├─ RetryTargetOperatorAfterScan → CloseTargetOperatorLiaisonAfterScan
                 │    → OpenTargetOperatorList
                 │      （到底后刷新拥有缓存，按新方案重开列表一次）
                 ├─ TargetOperatorNotFound              （完整扫描后无候选，停止任务）
                 └─ TargetOperatorScanFailed            （扫描或缓存失败，停止任务）

[Anchor]SellProductAfterSellOperator                     （售卖结束后恢复生产干员）
  └─ SellProduct{LocationId}AfterSellOperator
       ├─ SellProduct{LocationId}CurrentRestoreOperator  （恢复目标已在岗，完成据点）
       └─ SellProduct{LocationId}OpenRestoreOperatorList
            └─ SellProduct{LocationId}InRestoreOperatorList
                 ├─ SelectRestoreOperator → ConfirmRestoreOperator
                 │    → CloseRestoreOperatorLiaison → RestoreOperatorDone
                 │      （派驻恢复目标并锁定 location -> operator）
                 ├─ RestoreOperatorManagedConflict → CloseRestoreOperatorLiaison
                 │      （来源据点已启用，确认调至当前据点）
                 ├─ RestoreOperatorProtectedConflict → CancelRestoreOperatorAlreadyAssigned
                 │    → CloseRestoreOperatorLiaisonAfterAlreadyAssigned → OpenRestoreOperatorList
                 │      （来源据点未启用或无法识别，取消、临时排除并重新规划）
                 ├─ SwipeRestoreOperatorList → InRestoreOperatorList
                 │      （当前页未命中，继续向下扫描）
                 ├─ RetryRestoreOperatorAfterScan → CloseRestoreOperatorLiaisonAfterScan
                 │    → OpenRestoreOperatorList
                 │      （到底后按完整拥有集合重新分配并重试一次）
                 ├─ RestoreOperatorNotFoundAtBottom → CloseRestoreOperatorLiaisonAfterNotFound
                 │    → SkipRestoreOperatorDone          （无可用恢复候选，记录跳过）
                 └─ RestoreOperatorScanFailed            （扫描或缓存失败，停止任务）
```

售卖干员先按售卖收益分档：

1. 同时提供经验与信用点加成；
2. 仅提供信用点加成；
3. 仅提供经验加成；
4. 同档候选保持游戏干员列表中的稳定顺序。

本文所称“最高加成档”是当前据点最高售卖收益档与该据点恢复候选的交集，也就是同时完美满足售卖和恢复的干员。账号拥有至少一名完美候选时，售前规划只在这些完美候选中选择，即使候选当前被另一个已启用据点占用，也不会降级绕开；若账号没有完美候选，才回退到可用的最高售卖收益档。

`selection_data.json` 为每个售卖候选保留 `bonus_tier`，避免把同档候选的稳定顺序误当成收益差异。当前派驻属于可用的最高加成档时，Pipeline 不打开干员列表；需要更换时，Go 会逐个评估同档候选对应的全局恢复方案，优先沿用当前派驻，并选择能让全局恢复方案保留更多售卖干员、且最终派驻可供后续任务直接售卖的候选。全局结果仍相同时，按稳定候选顺序决胜。

干员缓存保存在 `debug/record/SellProductOwnedOperators.json`，按哈希 UID 隔离：

- 缓存只保存完整列表扫描结果；账号分区存在即视为可消费的完整快照，即使拥有列表为空。
- 当前账号没有快照时，Pipeline 会先完整扫描干员列表并写入缓存，再开始规划与售卖。
- 已有快照时直接复用；“本次运行前强制刷新”会忽略现有快照，在本次任务首次进入地区时完整扫描一次，同一任务的后续地区复用结果。
- 只有首次建立或主动强制刷新的全局扫描允许写入缓存；据点内选人时的局部滚动扫描不会覆盖已有快照。
- 规划与选人只基于完整快照中的真实拥有集合，不会用不完整观察做理论最优猜测。
- 列表扫描到底后，若刷新或重新规划导致目标变化，Pipeline 可关闭并重新打开列表执行一次新方案。
- Pipeline 必须识别列表、点击候选、识别“派驻”按钮并确认返回据点后，才认为切换成功。
- 若“派驻”后弹出候选已在其他据点派驻的确认框，Pipeline 用 `And` 同时识别弹窗、Go 来源分类和对应按钮。来源据点在本次任务已启用时点击确认，将候选调至当前据点；来源据点未启用或无法可靠识别时点击取消，并将候选加入本次任务的全局临时排除集合后重新规划。排除集合会在下次任务初始化时重置。

恢复时需要保证同一干员不能同时占用多个据点。Go 按以下顺序分配：

1. 最大化能够恢复的据点数量；
2. 覆盖数相同时，尽量保留各据点售前已经派驻的售卖干员，减少无意义切换；
3. 本次保留数量相同时，尽量让最终派驻仍属于对应据点的最高加成档（同时满足售卖和恢复），使后续任务无需再次切换；
4. 后续可沿用数量也相同时，选择候选 `Priority` 总和更小的方案；
5. 已确认的 `location -> operator` 立即锁定，后续据点不能重复分配该干员。

售卖目标找不到或扫描失败会停止任务，避免在错误干员上继续交易；恢复目标不可用时可以记录跳过，结束当前据点后继续任务。

## 自动售卖规则

`selection_data.json` 包含按 `itemId` 合并的多繁荣度记录、五语言名称、活动物品过滤结果和各据点默认顺序。Go 根据该顺序应用用户优先级覆盖。默认排序为：

1. 稀有度降序；
2. 单价降序；
3. 同值保留数据源中的稳定顺序。

任务配置提供一个默认关闭的优先售卖开关，开启后展开 6 个直接调整该列表的优先级槽位。已配置物品按槽位 1 至 6 移到默认列表最前面；不属于当前据点的物品自动跳过，同一物品重复配置时只保留最靠前的槽位，其余物品继续保持上述默认顺序。

任务运行期间，确认进入每个据点后，UI 会输出该据点的售卖干员目标、售卖后恢复目标、实际计划售卖顺序、因本次任务已确认缺货而排除的物品，以及适用的保留规则；未列出的物品默认全部售卖。随后在状态确认后显示干员实际沿用或切换结果、当前货品和交易完成状态。当前据点新确认物品缺货时，会立即显示物品名与据点名。干员已在其他据点派驻时会显示来源据点是否由本次任务管理；受保护候选被排除并重新规划时也会记录原因。完整扫描产生新方案时显示据点、用途和新干员。售卖干员不可用、干员扫描失败以及恢复干员不可用并跳过恢复时也会显示对应结果。任务内的 UI 提示均使用当前客户端语言。

未解锁货品不会出现在当前界面，因此会自然跳过。售卖没有固定次数限制，每轮流程如下：

```text
SellProductSellLoop                                  （不限次数的售卖循环）
  ├─ [Anchor]SellProductZeroMoneyHandler             （调度券不足则结束循环）
  └─ SellProductChangeGoods                          （调度券充足时识别并点击「更换货品」）
       └─ [Anchor]SellProductSelectPriorityItem      （识别并点击最高优先级货品）
            └─ SellProductSelectNewGoodConfirm       （识别确认按钮并点击）
                 └─ [Anchor]SellProductCommitPriorityItem （回到售卖界面后提交）
                      └─ [Anchor]SellProductBetterSliding  （应用保留规则）
                           └─ SellProduct{LocationId}BetterSliding （设置可售数量）
                                └─ SellProductSell / SellProductSkipToNextSellLoop
                                     （交易或因保留量跳过）
                                     └─ SellProductSellLoop
                                          （继续下一候选，直到满足结束条件）
```

每轮选择货品前都会先检查调度券。换货后再次检查时，调度券不足也优先于当前货品缺货，避免调度券已经耗尽后继续遍历后续优先物品。首次进入据点即发现不足时显示提示；已有交易完成后则静默结束该据点售卖循环。

`SellProductPriorityItem` 自定义识别器只在识别阶段记录为待处理。Pipeline 点击并确认货品、重新识别到据点售卖界面后，`SellProductPrioritySession` 才把该货品标记为已尝试。点击失败或单帧 OCR 波动不会跳过高优先级货品。

若 `SellProductZeroProductAfterChangeStillEmpty` 在换货后确认当前物品缺货，Pipeline 会调用当前据点绑定的 `SellProductMarkOutOfStock` 锚点。Go 按该据点最后提交的 `itemId` 写入本次任务共享的缺货集合；后续据点动态选品会直接跳过该物品。缺货集合不写入磁盘，并随下一次 SellProduct 会话初始化清空。

循环仅在以下情况结束：

- `SellProductZeroMoney` 识别到当前据点调度券不足；
- 当前可见的已知货品全部已尝试或已在本次任务中标记缺货，且连续两次识别到相同集合；
- 超出兑换上限时由 `SellProductAidQuotaExceededStop` 停止整个任务。

空 OCR 结果不会被当作“无剩余货品”。缺货物品仍保留在稳定识别集合中，但不会再次成为候选；缺货、成功交易或因保留量跳过都会继续下一轮。

独立保留规则提供六个槽位，每个槽位按稳定 `itemId` 配置最低保留数量：

- 未命中规则时，BetterSliding 使用默认“全部售出”。
- 命中规则时，使用 `TargetReverse` 只售卖高于保留量的部分。
- 当前库存不高于保留量时，通过 `SellProductSkipToNextSellLoop` 跳过交易。
- 同一物品重复配置时，后面的槽位覆盖前面的槽位；数量 `0` 等价于不保留。

## 生成器

生成器位于 `tools/pipeline-generate/SellProduct/`。`model.mjs` 根据 zmdmap 数据定义据点 ID、多语言 OCR、任务选项和模板数据；`selection-data.mjs` 生成 Go 使用的部署数据 `assets/data/SellProduct/selection_data.json`。`tools/pipeline-generate/data/` 是生成器的数据源目录。

| 维护入口                                             | 生成产物                                                    |
| ---------------------------------------------------- | ----------------------------------------------------------- |
| `model.mjs`                                          | 据点、地区、多语言 OCR 的共享模型                           |
| `pipeline-template.jsonc`                            | `assets/resource/pipeline/SellProduct/Outposts/*.json`      |
| `pipeline-adb-template.jsonc`                        | `assets/resource_adb/pipeline/SellProduct/Outposts/*.json`  |
| `sell-template.jsonc`                                | `assets/resource/pipeline/SellProduct/Sell.json`            |
| `session-template.jsonc`                             | `assets/resource/pipeline/SellProduct/OperatorSession.json` |
| `task-template.jsonc`                                | `assets/tasks/SellProduct.json`                             |
| `sync-locales.mjs`                                   | 五语言据点名和干员键                                        |
| `selection-data.mjs`                                 | `assets/data/SellProduct/selection_data.json`               |
| `tools/pipeline-generate/data/settlement_trade.json` | zmdmap 上游贸易数据源                                       |

以下文件由手工维护，生成器不处理：

- `assets/resource/pipeline/SellProduct.json`：任务入口和地区循环；
- `SellProduct/SellCore.json`、`ChangeGoods.json`：通用售卖与选货流程；
- `SellProduct/OperatorScan.json`：干员缓存扫描；
- `SellProduct/ReserveSession.json`：保留规则会话；
- `agent/go-service/sellproduct/operator_selection.go`：售卖干员筛选和全局恢复分配算法；
- `agent/go-service/sellproduct/selection_data.go`：加载并校验随应用发布的部署数据；
- `agent/go-service/sellproduct/item_ordering.go`：展开生成后的默认顺序、应用用户优先级覆盖并选择下一货品；
- `agent/go-service/sellproduct/` 下的其余文件：Custom 组件接入、会话状态、缓存和保留规则。

常用命令：

```shell
# 拉取最新 zmdmap 数据并重新生成
pnpm generate:SellProduct

# 使用当前缓存完整重新生成，不访问网络
node tools/pipeline-generate/SellProduct/sync-locales.mjs
node tools/pipeline-generate/SellProduct/selection-data.mjs
node tools/pipeline-generate/run-all.mjs SellProduct
```

维护时注意：

- 不要手改生成产物；修改对应模板或数据投影后重新生成。
- 新增货品通常只需更新 zmdmap 缓存；若需要保留规则 label，再补齐五语言 `item.*` 文案。
- 新增据点后要检查生成的地区 `next`、SceneManager 入口及 Win/ADB 两套文件。
- 活动物品临时排除项集中在 `selection-data.mjs`，同时影响 Task 可选项和运行时数据；上游移除活动数据后应清理该过滤项并重新生成。
- 保留规则的物品 case 通过 `attach` 提供 `item_id`，数量 `input` 通过 `custom_action_param.quantity` 提供整数值。

提交前至少运行：

```shell
node --test tools/pipeline-generate/SellProduct/data.test.mjs tools/pipeline-generate/SellProduct/selection-data.test.mjs tools/pipeline-generate/SellProduct/sync-locales.test.mjs
# 在 agent/go-service/ 目录运行
go test ./sellproduct
# 回到仓库根目录运行
pnpm check
pnpm test
git diff --check
```
