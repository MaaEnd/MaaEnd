# 开发手册 - SellProduct 维护文档

本文说明 `SellProduct`（售卖产品）任务的生成链路、Pipeline 组织、全局物品优先级、自动干员选择与新增据点/物品时的维护方式。

`SellProduct` 的核心特点是 **zmdmap 数据驱动 + Pipeline 模板生成**：据点、可售卖物品、任务选项和各据点重复节点不是逐个手写出来的，而是由 `tools/pipeline-generate/SellProduct/` 读取 `tools/pipeline-generate/data/settlement_trade.json` 后批量渲染。`settlement_trade.json` 由 `pnpm fetch:zmdmap` 从 zmdmap API 下载并缓存。

> [!IMPORTANT]
>
> `assets/tasks/SellProduct.json`、`assets/resource/pipeline/SellProduct/OperatorSession.json`、`assets/resource/pipeline/SellProduct/Outposts/*.json` 和 `assets/resource_adb/pipeline/SellProduct/Outposts/*.json` 都是 **生成产物**。不要直接手改这些文件；需要改据点、商品列表、自动干员注册、优先物品候选、售卖尝试模板或 Win/ADB 坐标时，应修改 `tools/pipeline-generate/SellProduct/` 下的数据装配或模板，然后重新生成。

## 概览

SellProduct 的核心维护点如下：

| 模块               | 路径                                                              | 作用                                                                                            |
| ------------------ | ----------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| zmdmap 缓存数据    | `tools/pipeline-generate/data/settlement_trade.json`              | 据点、繁荣度、可交易物品、多语言名称、稀有度、单价等原始数据                                    |
| 共享据点模型       | `tools/pipeline-generate/SellProduct/model.mjs`                   | 读取 zmdmap，统一派生 `RegionPrefix` / `LocationId` / 多语言 OCR 与 locale 键                   |
| Win 模板数据       | `tools/pipeline-generate/SellProduct/pipeline-data.mjs`           | 只输出 Win Pipeline 模板需要的据点字段和数量识别框                                              |
| ADB 模板数据       | `tools/pipeline-generate/SellProduct/pipeline-adb-data.mjs`       | 只输出 ADB Pipeline 模板需要的据点 ID 和数量识别框                                              |
| 会话模板数据       | `tools/pipeline-generate/SellProduct/session-data.mjs`            | 只输出自动干员据点注册节点需要的字段                                                    |
| Task 模板数据      | `tools/pipeline-generate/SellProduct/task-data.mjs`               | 生成全局物品优先级、缓存刷新和地区/据点开关                                                    |
| 据点 Pipeline 模板 | `tools/pipeline-generate/SellProduct/pipeline-template.jsonc`     | 生成 Win 资源包的每个据点售卖节点                                                               |
| 会话 Pipeline 模板 | `tools/pipeline-generate/SellProduct/session-template.jsonc`      | 生成自动模式据点注册节点                                                                        |
| ADB 据点模板       | `tools/pipeline-generate/SellProduct/pipeline-adb-template.jsonc` | 生成 ADB 资源包的据点数量 OCR 覆盖节点                                                          |
| 任务选项模板       | `tools/pipeline-generate/SellProduct/task-template.jsonc`         | 生成 `assets/tasks/SellProduct.json` 中的全局优先级、缓存刷新和地区/据点开关                         |
| Win 据点生成配置   | `tools/pipeline-generate/SellProduct/pipeline-config.json`        | 输出到 `assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json`                       |
| ADB 据点生成配置   | `tools/pipeline-generate/SellProduct/pipeline-adb-config.json`    | 输出到 `assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json`                   |
| 任务选项生成配置   | `tools/pipeline-generate/SellProduct/task-config.json`            | 输出到 `assets/tasks/SellProduct.json`                                                          |
| 会话生成配置       | `tools/pipeline-generate/SellProduct/session-config.json`         | 输出到 `assets/resource/pipeline/SellProduct/OperatorSession.json`                              |
| 任务入口           | `assets/resource/pipeline/SellProduct.json`                       | 主循环、地区入口；手写维护                                                                     |
| 地区售卖入口       | `assets/resource/pipeline/SellProduct/Sell.json`                  | 地区到据点的 `next` 列表；手写维护                                                              |
| 通用售卖核心       | `assets/resource/pipeline/SellProduct/SellCore.json`              | 售卖循环、缺货/调度券不足/超出兑换上限处理、最终交易流程                                        |
| 通用换货流程       | `assets/resource/pipeline/SellProduct/ChangeGoods.json`           | 进入选择货品界面、选择优先物品或默认物品                                                        |
| 据点通用识别       | `assets/resource/pipeline/SellProduct/EnterOutpost.json`          | 据点界面、地区据点页和据点管理文本识别                                                          |
| 联络干员通用识别   | `assets/resource/pipeline/SellProduct/Operator.json`              | 联络干员列表界面和打开按钮识别                                                                  |
| 自动干员会话/算法  | `agent/go-service/sellproduct/operator_*.go`                      | 理论/精确候选、缓存完整性、任务会话和恢复唯一分配                                               |
| ADB 通用售卖核心   | `assets/resource_adb/pipeline/SellProduct/SellCore.json`          | ADB 资源包下的通用售卖核心                                                                      |
| 优先物品自定义识别 | `agent/go-service/sellproduct/normalized_match.go`                | `SellProductNormalizedItemMatch`，对 OCR 结果和候选名做抗噪声精确匹配                           |
| 多语言文案         | `assets/locales/interface/*.json`                                 | `SellProduct` 任务文案、据点名、物品 label                                                      |
| 生成入口           | `package.json` 的 `generate:SellProduct` / `fetch:zmdmap`         | 更新 zmdmap 缓存并重新渲染生成产物                                                              |

## 生成产物与手写文件边界

### 生成产物

以下文件由 `@joebao/maa-pipeline-generate` 渲染，重新生成时会被覆盖：

- `assets/tasks/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Outposts/*.json`
- `assets/resource/pipeline/SellProduct/OperatorSession.json`
- `assets/resource_adb/pipeline/SellProduct/Outposts/*.json`

这些文件的来源分别是：

| 产物                            | 模板                          | 数据源                                |
| ------------------------------- | ----------------------------- | ------------------------------------- |
| `assets/tasks/SellProduct.json` | `task-template.jsonc`         | `task-data.mjs` + `model.mjs`         |
| Win 据点 Pipeline               | `pipeline-template.jsonc`     | `pipeline-data.mjs` + `model.mjs`     |
| 自动干员注册节点                | `session-template.jsonc`      | `session-data.mjs` + `model.mjs`      |
| ADB 据点数量 OCR 覆盖           | `pipeline-adb-template.jsonc` | `pipeline-adb-data.mjs` + `model.mjs` |

### 手写维护文件

以下文件不会由 SellProduct 生成器覆盖，维护者需要按业务变化手动更新：

- `assets/resource/pipeline/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Sell.json`
- `assets/resource/pipeline/SellProduct/SellCore.json`
- `assets/resource/pipeline/SellProduct/ChangeGoods.json`
- `assets/resource/pipeline/SellProduct/EnterOutpost.json`
- `assets/resource/pipeline/SellProduct/Operator.json`
- `assets/resource/pipeline/SellProduct/OperatorScan.json`
- `assets/resource_adb/pipeline/SellProduct/SellCore.json`
- `agent/go-service/sellproduct/*.go`
- `assets/locales/interface/*.json`

新增地区或据点时，生成器能生成任务选项和据点节点，但地区入口、地区到据点的 `next` 列表、SceneManager 跳转节点、据点管理页入口识别等仍可能需要手写补齐。

## 命名规则与数据模型

### 据点节点 ID（`LocationId`）

`LocationId` 是生成出的据点节点名前缀和文件名：

```text
assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json
assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json
```

`LocationId` 始终由 zmdmap 当前英文据点名转 PascalCase 得到，不再维护手写 ID 覆盖。英文名称变化时，重新生成会同步更新节点名、文件名、Task 选项与 locale 键。

`LocationId` 只用于节点名和文件名，不是展示文案。用户界面上的据点名由 `assets/locales/interface/*.json` 中的 `task.SellProduct.{RegionPrefix}{LocationId}` 提供。

### 地区前缀（`RegionPrefix`）

`RegionPrefix` 是任务选项和地区入口节点使用的地区 ID，例如 `ValleyIV`、`Wuling`。它由 `DOMAIN_REGION_PREFIX` 将 zmdmap 的 `domainId` 映射而来。

新增地区时不要直接依赖 `domain_3` 这类默认回退名，应先在 `DOMAIN_REGION_PREFIX` 里配置稳定的项目地区 ID。

### zmdmap 数据字段

`settlement_trade.json` 当前主要提供：

- `settlements`：据点列表，key 形如 `stm_tundra_1`。
- `settlement.domainId`：据点所属地区，例如 `domain_1`、`domain_2`。
- `settlement.settlementName`：据点多语言名称。
- `settlement.byProsperityLevel[*].tradeItems`：不同繁荣度下的可交易物品列表。
- `tradeItems[*].itemId`：物品 ID。
- `tradeItems[*].name`：物品多语言名称。
- `tradeItems[*].rarity` / `unitPrice`：用于生成优先物品选项的排序。

`model.mjs` 会把这些原始数据规范化为 `sellProductLocations`，再由 Win、ADB、会话和 Task 四个数据投影各自生成最小模板行。

当前已生成的据点为：

| zmdmap settlementId | 地区     | LocationId                     | 据点名       |
| ------------------- | -------- | ------------------------------ | ------------ |
| `stm_tundra_1`      | ValleyIV | `RefugeeCamp`                  | 难民暂居处   |
| `stm_tundra_2`      | ValleyIV | `InfraStation`                 | 基建前站     |
| `stm_tundra_3`      | ValleyIV | `ReconstructionHQ`             | 重建指挥部   |
| `stm_hongs_1`       | Wuling   | `SkyKingFlatsConstructionSite` | 天王坪援建点 |
| `stm_hongs_2`       | Wuling   | `CardiacRemediationStation`    | 心脏修缮站   |
| `stm_hongs_3`       | Wuling   | `XiranflowCloudseederStation`  | 盈天台建设站 |

## 自动生成机制

### 运行命令

```shell
# 推荐：在仓库根目录运行，自动更新 zmdmap 缓存并重新生成
pnpm generate:SellProduct

# 只更新 zmdmap 缓存
pnpm fetch:zmdmap

# 已经更新过缓存时，也可以在生成器目录单独渲染
cd tools/pipeline-generate/SellProduct
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config session-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

### Win 据点 Pipeline：`pipeline-config.json`

```json
{
    "template": "pipeline-template.jsonc",
    "data": "pipeline-data.mjs",
    "outputDir": "../../../assets/resource/pipeline/SellProduct/Outposts",
    "outputPattern": "${LocationId}.json",
    "format": true,
    "merged": false
}
```

每一行数据生成一个 Win 资源包据点文件。

### ADB 据点 Pipeline：`pipeline-adb-config.json`

```json
{
    "template": "pipeline-adb-template.jsonc",
    "data": "pipeline-adb-data.mjs",
    "outputDir": "../../../assets/resource_adb/pipeline/SellProduct/Outposts",
    "outputPattern": "${LocationId}.json",
    "format": true,
    "merged": false
}
```

ADB 据点模板不是完整复制 Win 据点流程，而是只生成各据点 4 个 `BetterSliding` 节点的覆盖配置。它们会把数量 OCR 区域替换为 `QuantityBoxAdb` 与 `MaxTargetBoxAdb`，其余据点流程继续复用 Win 资源包生成出的节点结构。

### 任务选项：`task-config.json`

```json
{
    "task": true,
    "template": "task-template.jsonc",
    "data": "task-data.mjs",
    "outputDir": "../../../assets/tasks/",
    "outputFile": "SellProduct.json",
    "format": true
}
```

该配置生成用户界面中的全局物品优先级、强制刷新干员缓存，以及地区和据点售卖开关。

### 共享模型与模板投影

`tools/pipeline-generate/SellProduct/model.mjs` 是据点命名、OCR 和 locale 的共享维护入口；`pipeline-data.mjs`、`pipeline-adb-data.mjs`、`session-data.mjs` 和 `task-data.mjs` 分别维护模板专属数据。

它当前负责：

1. 读取 `tools/pipeline-generate/data/settlement_trade.json`。
2. 从 `assets/locales/interface/zh_cn.json` 反查 `item.*` key，尽量把任务选项 label 生成为 `$item.xxx`。
3. 从 zmdmap 的 `tradeItems` 构建全局物品字典。
4. 按据点统计可售卖物品，并按 `rarity`、`unitPrice` 降序排列。
5. 将 `domainId` 映射成任务使用的 `RegionPrefix`。
6. `model.mjs` 从英文据点名自动派生 `LocationId`，并从五语言 `settlementName` 生成 OCR `TextExpected`。
7. 四个投影分别注入 Win / ADB 数量 OCR 区域、会话注册链和 Task 选项。

### OCR 兼容值

`TextExpected` 默认直接从 zmdmap 的 CN / TC / JP / KR / EN 全文生成。只有存在实际识别证据的固定误识或 UI 截断时，才在 `SETTLEMENT_OCR_ALIASES` 中追加候选；该列表不会替换官方全文。

### 地区映射覆盖

`DOMAIN_REGION_PREFIX` 负责把 zmdmap 的 `domainId` 映射到项目中的地区 ID：

```js
const DOMAIN_REGION_PREFIX = {
    domain_1: "ValleyIV",
    domain_2: "Wuling",
};
```

新地区接入时，如果 zmdmap 新增了 `domain_3`，通常需要先在这里添加稳定的 `RegionPrefix`。未配置的 domain 会回退到 `toPascalCase(domainId)`，这通常不适合直接作为用户可见配置和 Pipeline 前缀。

### 临时排除活动物品

`TEMP_EXCLUDED_ITEM_CN_NAMES` 用于临时排除仍出现在 zmdmap 数据中、但不应该继续出现在售卖配置里的活动物品。

维护规则：

- 只用于短期兼容活动数据。
- 注释里应写清楚删除条件。
- 当 zmdmap 数据更新并确认活动物品已移除后，应删除对应排除项。

### 优先物品候选名

生成出的每个优先物品选项会覆盖对应节点：

```text
SellProduct{LocationId}SelectItem{N}
```

覆盖内容包括：

- `enabled: true`
- `custom_recognition_param.candidates`
- 未命中处理 anchor

`candidates` 来自 zmdmap 的 CN / TC / JP / EN 名称。英文名会去掉部分容易干扰匹配的符号后再进入候选。

## 主流程

整体流程可以按以下链路理解：

```text
SellProductMain
-> SellProductCaptureUid
-> SellProductInitializeOperatorSession / SellProductRegisterAuto{LocationId}
-> SellProductLoop
-> SellProductAuto / SellProductValleyIV / SellProductWuling
-> SellProduct{Region}Sell
-> SellProduct{LocationId}
-> SellProduct{LocationId}Sell
-> SellProduct{LocationId}SetBeforeSellOperatorAnchor
-> SellProduct{LocationId}SetAfterSellOperatorAnchor
-> SellProduct{LocationId}BeforeSellOperator
-> SellProductSellLoop
-> SellProduct{LocationId}SellAttempt{1..4}
-> SellProductChangeGoods
-> SellProduct{LocationId}SelectItem{1..4} / SellProductSelectFirstGood / SellProductSelectNextGood
-> SellProduct{LocationId}BetterSliding{1..4}
-> SellProductSell
-> SellProductSellCheck / SellProductSellCheckThenLoop
-> SellProductSellLoop 或 SellProductSellLoopEnd
-> SellProduct{LocationId}AfterSellOperator
```

关键点：

- 任务直接从 `SellProductMain` 开始，不再暴露执行周期配置。
- `SellProductCaptureUid` 先捕获哈希 UID，随后初始化任务级自动干员会话并注册已启用的据点。
- `SellProductLoop` 只在地区建设界面继续执行；不在目标界面时交给 `SceneEnterMenuRegionalDevelopment`。
- `SellProductAuto` 会根据当前地区建设页面自动选择四号谷地或武陵。
- `SellProduct{Region}Sell` 进入对应地区的据点管理页，然后按 `next` 遍历该地区所有据点。
- 每个据点节点由模板生成，负责识别当前据点、点击据点标签、设置售卖锚点并自动切换最优联络干员。
- 售卖前会通过 `SellProduct{LocationId}BeforeSellOperator` 检查当前干员，不一致时打开联络干员列表、选择目标干员，并在按钮变为「派驻」后确认派驻；售卖结束后自动恢复原岗位。
- `SellProductSellLoop` 通过 anchor 串起最多 4 次售卖尝试。
- 每次尝试先换货，再用 BetterSliding 把数量调到目标值，最后点击交易。
- `SellProductSellLoopEnd` 会通过 `SellProductAfterSellOperator` anchor 进入 `SellProduct{LocationId}AfterSellOperator`，恢复完成后结束该据点流程。

## 任务选项如何改 Pipeline

`assets/tasks/SellProduct.json` 由 `task-template.jsonc` 生成。用户在界面中选择的配置会通过 `pipeline_override` 修改 Pipeline。

### 保留的选项

| 选项                                   | 行为                                                                                      |
| -------------------------------------- | ----------------------------------------------------------------------------------------- |
| `SellProductGlobalItemPriority`        | 控制是否启用全局优先物品；关闭时按默认货品执行前两次售卖                                  |
| `SellProductPriorityItem{1..4}`        | 总开关下同级显示的四档售卖物品优先级，统一应用于全部据点                                  |
| `SellProductForceRefreshOperatorCache` | 控制本次任务是否在售卖前完整刷新干员缓存                                                  |
| `{RegionPrefix}Sell`                   | 控制整个地区是否参与售卖，并展开该地区的据点开关                                          |
| `{RegionPrefix}{LocationId}`           | 控制单个据点是否参与售卖，同时启用或禁用对应的 `SellProductRegisterAuto{LocationId}` 节点 |

已移除执行周期、超出兑换上限自动确认、手动干员、售卖次数和保留份数等选项。固定行为为：超出兑换上限时停止并提示、具体货品全部售出、启用据点始终自动选择最优干员并在售卖后恢复。

### 自动选择干员状态机

自动选择由 Pipeline 和 Go 协作完成：Pipeline 负责打开、滚动、关闭列表及派驻确认，Go 只负责候选数据、缓存状态和恢复分配算法。

任务入口在捕获 UID 后执行：

```text
SellProductInitializeOperatorSession
-> SellProductRegisterAuto{LocationId}（仅启用自动模式的据点）
-> SellProductOperatorSessionReady
-> SellProductLoop
```

会话初始化会清空上次任务的扫描完成状态、计划和恢复锁定。`session-template.jsonc` 为每个据点生成一个默认禁用的注册节点，据点售卖开关通过 `pipeline_override` 启用对应节点。新增据点后必须重新生成 `OperatorSession.json`，并把新注册节点补入手写的 `SellProductInitializeOperatorSession.next`。

#### 缓存完整性

拥有干员缓存位于 `debug/record/SellProductOwnedOperators.json`，按哈希 UID 分区。账号数据包含：

```json
{
    "updated_at": "2026-07-15T00:00:00Z",
    "operators": [
        "佩丽卡",
        "莱万汀"
    ],
    "complete": true
}
```

- `complete: false`：只表示列表局部观察到这些干员，不能据此排除其他候选。
- `complete: true`：本次从列表顶部完整遍历到底，可以据此计算精确方案。
- v1 缓存在读取时迁移为 v2 完整快照，下一次写入时落盘新结构。
- 局部命中只做集合追加；只有列表到底才允许替换候选域并设置 `complete: true`。

#### 理论计划与精确计划

缓存不完整时，Go 暂时假设相关候选都可能拥有，并计算理论最优解。每次只搜索计划指定的第一候选：

```text
计算理论最优
-> 当前干员已是目标：直接继续
-> 打开列表并只搜索计划候选
   -> 找到：选择并记录局部观察
   -> 到底未找到：写入完整缓存并重新计算
      -> 新方案存在：关闭并重新打开列表，只重选一次
      -> 无售卖候选：停止任务并提示
      -> 无恢复候选：关闭列表并继续
```

“重新选择一次”不是盲目重试：第一次到底已经产生了新的完整拥有集合，第二次使用的是重新计算后的不同计划。若新计划来源于刚完成的完整扫描却仍无法识别，应按 OCR/界面异常处理，不应继续循环。

目标干员优先级为：经验与信用点双加成、仅信用点、仅经验；同等级按游戏干员列表顺序稳定排序。列表搜索不能选择当前页可见的次优项，否则无法保证全局最优。

#### 恢复分配与锁定

恢复干员需要满足同一干员不能同时派驻多个据点。Go 仅对会话中注册的自动据点求解：

1. 最大化能够恢复的据点数。
2. 在覆盖数相同时，最小化候选优先级总和。
3. 已确认恢复完成的 `location -> operator` 写入锁定集合。
4. 缓存刷新后只重新规划尚未完成的据点，锁定干员不可再次分配；已完整扫描并确认无恢复候选的据点也会标记为完成，避免继续为它预留共享干员。

`SellProduct{LocationId}CurrentRestoreOperator` 在当前干员已经正确时直接锁定；正常选择则在 `SellProduct{LocationId}RestoreOperatorDone` 确认回到据点界面后锁定，避免点击或确认失败时提前占用。

#### 强制刷新

“本次运行前强制刷新”把任务入口会话模式设为 `refresh`。地区售卖前的 `SellProductScanOperatorList` 只有在本次任务尚未完成全列表扫描时才执行；完成标记属于任务会话，不跨任务复用。第一次完整扫描可供同一任务中的所有地区和据点共享。

### 全局物品优先级

`SellProductGlobalItemPriority` 默认关闭，此时不会进行优先货品识别，而是在每个启用据点按默认货品执行前两次售卖。开启后，四个 `SellProductPriorityItem{1..4}` 顺位统一应用于所有启用据点：

- 第 1、2 档默认使用 `Auto`，分别按每个据点自身的稀有度、单价排序选择第一和第二件货品。
- 第 3、4 档默认使用 `None`，不会执行对应售卖尝试。
- 选择具体货品时，只启用实际出售该货品的据点；其他据点会跳过该顺位，不会因列表中不存在该货品而产生误售。
- 命中具体货品后使用默认 BetterSliding 配置全部售出，不再支持保留指定份数。
- 若已启用的优先货品在界面中未识别到，流程仍通过 `SellProductPriorityGoodMissWarning` 提示并回退选择默认货品。

## 优先物品识别

优先物品节点使用 Go 自定义识别：

```text
SellProductNormalizedItemMatch
```

实现文件：

```text
agent/go-service/sellproduct/normalized_match.go
```

这个识别器会在选择货品界面的 ROI 内运行 OCR，然后对 OCR 文本和 `candidates` 做两层严格匹配：

1. Tier A：剥除空白、方括号、竖线、连字符、点号、顿号等常见分隔符，并统一 ASCII 大小写后严格相等。
2. Tier B：在 Tier A 基础上剥除 ASCII 字母和数字，用于处理 CJK 名称前后混入英文噪声的情况。

维护时要注意：

- 不要把它改成宽松编辑距离匹配，否则容易把“柑实罐头”误匹配成“优质柑实罐头”或“精选柑实罐头”。
- 新增候选名时应优先从 zmdmap 多语言名称生成。
- 如果 OCR 有固定噪声，优先把有证据的候选补进 `model.mjs` 的 `SETTLEMENT_OCR_ALIASES`，而不是扩大匹配算法。
- 修改匹配算法后应运行 `agent/go-service/sellproduct/normalized_match_test.go` 覆盖的回归测试。

## BetterSliding 与数量区域

每个据点会生成 4 个 BetterSliding 节点：

```text
SellProduct{LocationId}BetterSliding1
SellProduct{LocationId}BetterSliding2
SellProduct{LocationId}BetterSliding3
SellProduct{LocationId}BetterSliding4
```

默认参数：

- `Target: 999999`
- `ClampTargetToMax: true`
- `Direction: "right"`
- `MaxTarget.Box`：读取最大可售数量。
- `Quantity.Box`：读取当前交易份数。
- `ExceedingOverrideEnable: "SellProductSkipToNextSellLoop"`

数量区域分别在 `pipeline-data.mjs` 和 `pipeline-adb-data.mjs` 维护：

| 常量                   | 用途                       |
| ---------------------- | -------------------------- |
| `QUANTITY_BOX`         | Win 资源包当前交易份数 OCR |
| `MAX_QUANTITY_BOX`     | Win 资源包最大可售数量 OCR |
| `QUANTITY_BOX_ADB`     | ADB 资源包当前交易份数 OCR |
| `MAX_QUANTITY_BOX_ADB` | ADB 资源包最大可售数量 OCR |

如果游戏 UI 调整了数量位置，只改这些常量，再重新生成即可同步所有据点和 4 次尝试。

## 维护流程

### 更新 zmdmap 数据并重新生成

```shell
pnpm generate:SellProduct
```

这个命令会先执行 `pnpm fetch:zmdmap` 的等价逻辑，更新 `tools/pipeline-generate/data/settlement_trade.json`，再依次运行 `SellProduct` 目录下的生成配置。

### zmdmap 新增可售卖物品

1. 运行 `pnpm generate:SellProduct`。
2. 检查 `assets/tasks/SellProduct.json` 中对应据点的优先物品选项是否出现新物品。
3. 若新物品 label 没有生成 `$item.xxx`，在 `assets/locales/interface/*.json` 中补齐对应 `item.*` 多语言文案。
4. 若 OCR 名称有固定误识别，根据实际证据补充 `model.mjs` 的 `SETTLEMENT_OCR_ALIASES`。

普通新增物品通常不需要改据点 Pipeline 模板。

### zmdmap 新增据点

1. 运行 `pnpm fetch:zmdmap` 更新缓存。
2. 确认 `model.mjs` 从英文名派生的 `LocationId` 和五语言 `TextExpected`正确；有实际 OCR 异常时再补 `SETTLEMENT_OCR_ALIASES`。
3. 如果是新地区，补 `DOMAIN_REGION_PREFIX`。
4. 运行 `pnpm generate:SellProduct`。
5. 在 `assets/resource/pipeline/SellProduct/Sell.json` 中把新据点加入对应地区的 `next` 列表。
6. 如有新地区，在 `assets/resource/pipeline/SellProduct.json` 中补地区入口和自动选择逻辑。
7. 补齐 SceneManager 进入该地区据点管理页所需的节点。
8. 在 `assets/locales/interface/*.json` 中补齐 `task.SellProduct.{RegionPrefix}{LocationId}` 和新地区文案。
9. 检查 Win 与 ADB 两套生成结果。

生成器不会自动判断某个新据点在游戏 UI 中如何进入，也不会自动补 SceneManager 跳转。

### 据点 OCR 不稳定

优先检查：

- `SellProductCheck{LocationId}TabText`
- `SellProductCheck{LocationId}Text`
- `SETTLEMENT_OCR_ALIASES[settlementId]`

如果是固定误识别文本，直接把候选补到 `TextExpected`。如果只是 ROI 不合适，需要改 `pipeline-template.jsonc` 和 `pipeline-adb-template.jsonc` 中对应 OCR 节点的 `roi`，然后重新生成。

### 优先物品经常选不到

排查顺序：

1. 确认任务选项是否真的选择了该优先物品。
2. 查看生成出的 `SellProduct{LocationId}SelectItem{N}.custom_recognition_param.candidates`。
3. 检查 zmdmap 多语言名称是否包含游戏 UI 实际显示名。
4. 查看 Go 日志中 `SellProductNormalizedItemMatch` 的 `ocr_texts` 与 `candidates`。
5. 固定噪声优先补候选；只有算法确实无法表达时才改 Go 匹配逻辑。

## 自检清单

修改生成器或数据后建议执行：

```shell
pnpm generate:SellProduct
pnpm prettier --write "docs/zh_cn/developers/tasks/sell-product-maintain.md" "docs/zh_cn/developers/README.md"
```

如果改了 Go 匹配逻辑：

```shell
cd agent/go-service
go test ./sellproduct
```

提交前至少检查：

1. `assets/tasks/SellProduct.json` 是否符合 interface V2。
2. 生成的据点文件是否没有残留旧据点。
3. `SellProduct/Sell.json` 中地区 `next` 是否包含对应据点。
4. 任务选项里的全局优先级、缓存刷新、地区和据点开关层级是否完整。
5. Win 与 ADB 两套 `Outposts/*.json` 是否都已重新生成。
6. JSON/Markdown 是否符合 `.prettierrc`。

## 常见坑

- **直接手改生成产物**：下次运行 `pnpm generate:SellProduct` 会覆盖改动。应改 `model.mjs`、对应模板数据、模板或手写联动文件。
- **只生成 Win 没生成 ADB**：`pipeline-adb-config.json` 负责 ADB 据点节点。涉及数量区域、据点 OCR、售卖尝试模板时要同时确认 ADB 产物。
- **新增物品没有可翻译 label**：`task-data.mjs` 会从 `zh_cn.json` 反查 `item.*` key。找不到时仍能生成选项，但 label 会退回普通名称；需要补齐多语言。
- **新增地区后任务选项有了，但流程进不去**：任务选项生成不等于入口链路完成。还需要补 `SellProduct.json`、`Sell.json` 和 SceneManager 跳转。
- **扩大优先物品匹配导致串货**：不要用宽松相似度替代当前严格匹配。相近商品名很多，匹配策略必须避免子串误命中。

## 致谢

SellProduct 的据点与可交易物品数据来自 `zmdmap`，由 `pnpm fetch:zmdmap` 下载到 `tools/pipeline-generate/data/settlement_trade.json` 后参与生成。
