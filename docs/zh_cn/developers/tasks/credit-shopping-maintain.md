# 开发手册 - 信用点商店维护文档

本文用于说明 `CreditShopping`（信用点商店）的整体结构、购买优先级、获取信用点联动、刷新策略，以及 `assets/tasks/CreditShopping.json` 中各个 `interface` 选项如何覆盖 Pipeline 行为，便于后续维护与扩展。  
该文档更新于 2026 年 4 月 6 日  
[perf:每个购买选项都会接受信用点阈值限制(#1980)](https://github.com/MaaEnd/MaaEnd/pull/1980) 提交之后

## 文件概览

当前实现主要分布在以下文件中：

| 模块           | 路径                                                        | 作用                                                           |
| -------------- | ----------------------------------------------------------- | -------------------------------------------------------------- |
| 项目接口挂载   | `assets/interface.json`                                     | 将 `tasks/CreditShopping.json` 挂到 `daily` 任务组             |
| 任务与选项定义 | `assets/tasks/CreditShopping.json`                          | 定义任务入口、界面选项、子选项和 `pipeline_override`           |
| 任务入口       | `assets/resource/pipeline/CreditShopping/GoToShop.json`     | 负责进入商店并切到信用交易所页签                               |
| 领取信用       | `assets/resource/pipeline/CreditShopping/ClaimCredit.json`  | 负责领取待收取信用并关闭奖励弹窗                               |
| 商品扫描主循环 | `assets/resource/pipeline/CreditShopping/Shopping.json`     | 负责初始化参数、扫描商品、按优先级购买、刷新或结束             |
| 商品列表识别   | `assets/resource/pipeline/CreditShopping/Item.json`         | 负责识别商品图标、是否售罄、是否买得起、商品名与折扣           |
| 购买弹窗流程   | `assets/resource/pipeline/CreditShopping/BuyItem.json`      | 负责购买确认、购买失败处理与回到商品列表                       |
| 购买结果聚焦   | `assets/resource/pipeline/CreditShopping/BuyItemFocus.json` | 在购买弹窗中识别具体买到的商品并记录 focus                     |
| 刷新相关识别   | `assets/resource/pipeline/CreditShopping/Reflash.json`      | 负责识别刷新按钮、刷新花费与“无法刷新”状态                     |
| 获取信用点联动 | `assets/resource/pipeline/DijiangRewards/NeedCredit.json`   | 在信用不足时回基建开启线索交流或赠予线索获取信用               |
| Go 参数解析    | `agent/go-service/creditshopping/creditshopping.go`         | 将任务选项写入的 `attach` 关键词合并成 OCR 正则并覆盖 Pipeline |
| 多语言文案     | `assets/locales/interface/*.json`                           | `CreditShopping` 任务与选项文案                                |

## 总体执行逻辑

任务入口是 `GoToShop.json` 中的 `CreditShoppingMain`：

1. 先尝试命中 `CreditShoppingShopping`，若已在信用交易所页则直接进入扫描循环。
2. 若当前仅处于商店页面，则通过 `CreditShoppingCheckShopPage` 点击信用交易所页签。
3. 进入页签后先经过 `ClaimCredit.json`：
    1. 有待领取信用时点击 `CreditShoppingClaimCredit`
    2. 没有待领取信用时命中 `CreditShoppingNoCreditClaim`
4. 回到 `Shopping.json` 的 `CreditShoppingShopping` 后，先执行一次 `CreditShoppingInit`。
5. `CreditShoppingInit` 通过自定义动作 `CreditShoppingParseParams` 读取节点 `attach`，生成当前运行时的白名单 OCR 正则。
6. 后续循环进入 `CreditShoppingScanItem`，按固定顺序判断：
    1. 是否需要先去补信用点
    2. 是否命中优先购买 1
    3. 是否命中优先购买 2
    4. 是否命中优先购买 3
    5. 是否触发保留信用点阈值
    6. 是否因刷新策略进入“已用尽刷新次数后直接买”或“稳健刷新改直购”
    7. 是否按强制策略购买任意物品或执行刷新
    8. 若以上都不命中，则结束任务

这里最关键的设计点是：

- `CreditShoppingInit` 只执行一次，把复杂的“多选物品列表 -> OCR 正则”转换放到 Go 中做。
- `CreditShoppingScanItem` 的 `next` 顺序本身就是业务优先级，维护时不要只看单个节点，要把整个扫描顺序一起看。
- 三档购买都可以分别配置“无条件购买”和“自动获取信用点”，当前默认值分别是：购买物品选项 1 为开启/开启，购买物品选项 2 为关闭/关闭，购买物品选项 3 为关闭/关闭。

## 购买优先级模型

当前任务把商品分成 3 档：

### 购买物品选项1

- 入口节点：`CreditShoppingBuyPriority1`
- 识别条件：商品存在、未售罄、买得起、名称命中 `BuyFirstOCR`、折扣命中 `IsDiscountPriority1`
- 默认附带：
    - `CreditShoppingPriority1UnconditionalPurchase=Yes`，即“无条件购买”，跳过保留信用点阈值检查
    - `CreditShoppingPriority1AutoGetCredits=Yes`，即买不起时允许触发自动获取信用点

这档通常用于“即使信用点快见底也值得买”的商品。

### 购买物品选项2

- 入口节点：`CreditShoppingBuyPriority2`
- 识别条件：商品存在、未售罄、买得起、名称命中 `Priority2OCR`、折扣命中 `IsDiscountPriority2`
- 默认附带：
    - `CreditShoppingPriority2UnconditionalPurchase=No`，即需要满足保留信用点阈值
    - `CreditShoppingPriority2AutoGetCredits=No`，即买不起时默认不触发自动获取信用点

### 购买物品选项3

- 入口节点：`CreditShoppingBuyPriority3`
- 识别条件：商品存在、未售罄、买得起、名称命中 `Priority3OCR`、折扣命中 `IsDiscountPriority3`
- 默认附带：
    - `CreditShoppingPriority3UnconditionalPurchase=No`，即需要满足保留信用点阈值
    - `CreditShoppingPriority3AutoGetCredits=No`，即买不起时默认不触发自动获取信用点

### 为什么保留阈值改成放在三档购买之后

`CreditShoppingScanItem.next` 的顺序是：

1. `AutoGetCredits`
2. `CreditShoppingBuyPriority1`
3. `CreditShoppingBuyPriority2`
4. `CreditShoppingBuyPriority3`
5. `CreditShoppingReserveCredit`

这意味着：

- 三档购买都会先尝试自己的购买识别。
- 是否受保留信用点阈值限制，不再由 `next` 顺序决定，而是由各自的 `CreditShoppingPriority{N}ReserveCreditGate` 控制。
- 如果三档购买都不命中，最后才由统一的 `CreditShoppingReserveCredit` 负责“低于阈值则结束任务”的兜底退出。

这里的命名约定是：

- `CreditShoppingPriority{N}ReserveCreditGate`：某一档购买在执行前是否需要通过保留信用点阈值检查的准入节点。
- `CreditShoppingReserveCredit`：当三档购买都未命中后，统一负责“当前信用点已经低于保留阈值，应结束任务”的兜底退出节点。

维护时如果想改变“哪些商品要无视保留阈值”，应该优先调整各档位的“无条件购买”开关，而不是再把 `CreditShoppingReserveCredit` 插回购买节点中间。

## 运行时参数覆盖

`CreditShopping` 的多选白名单不是直接把长正则写死在任务文件里，而是分两步完成：

1. `assets/tasks/CreditShopping.json` 的 checkbox case 把各商品的多语言名称写入不同 OCR 节点的 `attach`
2. `agent/go-service/creditshopping/creditshopping.go` 在 `CreditShoppingInit` 时读取这些 `attach`，合并为运行时正则，再调用 `OverridePipeline`

当前会被动态覆盖的节点有：

- `BuyFirstOCR`
- `BuyFirstOCR_CanNotAfford`
- `Priority2OCR`
- `Priority2OCR_CanNotAfford`
- `Priority3OCR`
- `Priority3OCR_CanNotAfford`

实际规则如下：

- `CreditShoppingPriority1Items` 的关键词会同时写到 `BuyFirstOCR` 和 `BuyFirstOCR_CanNotAfford`
- Go 会把两边 `attach` 合并去重，生成同一份白名单正则
- `CreditShoppingPriority2Items` 和 `CreditShoppingPriority3Items` 则分别只改各自档位
- 如果某一档没有任何勾选项，Go 会把对应 `expected` 改成 `a^`，等价于“永不匹配”

这套设计的好处是：

- 任务层仍然保持“每个商品一个 case”的可维护性
- Pipeline 运行时只做简单 OCR，不需要维护超长硬编码正则
- Go 可以统一处理去重、转义、空列表回退

### Go 的工作逻辑

`CreditShopping` 里 Go 的职责很单纯：它不负责购买流程本身，只负责把任务选项整理成 Pipeline 能直接使用的运行时参数。

执行顺序可以理解为：

1. `CreditShoppingInit` 在进入商店扫描循环前触发一次 `CreditShoppingParseParams`
2. Go 读取 `BuyFirstOCR`、`Priority2OCR`、`Priority3OCR` 等节点上的 `attach`
3. 把任务选项里勾选的多语言商品名整理成各档位对应的白名单匹配条件
4. 通过 `OverridePipeline` 回写到这些 OCR 节点的 `expected`
5. 后续商品扫描仍由 Pipeline 完成，Go 不再参与逐个商品判断

也就是说，这里的分工是：

- `assets/tasks/CreditShopping.json` 负责定义“用户选了哪些商品”
- Go 负责把这些选择转换成 OCR 可用的匹配条件
- `assets/resource/pipeline/CreditShopping/*.json` 负责真正的识别、点击、购买和刷新流程

维护时只要记住一点：如果你改的是“白名单商品怎么组织”，主要看任务选项和 Go；如果你改的是“什么时候买、怎么买、买完去哪”，主要看 Pipeline。

## 获取信用点联动

`CreditShopping` 支持在“某档购买想买但买不起”时，临时跳去基建补信用。

### `AutoGetCredits`

自动获取信用点不再是顶层总开关，而是挂在三个购买物品选项下面：

- `CreditShoppingPriority1AutoGetCredits`
- `CreditShoppingPriority2AutoGetCredits`
- `CreditShoppingPriority3AutoGetCredits`

它们分别控制：

- 该购买物品选项里的目标商品买不起时，是否允许触发自动补信用
- 关闭时，通过把对应 `AutoGetCreditsBuyPriority{N}` 的识别改成 `a^` 来禁用触发

汇总入口仍然是 `Shopping.json` 的 `AutoGetCredits`：

- 节点：`AutoGetCredits`
- 触发来源：`AutoGetCreditsBuyPriority1`、`AutoGetCreditsBuyPriority2`、`AutoGetCreditsBuyPriority3`
- 触发方式：三者做 `Or`，任意一档命中即跳到 `GoToNeedCredit`

这意味着自动补信用的触发，不再只属于购买物品选项 1，而是由各档位独立控制。

### 赠送线索设置

顶层不再有“获取信用点设置”分组，当前只保留两项与送线索相关的设置：

- `CreditShoppingClueSend`
- `CreditShoppingClueStockLimit`

其中：

- `CreditShoppingClueSend` 会同时覆盖：
    - `ReceptionRoomSendCluesEntry_NeedCredit.max_hit`
    - `ReceptionRoomSendCluesSelectClues_NeedCredit.max_hit`
- `CreditShoppingClueStockLimit` 会通过覆盖 `ClueItemCount_NeedCredit.expected`，决定“库存达到多少才算可赠送”

`CreditShoppingClueSend` 当前支持输入 `0`：

- `0`：表示不赠送线索
- `1+`：表示一次联动流程中最多赠送对应次数

默认阈值 `2` 的实际含义是“单种线索库存至少有 3 个才送”，也就是默认保留 2 个。

## 折扣选项的真实含义

三档优先级都有一个折扣筛选项：

- `CreditShoppingPriority1DiscountValue`
- `CreditShoppingPriority2DiscountValue`
- `CreditShoppingPriority3DiscountValue`

这些选项覆盖的不是“点哪个折扣按钮”，而是直接修改 `IsDiscountPriority{N}` 与对应 `CanNotAfford` 节点的 `expected`。

例如：

- `Any`：改成 `ColorMatch`，只要求折扣区域存在
- `-75%`：只接受 `75|95|99`
- `-99%`：只接受 `99`

维护时要注意两点：

1. `Any` 与其他 case 的实现方式不同，它不是宽松 OCR，而是改成必中的颜色匹配。因为如果直接改为直接命中,会丢失target roi
2. `AutoGetCredits` 依赖 `*_CanNotAfford` 这一套折扣节点，所以折扣规则必须同时覆盖“买得起”和“买不起”两边。

## 强制策略与刷新策略

当前“没有合适的白名单商品可买”后，系统由 `CreditShoppingForce` 决定后续行为。

### `CreditShoppingForce=Exit`

- 关闭 `CreditShoppingBuyBlacklist`
- 关闭 `RefreshItem`
- 关闭 `CreditShippingCanNotToBuy`

语义是：没有合适商品就直接结束，不购买任意物品，也不刷新。

### `CreditShoppingForce=IgnoreBlackList`

- 打开 `CreditShoppingBuyBlacklist`
- 关闭刷新相关节点

语义是：三个购买物品选项都未命中后，只要还有买得起、未售罄的商品，就继续购买任意物品。

### `CreditShoppingForce=Refresh`

- 关闭 `CreditShoppingBuyBlacklist`
- 打开 `RefreshItem`
- 打开 `CreditShippingCanNotToBuy`
- 继续展开 `PrudentRefresh`

语义是：没有合适商品时，优先考虑刷新商店。

### `PrudentRefresh`

稳健刷新模式不是“更谨慎地刷新”，而是：

- 当表达式 `{当前信用}-{刷新花费}<{阈值}` 成立
- 且列表中还有任意买得起、未售罄商品

就不刷新，改为直接购买当前可买商品。

默认阈值通过 `PrudentRefreshThreshold` 写进：

```text
{CreditShoppingReserveCreditOCRInternal}-{RefreshCost}<{PrudentRefreshThreshold}
```

因此它拦的是“刷新后剩余信用太少”的场景，而不是“当前信用低于某值”的场景。

## 添加或修改商品

当前商品维护分成两层：

1. 列表页上的白名单匹配
2. 购买弹窗中的商品确认与 focus 记录

如果新增一个商品，至少要同步以下内容。

### 1. 任务选项中的商品 case

文件：`assets/tasks/CreditShopping.json`

需要在以下一个或多个 checkbox 里添加 case：

- `CreditShoppingPriority1Items`
- `CreditShoppingPriority2Items`
- `CreditShoppingPriority3Items`

每个 case 至少要补：

- `name`
- `label`
- 对应 OCR 节点的 `attach`

注意：

- `Priority1` 要同时维护 `BuyFirstOCR` 和 `BuyFirstOCR_CanNotAfford`
- `Priority2/3` 只需要维护各自档位的 OCR 节点
- `attach` value 建议写该商品所有已支持语言的稳定 OCR 文案

### 2. 购买弹窗入口列表

文件：`assets/resource/pipeline/CreditShopping/BuyItem.json`

在 `CreditShoppingBuyItem.next` 中加入对应的 `CreditShoppingBuyItemOCR_{ItemName}` 节点，否则就算商品被点开了，也无法在购买弹窗中命中具体商品分支。

### 3. 购买弹窗 OCR 节点

文件：`assets/resource/pipeline/CreditShopping/BuyItemFocus.json`

新增对应的 `CreditShoppingBuyItemOCR_{ItemName}` 节点，维护：

- 商品名 OCR `expected`
- `focus.Node.Recognition.Succeeded`

如果只改了任务白名单，没补这一步，运行时依旧可能能点击商品，但买入后无法正确记录具体买到什么。

### 4. 国际化文案

文件：`assets/locales/interface/*.json`

需要补：

- `option.CreditShoppingItems.cases.{ItemName}.label`

若新增了新的顶层或子选项，还要同步补齐对应 option 文案。

## 修改默认白名单或折扣时的注意点

最容易漏掉的是“默认值”和“可选 case”不是一回事。

例如：

- `CreditShoppingPriority1Items.default_case`
- `CreditShoppingPriority2Items.default_case`
- `CreditShoppingPriority3Items.default_case`

只新增 case 不改 `default_case`，默认用户配置不会自动包含新商品。

同理，若调整默认折扣策略，也要同步看：

- `CreditShoppingPriority1DiscountValue.default_case`
- `CreditShoppingPriority2DiscountValue.default_case`
- `CreditShoppingPriority3DiscountValue.default_case`

## 维护时最容易漏掉的同步点

### 1. 只改白名单，不改购买确认

这会导致：

- 商品列表页能点到
- 购买弹窗里却没有对应 `CreditShoppingBuyItemOCR_{ItemName}` 节点

最终表现通常是买入后 focus 缺失，或购买确认链路行为不透明。

### 2. 只改 `BuyFirstOCR`，忘了 `BuyFirstOCR_CanNotAfford`

`AutoGetCredits` 依赖的是“买不起”分支：

- `AutoGetCreditsBuyPriority1`
- `BuyFirstOCR_CanNotAfford`
- `IsDiscountPriority1_CanNotAfford`

如果只维护可购买分支，信用不足时不会正确触发补信用逻辑。

### 3. 误以为自动补信用只属于购买物品选项1

当前不是。

自动跳去基建补信用接在 `AutoGetCreditsBuyPriority1/2/3` 三个分支上，是否允许触发由各自购买物品选项下的 `AutoGetCredits` 开关决定。

### 4. 误以为刷新费不足也会自动补信用

当前不会。

`RefreshGetCredits` 相关任务选项已经删除，刷新额度不足不会再触发独立的补信用流程。当前自动补信用只会由“某档购买项买不起”触发。

### 5. 误以为 `PrudentRefresh` 受保留信用阈值控制

不是。

`CreditShoppingReserve` 和 `PrudentRefreshThreshold` 是两套独立条件：

- 前者控制各购买档位自己的阈值判断，以及最终是否兜底退出
- 后者控制刷新前是否改为直接购买

两者不要混用。

### 6. 忘了检查 `next` 顺序

`CreditShopping` 很多行为不是靠单个开关决定，而是靠 `CreditShoppingScanItem.next` 的先后顺序决定。

一旦你改了：

- 保留阈值触发点
- 自动补信用触发点
- 强制购买或刷新策略

都要回头确认 `CreditShoppingScanItem.next` 仍然符合预期优先级。

## 推荐的理解方式

维护 `CreditShopping` 时，建议把它分成四层：

1. **入口层**：`GoToShop.json` + `ClaimCredit.json`，负责“进入信用交易所并收取今日信用”。
2. **扫描决策层**：`Shopping.json`，负责“按什么顺序决定买、停、补信用、刷新”。
3. **识别层**：`Item.json` + `Reflash.json`，负责“商品名、折扣、可购买状态、刷新状态怎么识别”。
4. **参数装配层**：`assets/tasks/CreditShopping.json` + `agent/go-service/creditshopping/creditshopping.go`，负责“用户选了哪些商品，最终变成什么 OCR 条件”。

这样定位问题会比较快：

- 进不去信用商店，看入口层。
- 进了商店但决策不对，看扫描决策层。
- 识别错商品、折扣或价格状态，看识别层。
- 同一套 Pipeline 在不同选项下行为不一致，看参数装配层。

## 自检清单

修改后至少检查以下几项：

1. `assets/interface.json` 是否仍然导入了 `tasks/CreditShopping.json`。
2. `CreditShoppingInit` 是否还能正常执行 `CreditShoppingParseParams`。
3. 新增商品时，`assets/tasks/CreditShopping.json`、`BuyItem.json`、`BuyItemFocus.json`、`assets/locales/interface/*.json` 是否同步修改。
4. 若改了获取信用点逻辑，`NeedCredit.json` 中的 `ReceptionRoomSendCluesEntry_NeedCredit`、`ReceptionRoomSendCluesSelectClues_NeedCredit`、`ClueItemCount_NeedCredit` 是否与任务选项语义一致。
5. 若改了刷新策略，`RefreshItem`、`CanNotFlash`、`CreditShoppingPrudentRefresh` 的先后关系是否仍正确。
6. 若改了优先级语义，`CreditShoppingPriority1/2/3ReserveCreditGate` 与 `CreditShoppingReserveCredit` 的职责划分是否仍清晰。
7. 若改了自动补信用逻辑，`AutoGetCreditsBuyPriority1/2/3` 是否与三个购买物品选项中的 `AutoGetCredits` 开关保持一致。
