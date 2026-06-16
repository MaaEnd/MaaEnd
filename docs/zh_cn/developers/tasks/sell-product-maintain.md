# 开发手册 - SellProduct 维护文档

本文说明 `SellProduct`（售卖产品）任务的生成链路、Pipeline 组织、任务选项、优先物品匹配、保留份数与新增据点/物品时的维护方式。

`SellProduct` 的核心特点是 **zmdmap 数据驱动 + Pipeline 模板生成**：据点、可售卖物品、任务选项和各据点重复节点不是逐个手写出来的，而是由 `tools/pipeline-generate/SellProduct/` 读取 `tools/pipeline-generate/data/settlement_trade.json` 后批量渲染。`settlement_trade.json` 由 `pnpm fetch:zmdmap` 从 zmdmap API 下载并缓存。

> [!IMPORTANT]
>
> `assets/tasks/SellProduct.json`、`assets/resource/pipeline/SellProduct/Outposts/*.json` 和 `assets/resource_adb/pipeline/SellProduct/Outposts/*.json` 都是 **生成产物**。不要直接手改这些文件；需要改据点、商品列表、优先物品候选、售卖尝试模板或 Win/ADB 坐标时，应修改 `tools/pipeline-generate/SellProduct/` 下的数据装配或模板，然后重新生成。

## 概览

SellProduct 的核心维护点如下：

| 模块               | 路径                                                              | 作用                                                                                            |
| ------------------ | ----------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| zmdmap 缓存数据    | `tools/pipeline-generate/data/settlement_trade.json`              | 据点、繁荣度、可交易物品、多语言名称、稀有度、单价等原始数据                                    |
| 数据装配           | `tools/pipeline-generate/SellProduct/data.mjs`                    | 将 zmdmap 数据转换成模板可消费的 `settlementFlatRows`                                           |
| 据点 Pipeline 模板 | `tools/pipeline-generate/SellProduct/pipeline-template.jsonc`     | 生成 Win 资源包的每个据点售卖节点                                                               |
| ADB 据点模板       | `tools/pipeline-generate/SellProduct/pipeline-adb-template.jsonc` | 生成 ADB 资源包的据点数量 OCR 覆盖节点                                                          |
| 任务选项模板       | `tools/pipeline-generate/SellProduct/task-template.jsonc`         | 生成 `assets/tasks/SellProduct.json` 中的地区、据点、干员切换、售卖尝试、优先物品和保留份数选项 |
| Win 据点生成配置   | `tools/pipeline-generate/SellProduct/pipeline-config.json`        | 输出到 `assets/resource/pipeline/SellProduct/Outposts/${LocationId}.json`                       |
| ADB 据点生成配置   | `tools/pipeline-generate/SellProduct/pipeline-adb-config.json`    | 输出到 `assets/resource_adb/pipeline/SellProduct/Outposts/${LocationId}.json`                   |
| 任务选项生成配置   | `tools/pipeline-generate/SellProduct/task-config.json`            | 输出到 `assets/tasks/SellProduct.json`                                                          |
| 任务入口           | `assets/resource/pipeline/SellProduct.json`                       | `ScheduleRecognition`、主循环、地区入口；手写维护                                               |
| 地区售卖入口       | `assets/resource/pipeline/SellProduct/Sell.json`                  | 地区到据点的 `next` 列表；手写维护                                                              |
| 通用售卖核心       | `assets/resource/pipeline/SellProduct/SellCore.json`              | 售卖循环、缺货/调度券不足/超出兑换上限处理、最终交易流程                                        |
| 通用换货流程       | `assets/resource/pipeline/SellProduct/ChangeGoods.json`           | 进入选择货品界面、选择优先物品或默认物品                                                        |
| 据点通用识别       | `assets/resource/pipeline/SellProduct/EnterOutpost.json`          | 据点界面、地区据点页和据点管理文本识别                                                          |
| 联络干员通用识别   | `assets/resource/pipeline/SellProduct/Operator.json`              | 联络干员列表界面和打开按钮识别                                                                  |
| ADB 通用售卖核心   | `assets/resource_adb/pipeline/SellProduct/SellCore.json`          | ADB 资源包下的通用售卖核心                                                                      |
| 优先物品自定义识别 | `agent/go-service/sellproduct/normalized_match.go`                | `SellProductNormalizedItemMatch`，对 OCR 结果和候选名做抗噪声精确匹配                           |
| 多语言文案         | `assets/locales/interface/*.json`                                 | `SellProduct` 任务文案、据点名、物品 label                                                      |
| 生成入口           | `package.json` 的 `generate:SellProduct` / `fetch:zmdmap`         | 更新 zmdmap 缓存并重新渲染生成产物                                                              |

## 生成产物与手写文件边界

### 生成产物

以下文件由 `@joebao/maa-pipeline-generate` 渲染，重新生成时会被覆盖：

- `assets/tasks/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Outposts/*.json`
- `assets/resource_adb/pipeline/SellProduct/Outposts/*.json`

这些文件的来源分别是：

| 产物                            | 模板                          | 数据源                   |
| ------------------------------- | ----------------------------- | ------------------------ |
| `assets/tasks/SellProduct.json` | `task-template.jsonc`         | `data.mjs` + zmdmap 缓存 |
| Win 据点 Pipeline               | `pipeline-template.jsonc`     | `data.mjs` + zmdmap 缓存 |
| ADB 据点数量 OCR 覆盖           | `pipeline-adb-template.jsonc` | `data.mjs` + zmdmap 缓存 |

### 手写维护文件

以下文件不会由 SellProduct 生成器覆盖，维护者需要按业务变化手动更新：

- `assets/resource/pipeline/SellProduct.json`
- `assets/resource/pipeline/SellProduct/Sell.json`
- `assets/resource/pipeline/SellProduct/SellCore.json`
- `assets/resource/pipeline/SellProduct/ChangeGoods.json`
- `assets/resource/pipeline/SellProduct/EnterOutpost.json`
- `assets/resource/pipeline/SellProduct/Operator.json`
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

默认情况下，`LocationId` 由 zmdmap 英文据点名转 PascalCase 得到。实际维护中优先看 `data.mjs` 的 `SETTLEMENT_OVERRIDE`：如果某个据点在这里配置了 `LocationId`，生成器会使用覆盖值。

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

`data.mjs` 会把这些原始数据装配成每个据点一行的 `settlementFlatRows`，再交给三个生成配置消费。

当前已生成的据点为：

| zmdmap settlementId | 地区     | LocationId                  | 据点名       |
| ------------------- | -------- | --------------------------- | ------------ |
| `stm_tundra_1`      | ValleyIV | `RefugeeCamp`               | 难民暂居处   |
| `stm_tundra_2`      | ValleyIV | `InfrastructureOutpost`     | 基建前站     |
| `stm_tundra_3`      | ValleyIV | `ReconstructionCommand`     | 重建指挥部   |
| `stm_hongs_1`       | Wuling   | `SkyKingFlats`              | 天王坪援建点 |
| `stm_hongs_2`       | Wuling   | `CardiacRemediationStation` | 心脏修缮站   |

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
npx @joebao/maa-pipeline-generate --config task-config.json
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

### Win 据点 Pipeline：`pipeline-config.json`

```json
{
    "template": "pipeline-template.jsonc",
    "data": "data.mjs",
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
    "data": "data.mjs",
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
    "data": "data.mjs",
    "outputDir": "../../../assets/tasks/",
    "outputFile": "SellProduct.json",
    "format": true
}
```

该配置生成用户界面中的地区开关、据点开关、联络干员切换、4 次售卖尝试、优先物品和保留份数配置。

### 数据装配：`data.mjs`

`tools/pipeline-generate/SellProduct/data.mjs` 是 SellProduct 生成器的主要维护入口。

它当前负责：

1. 读取 `tools/pipeline-generate/data/settlement_trade.json`。
2. 从 `assets/locales/interface/zh_cn.json` 反查 `item.*` key，尽量把任务选项 label 生成为 `$item.xxx`。
3. 从 zmdmap 的 `tradeItems` 构建全局物品字典。
4. 按据点统计可售卖物品，并按 `rarity`、`unitPrice` 降序排列。
5. 将 `domainId` 映射成任务使用的 `RegionPrefix`。
6. 为据点生成 `LocationId`、据点 OCR `TextExpected`、任务选项、优先物品候选名。
7. 注入 Win / ADB 两套 BetterSliding 数量 OCR 区域。

### 据点命名覆盖

`SETTLEMENT_OVERRIDE` 用于处理 zmdmap 原始名称不适合直接生成节点 ID，或 OCR 需要特殊候选文本的情况。

当前覆盖项包括：

- `LocationId`：覆盖默认的 `toPascalCase(EN)`，决定生成出的节点前缀和文件名。
- `TextExpected`：覆盖据点 OCR 候选。填写后会完全替代默认 CN / TC / JP / EN 候选，需要自行覆盖必要语言和常见 OCR 噪声。

典型场景：

- 英文名太长或不符合项目命名习惯。
- 游戏 UI 中实际显示与 zmdmap 名称有差异。
- OCR 常把某个据点识别成固定错误文本，例如把 `HQ` 读偏。

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
SellProductSchedule
-> SellProductMain
-> SellProductLoop
-> SellProductAuto / SellProductValleyIV / SellProductWuling
-> SellProduct{Region}Sell
-> SellProduct{LocationId}
-> SellProduct{LocationId}Sell
-> SellProduct{LocationId}SetBeforeSellOperatorAnchor
-> SellProduct{LocationId}SetAfterSellOperatorAnchor
-> SellProduct{LocationId}BeforeSellOperator（可选）
-> SellProductSellLoop
-> SellProduct{LocationId}SellAttempt{1..4}
-> SellProductChangeGoods
-> SellProduct{LocationId}SelectItem{1..4} / SellProductSelectFirstGood / SellProductSelectNextGood
-> SellProduct{LocationId}BetterSliding{1..4}
-> SellProductSell
-> SellProductSellCheck / SellProductSellCheckThenLoop
-> SellProductSellLoop 或 SellProductSellLoopEnd
-> SellProduct{LocationId}AfterSellOperator（可选）
```

关键点：

- `SellProductScheduleEnabled` 通过 `ScheduleRecognition` 判断用户选择的星期，命中后由 Pipeline 进入 `SellProductMain`。
- `SellProductLoop` 只在地区建设界面继续执行；不在目标界面时交给 `SceneEnterMenuRegionalDevelopment`。
- `SellProductAuto` 会根据当前地区建设页面自动选择四号谷地或武陵。
- `SellProduct{Region}Sell` 进入对应地区的据点管理页，然后按 `next` 遍历该地区所有据点。
- 每个据点节点由模板生成，负责识别当前据点、点击据点标签、设置售卖锚点和联络干员切换锚点。
- 若启用联络干员切换，售卖前会通过 `SellProduct{LocationId}BeforeSellOperator` 检查当前干员，不一致时打开联络干员列表、选择目标干员并确认派驻。
- `SellProductSellLoop` 通过 anchor 串起最多 4 次售卖尝试。
- 每次尝试先换货，再用 BetterSliding 把数量调到目标值，最后点击交易。
- 若配置售卖后恢复干员，`SellProductSellLoopEnd` 会通过 `SellProductAfterSellOperator` anchor 进入 `SellProduct{LocationId}AfterSellOperator`，否则命中通用空节点结束该据点流程。

## 任务选项如何改 Pipeline

`assets/tasks/SellProduct.json` 由 `task-template.jsonc` 生成。用户在界面中选择的配置会通过 `pipeline_override` 修改 Pipeline。

### 顶层选项

| 选项                  | 行为                                                           |
| --------------------- | -------------------------------------------------------------- |
| `SellProductSchedule` | 写入 `SellProductSchedule.attach` 的星期布尔值                 |
| `SellBeyondAidQuota`  | 控制超出据点可兑换调度券上限时，是停止任务还是自动确认继续交易 |
| `{RegionPrefix}Sell`  | 控制地区入口节点 `SellProduct{RegionPrefix}` 是否启用          |

### 据点与售卖尝试

每个据点都会生成一组开关：

```text
{RegionPrefix}{LocationId}
{RegionPrefix}{LocationId}Operator
{RegionPrefix}{LocationId}Attempt1
{RegionPrefix}{LocationId}Attempt2
{RegionPrefix}{LocationId}Attempt3
{RegionPrefix}{LocationId}Attempt4
```

默认行为：

- 据点开关默认开启。
- 联络干员切换默认关闭；开启后需要选择售卖用干员，并可选择售卖后恢复干员。
- 第 1、2 次售卖尝试默认开启。
- 第 3、4 次售卖尝试默认关闭。

### 联络干员切换

每个据点都有一个可选的联络干员切换配置：

```text
{RegionPrefix}{LocationId}Operator
{RegionPrefix}{LocationId}TargetOperator
{RegionPrefix}{LocationId}RestoreOperator
```

默认值是关闭。开启后，任务选项会：

- 将 `SellProduct{LocationId}SetBeforeSellOperatorAnchor` 的 `SellProductBeforeSellOperator` anchor 指向 `SellProduct{LocationId}BeforeSellOperator`。
- 根据 `TargetOperator` 写入当前干员识别、列表选择和选中确认节点的多语言 OCR 候选。
- 根据 `RestoreOperator` 决定 `SellProductAfterSellOperator` anchor 是保持通用空节点，还是指向 `SellProduct{LocationId}AfterSellOperator` 并写入恢复目标的多语言 OCR 候选。

售卖前若当前联络干员已是目标干员，会直接进入 `SellProductSellLoop`。若列表中找不到目标干员或恢复干员，对应节点会 `StopTask` 并提示用户确认干员是否已持有或调整配置。

### 优先物品

每次售卖尝试都有一个优先物品选择：

```text
{RegionPrefix}{LocationId}Item{1..4}
```

默认值是 `无`。选择具体物品后，任务选项会：

- 启用 `SellProduct{LocationId}SelectItem{N}`。
- 写入该物品的多语言候选名。
- 将未命中处理设置为 `SellProductPriorityGoodMissWarning`。

如果优先物品未命中，流程会提示“已配置优先货品但当前未识别到任何匹配项”，然后选择默认货品继续售卖，避免整次任务停在选择货品界面。

### 保留份数

每次售卖尝试都有一个保留份数配置：

```text
{RegionPrefix}{LocationId}Reserve{1..4}
{RegionPrefix}{LocationId}ReserveValue{1..4}
```

默认是 `全部售出`。选择 `保留指定份数` 后，会覆盖对应 BetterSliding 节点：

- `next` 改为先尝试 `SellProductSkipToNextSellLoop`，再尝试 `SellProductSellThenLoop`。
- `attach.Target` 写入用户输入的保留数量。
- `attach.TargetReverse` 设为 `true`。

这表示 BetterSliding 会按“当前最大可售数量 - 保留数量”计算目标。若保留数量大于或等于当前库存，则走 `SellProductSkipToNextSellLoop`，跳过本次售卖尝试并进入下一次。

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
- 如果 OCR 有固定噪声，优先把准确候选补进 `data.mjs` 的数据装配逻辑，而不是扩大匹配算法。
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

数量区域在 `data.mjs` 统一维护：

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
4. 若 OCR 名称有固定误识别，再评估是否需要调整 `data.mjs` 候选名装配逻辑。

普通新增物品通常不需要改据点 Pipeline 模板。

### zmdmap 新增据点

1. 运行 `pnpm fetch:zmdmap` 更新缓存。
2. 在 `data.mjs` 检查是否需要补 `SETTLEMENT_OVERRIDE`，确保 `LocationId`、`TextExpected` 稳定。
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
- `SETTLEMENT_OVERRIDE[settlementId].TextExpected`

如果是固定误识别文本，直接把候选补到 `TextExpected`。如果只是 ROI 不合适，需要改 `pipeline-template.jsonc` 和 `pipeline-adb-template.jsonc` 中对应 OCR 节点的 `roi`，然后重新生成。

### 优先物品经常选不到

排查顺序：

1. 确认任务选项是否真的选择了该优先物品。
2. 查看生成出的 `SellProduct{LocationId}SelectItem{N}.custom_recognition_param.candidates`。
3. 检查 zmdmap 多语言名称是否包含游戏 UI 实际显示名。
4. 查看 Go 日志中 `SellProductNormalizedItemMatch` 的 `ocr_texts` 与 `candidates`。
5. 固定噪声优先补候选；只有算法确实无法表达时才改 Go 匹配逻辑。

### 保留份数不符合预期

优先检查：

- 对应 `ReserveValue{N}` 是否覆盖到了正确的 `SellProduct{LocationId}BetterSliding{N}`。
- `attach.Target` 是否为用户输入值。
- `attach.TargetReverse` 是否为 `true`。
- `MaxTarget.Box` 是否能读到最大可售数量。
- `Quantity.Box` 是否能读到当前交易份数。
- Win 与 ADB 资源包是否使用了各自正确的 OCR 区域。

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
4. 任务选项里的地区、据点、尝试、优先物品和保留份数层级是否完整。
5. Win 与 ADB 两套 `Outposts/*.json` 是否都已重新生成。
6. JSON/Markdown 是否符合 `.prettierrc`。

## 常见坑

- **直接手改生成产物**：下次运行 `pnpm generate:SellProduct` 会覆盖改动。应改 `data.mjs`、模板或手写联动文件。
- **只生成 Win 没生成 ADB**：`pipeline-adb-config.json` 负责 ADB 据点节点。涉及数量区域、据点 OCR、售卖尝试模板时要同时确认 ADB 产物。
- **新增物品没有可翻译 label**：`data.mjs` 会从 `zh_cn.json` 反查 `item.*` key。找不到时仍能生成选项，但 label 会退回普通名称；需要补齐多语言。
- **新增地区后任务选项有了，但流程进不去**：任务选项生成不等于入口链路完成。还需要补 `SellProduct.json`、`Sell.json` 和 SceneManager 跳转。
- **扩大优先物品匹配导致串货**：不要用宽松相似度替代当前严格匹配。相近商品名很多，匹配策略必须避免子串误命中。

## 致谢

SellProduct 的据点与可交易物品数据来自 `zmdmap`，由 `pnpm fetch:zmdmap` 下载到 `tools/pipeline-generate/data/settlement_trade.json` 后参与生成。
