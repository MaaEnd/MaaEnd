# 开发手册 - SellProduct 售卖产品

`SellProduct` 按用户配置的地区/据点自动售卖产品。任务遵循「Pipeline 管流程，Go 管算法」：Pipeline（`assets/resource/pipeline/SellProduct*.json`）负责界面识别与点击；Go Service（`agent/go-service/sellproduct/`）负责选品策略、保留规则、干员规划与缓存。

## 要点速览

- **入口**：`assets/tasks/SellProduct.json` 的 `SellProduct` 任务 → Pipeline 入口 `SellProductSchedule`（按星期门控）。
- **职责边界**：Go 侧只有两个业务包——`goods/`（货品选品、保留、缺货、优先表）与 `operator/`（干员识别、派驻规划、缓存），两者互不依赖；顶层 `runtime.go` 组合两者输出据点计划提示。
- **生成产物不要手改**：`assets/data/SellProduct/selection_data.json`、`assets/tasks/SellProduct.json`、`pipeline/SellProduct/{OperatorSession.json, Loop.json, {地区}/}`（Win32 与 ADB 两套）。一律改 `tools/pipeline-generate/SellProduct/` 下的模型/模板/投影后重新生成。
- **状态分两类**：缺货集合、已尝试、已满足保留量都是**任务级会话状态**，不落盘，下次任务初始化时清空；唯一落盘的是 `debug/record/SellProductCache.json`（按哈希 UID 隔离的干员快照 + 据点发展值状态）。

## 流程总览

```text
SellProductSchedule ──命中星期──> SellProductMain
  └─ SellProductEnterRegionalDevelopment   SceneManager 进地区建设
  └─ SellProductCaptureUid                 捕获哈希 UID（隔离账号缓存）
  └─ SellProductPrepareSession             SubTask 顺序执行 4 段初始化（先重置后配置）：
  │    ├─ InitializeReserveSession → RegisterReserveRule1..6   重置并注册保留规则（空槽位 no-op）
  │    ├─ InitializeOperatorSession → RegisterLocation × 6     重置干员会话并注册启用据点
  │    ├─ ConfigurePrioritySession                             优先售卖总开关/严格模式
  │    └─ ConfigureSelectionStrategy                           选品策略（稀有度/单价/库存）
  └─ SellProductLoop                       按新地区优先遍历：武陵 → 四号谷地 → SellProductTaskEnd

每个地区（SellProduct{Region}Sell）
  ├─ SceneManager 进本地区据点管理（SubTask）
  ├─ {Region}InitializePrioritySession → RegisterPriorityItem1..6   切换本地区优先表
  ├─ {Region}PrepareOperatorCache → ScanOperatorList     无快照时完整扫描干员列表
  └─ [JumpBack] 按固定顺序逐个执行本地区据点 → 回到 SellProductLoop

每个据点（SellProduct{LocationId}Sell）
  ├─ 绑定本据点锚点（ZeroMoneyHandler / SelectPriorityItem / CommitPriorityItem
  │    / MarkOutOfStock / BetterSliding / 售前售后 OperatorTarget）
  ├─ 识别据点发展值是否已满 → ReportLocationPlan（输出本据点售卖计划）
  ├─ SetOperatorAnchors → SellProductSellMain
  │    ├─ BeforeSellOperator → [Anchor]…Target   售前：切到计划售卖干员
  │    ├─ SellProductSellLoop                    通用售卖循环（见下）
  │    └─ AfterSellOperator → [Anchor]…Target    售后：按全局方案派驻生产干员
  └─ 返回地区节点继续下一个据点

SellProductSellLoop（不限次数，每轮先查调度券）
  ├─ [Anchor]ZeroMoneyHandler     调度券不足 → SellProductSellLoopEnd（进售后）
  ├─ [Anchor]CurrentGoodsReady    当前货品符合当前选品规则 → adopt 沿用 → SellProductAtSell
  └─ SellProductChangeGoods       点「更换货品」→ ResetGoodsSelection → ChangeGoodsRelay
       ├─ [Anchor]SelectPriorityItem → SelectNewGoodConfirm → [Anchor]CommitPriorityItem
       │    → SellProductAtSell：再查调度券 → 缺货则 [Anchor]MarkOutOfStock 记缺货
       │      → 提示需选货则重换一次 → [Anchor]BetterSliding 应用保留规则 → 交易/跳过
       └─ [Anchor]PriorityItemsExhausted → CloseGoodsAfterExhausted → SellLoopEnd
```

整任务级终止：据点管理未解锁时 SceneManager 进不去，任务终止；超出据点可兑换调度券上限的弹窗由 `SellProductAidQuotaExceededStop` 直接 StopTask，不自动确认。

> [!IMPORTANT]
>
> `InitializeReserveSession` 必须排在两个 `Configure*` 之前：其 `reset` 会连带重置整个优先售卖会话（总开关、严格模式、选品策略），顺序颠倒会吞掉配置。`InitializeOperatorSession` 与货品会话完全独立，位置可自由调整。

## 自动售卖规则（Go `goods/`）

### 选品策略（任务选项三选一）

| 策略 | 排序 | 备注 |
| ------------------ | ------------------------------------- | ------------------------------------ |
| 稀有度优先（默认） | 稀有度降序 → 单价降序 → 来源稳定序 | 允许选底部只露名称、库存未知的货品 |
| 单价优先 | 单价降序 → 稀有度降序 → 稳定序 | 同上 |
| 库存优先 | 仓储数量降序 → 单价 → 稀有度 → 稳定序 | 要求库存已识别，且不低于用户最低单价 |

### 选品识别（`SellProductPriorityItem` 自定义识别器）

- 只扫第一页，不为读第二页低等级商品滑动列表。
- 用详情图标模板锚定完整格子再 OCR 商品名；格子的名称/库存/点击相对区域由 Win32、ADB 两套 Pipeline 以 `stock_*_offset` 传入，Go 不硬编码平台坐标。
- 名称数 < 图标数视为 OCR 漏识 → 保持画面重新识别，不做选择；底部半截格子记录为「库存未知」。
- 先排除：零库存、已尝试、已缺货、永不售卖、已达保留量。
- 选中只记为「待提交」，确认返回售卖界面后 `commit` 才标记已尝试——点击失败或单帧 OCR 波动不会跳过高优先级货品。
- 候选全部不可用时，连续两次稳定识别到相同集合 → `PriorityItemsExhausted`，关闭列表结束本据点售卖。空 OCR 结果不算「无剩余货品」。

### 当前货品沿用（`SellProductCurrentGoods` 自定义识别器）

- 据点主界面的货品槽位用 IconRecognition `single_roi` 识别当前选中的货品，候选只含本据点可售物品；Win32、ADB 两套 Pipeline 分别通过 `roi` 传入 `[1177,450,54,54]` 与 `[1151,393,66,66]`，Go 不硬编码平台坐标。
- 稀有度和单价策略会复用正式选品规则，只有当前货品恰好是应用优先槽位、已尝试/缺货状态和保留规则后的下一候选时才沿用；否则落回「更换货品」流程扫描列表。
- 优先槽位先于库存策略生效，因此库存策略也可沿用排除已尝试、缺货和保留规则后第一个可用的优先货品；当前货品不是该优先候选，或已无可用优先货品时，必须打开列表重新读取实时仓储数量，不能沿用普通候选。
- 命中后由 `adopt` 操作把识别 detail 中的 `itemId` 登记为据点当前物品，状态效果与换货 `commit` 一致（记录已尝试、更新保留规则选中项），后续售卖、保留规则与缺货标记流程不区分货品来源。

### 优先售卖（默认关闭的总开关，与地区售卖开关解耦）

- 展开「仅售卖优先产品」+ 每地区独立开关和 6 个槽位（只列本地区可售物品）。
- 进入地区时切换该地区优先表；槽位 1→6 依次尝试；同一物品重复配置只保留最靠前槽位。
- 严格模式（仅售卖优先产品）只对**同时开启地区优先配置**的地区生效：这些地区只卖明确配置的物品，其余地区仍按所选策略正常售卖；已开启但无适用物品的地区在稳定确认两次空候选后正常结束。

### 保留规则（6 个独立槽位）

- 两种模式：保留指定数量 / 永不售卖（内部为数量 `-1`）；数量 `0` 等价于不保留；同一物品重复配置时后面的槽位覆盖前面的。
- 「保留指定数量」用 BetterSliding `ReverseTarget` 只卖超出部分；单次交易达到保留量、或库存本就不高于保留量，都会 `satisfy` 标记本次任务已满足，后续据点选品直接跳过。
- 「永不售卖」在选品识别阶段排除，不切货品、不记缺货。

### 缺货

换货后确认当前物品缺货 → `[Anchor]MarkOutOfStock` 按最后提交的 `itemId` 写入任务级缺货集合，后续据点选品直接跳过；不落盘，下次任务初始化时清空。

## 自动选择干员规则（Go `operator/`）

售前（切售卖干员）与售后（恢复生产派驻）共用同一闭环：**检查当前干员 → 打开列表 → 逐页扫描 → 按完整快照重规划**。

### 售卖干员分档

1. 发展值 + 交易收益双加成；
2. 仅发展值加成；
3. 仅交易收益加成；
4. 同档按游戏列表稳定序（命中该据点 `settlementFeatures` 数降序 → charId 数字降序，与稀有度无关）。

据点发展值已满时发展值词条失效，只按交易收益分档。「完美候选」= 当前据点最高售卖档 ∩ 该据点恢复候选；账号有完美候选时只从中规划（即使被其他已启用据点占用也不降级绕开），没有才回退最高售卖档。当前派驻已在可用最高档时直接沿用，不打开列表。

### 售后恢复分配

同一干员不能同时占多个据点，按顺序择优：可恢复据点数最多 → 沿用售前售卖干员的据点最多 → 最终派驻仍属最高加成档的据点最多 → 候选 `Priority` 总和更小。已确认的 `location → operator` 立即锁定，后续据点不得复用；新地区先完成先锁定。

### 派驻冲突

候选已在其他据点时弹确认框：来源据点本次已启用 → 确认调走；未启用或无法可靠识别 → 取消，并把该干员加入任务级临时排除集合后重新规划（下次任务初始化时重置）。

### 失败策略

售卖干员找不到 / 扫描失败 → 停止任务（避免带错干员交易）；恢复干员不可用 → 记录跳过，完成当前据点后继续任务。

### 干员缓存（`debug/record/SellProductCache.json`）

- 按 CaptureUID 的 16 位小写十六进制加盐哈希分账号隔离，未捕获时为 `unknown`；键不做二次规范化，避免碰撞串号。
- `operators` 是 `updated_at` + `ids` 的完整列表快照（字段缺失 = 尚未扫描；空数组 = 扫完但没有相关干员）；`locations` 保存各据点发展值是否已满。两者都用 `selection_data.json` 的稳定 ID，不存中文名，与客户端语言无关。
- 无快照 → 先完整扫描再开始售卖；「强制刷新干员缓存」→ 本次任务首次进地区时重扫一次，后续地区复用。据点内局部滚动扫描不覆盖快照。
- 售前识别到已知干员却不在快照 → 快照失效并完整重扫一次（一次任务最多触发一次）。
- 进据点时重新识别发展值并写回缓存；状态变化会让未完成据点立即重新规划，已完成的售后派驻保持锁定。
- 缓存无格式版本、不迁移旧格式；JSON 损坏或顶层结构不兼容时整份视为不存在，单账号分区异常只失效该账号。

## 运行期提示

- 复用干员快照时输出该账号 `updated_at` 的本地时间。
- 进据点后输出：售卖/售后干员目标、货品计划（静态策略列出顺序、库存策略提示扫描后决定）、缺货/已达保留量/永不售卖的排除项与适用保留规则。
- 之后输出干员沿用/切换结果、交易完成、缺货、保留达成、冲突重规划、扫描失败等动态状态。
- 文本均跟随客户端语言：Pipeline `focus` 用 interface i18n，Go 输出用 go-service i18n。

## 生成器与维护

生成器位于 `tools/pipeline-generate/SellProduct/`。zmdmap 数据 CI 通过 `data/scripts/sell_product_data.py` 从 TableCfg 裁剪并发布 `tools/pipeline-generate/data/sell_product.json`，这份精简游戏数据只保留据点、可售物品、据点特性与干员匹配关系；MaaEnd 通过 `fetch-data.mjs` 下载该文件。`model.mjs` 统一定义据点/地区/多语言键，各 `*-data.mjs` 是对应模板的最小数据投影。

| 维护入口 | 生成产物 |
| ------------------------------- | --------------------------------------------------------------- |
| `pipeline(-adb)-template.jsonc` | `pipeline/SellProduct/{Region}/{Location}.json`（Win/ADB 两套） |
| `sell-template.jsonc` | `pipeline/SellProduct/{Region}/SellProduct{Region}.json` |
| `loop-template.jsonc` | `pipeline/SellProduct/Loop.json` |
| `session-template.jsonc` | `pipeline/SellProduct/OperatorSession.json` |
| `task-template.jsonc` | `assets/tasks/SellProduct.json` |
| `sync-locales.mjs` | 五语言据点/干员/物品 locale 键 |
| `selection-data.mjs` | `assets/data/SellProduct/selection_data.json` |

手工维护（生成器不碰）：

- `pipeline/SellProduct.json`：任务入口与初始化链；
- `SellProduct/SellCore.json`、`ChangeGoods.json`：通用售卖循环与选货流程；
- `SellProduct/OperatorScan.json`：干员缓存扫描；
- `SellProduct/ReserveSession.json`：保留规则会话；
- `agent/go-service/sellproduct/` 全部 Go 代码：`goods/`（货品，含 `strategy/` 三个纯策略实现）、`operator/`（干员）、`internal/selectiondata/`（共享部署数据加载校验）、`internal/ocrmatch/`（共享 OCR 严格匹配）、`runtime.go`（`SellProductLocationPlan` 组合提示）、`register.go`（聚合注册）。

```shell
# 同步 zmdmap 精简游戏数据并完整重新生成
pnpm generate:SellProduct

# 只同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap

# 只用已生成的数据重新渲染
node tools/pipeline-generate/SellProduct/sync-locales.mjs
node tools/pipeline-generate/SellProduct/selection-data.mjs
node tools/pipeline-generate/run-all.mjs SellProduct
```

维护注意：

- 新增货品通常只需同步 zmdmap 精简游戏数据；`sync-locales.mjs` 自动补齐五语言 `item.*` 键（中文名相同的复用旧键）。活动物品临时排除集中在 `selection-data.mjs`，原始数据移除后应清理并重新生成。
- 新增据点后检查生成的地区 `next`、SceneManager 入口及 Win/ADB 两套产物。
- 新增地区时先在两套资源包的 `SellProduct/` 下手动创建地区子目录再运行生成（生成器只创建 `outputDir`）。
- 保留规则的物品 case 通过 `attach` 提供 `item_id`，数量 input 通过 `custom_action_param.quantity` 提供整数值。

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
