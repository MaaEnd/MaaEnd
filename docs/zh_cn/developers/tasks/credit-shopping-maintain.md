# 开发手册 - 信用点商店维护文档

本文说明 `CreditShopping` 的文件分布与执行流程。  
购买条件不是几个独立开关，而是从 `Item/` 串到 `Flow/` 的整条筛选链，维护时需按链路理解。  
该文档更新于 2026 年 11 月 9 日。

## 文件路径

```text
CreditShopping/
├── Flow/                  # 主购物流程
│   ├── ApplyPriorityItems.json # 勾选商品 → 各档 item_ids（Custom：CreditShoppingApplyPriorityItemsAction）
│   ├── Entry.json         # 页签检测 + 进入扫描循环
│   ├── Scan.json          # 货架扫描门控（触发购买决策循环）
│   ├── BuyAction.json     # 三档购买 / 强制购买 / 稳健刷新 / 次数用尽
│   ├── BuyItem.json       # 购买确认 / 保留阈值检查
│   └── Special.json       # 刷新时无法购买 / 刷新商品
├── Credit/                # 信用点管理
│   ├── Reserve.json       # 保留阈值检测 + 当前信用点 OCR
│   ├── AutoGetCredits.json# 自动补信用（买不起时跳基建）
│   └── ClaimCredit.json   # 领取待收取信用（含 IMS A3 累加）
├── Item/                  # 货架商品识别（识别链）
│   ├── Priority1Items.json# 购买物品选项1：IconRecognition item_ids
│   ├── Priority2Items.json# 购买物品选项2：IconRecognition item_ids
│   ├── Priority3Items.json# 购买物品选项3：IconRecognition item_ids
│   ├── AnyItems.json      # 任意可买物品：全 catalog IconRecognition
│   └── ShelfChecks.json   # 买得起 / 折扣（相对物品框偏移）
├── GoToShop.json           # 导航到商店并切到信用交易所页签
└── Reflash.json            # 刷新按钮 / 花费 / 次数用尽 / 无法刷新
```

其他关联文件：

| 路径 | 作用 |
| --------------------------------------------------------- | -------------------------------------- |
| `assets/interface.json` | 任务挂载（`other_menu` 组；另被 `DailyFull` / `QuickDaily` 预设引用） |
| `assets/tasks/CreditShopping.json` | 任务入口、三档购买、保留阈值、刷新选项 |
| `agent/go-service/creditshopping/` | 把勾选商品复制成各档 `IconRecognition` 的 `item_ids`（唯一 CustomAction） |
| `assets/resource/pipeline/DijiangRewards/Entry.json`（`NeedCredit`）、`ReceptionRoom.json` | 信用不足时回基建补信用 |
| `assets/locales/interface/*.json` | 任务、选项与 focus 文案 |

## 执行流程

1. 进入信用交易所页签；若不在商店则先导航（`GoToShop.json`），再[领取待收取信用](#领取信用)（`Credit/ClaimCredit.json`）。
2. 进入扫描循环前，[一次性把各档勾选商品写成 `item_ids`](#物品勾选与初始化)（`Flow/ApplyPriorityItems.json` + Go）。
3. 每轮扫描当前货架，按固定优先级依次判断（`Flow/Scan.json` → `Flow/BuyAction.json`）：
    - 某档开启了「自动获取信用点」，且该档目标商品[买不起（信用不够）](#自动补信用) → 跳基建补信用后回来；未开启的档位不做此判断。
    - 是否命中[购买物品选项 1 / 2 / 3](#三档购买优先级) → 直接确认购买。
    - 当前信用是否[低于保留阈值](#保留信用点阈值) → 结束任务。
    - 刷新次数是否已用尽、是否触发[稳健刷新改直购](#强制策略与刷新)。
    - 按[强制策略](#强制策略与刷新)购买任意可买品 / 刷新货架 / 直接结束。
4. 点击确认后进入奖励界面：IMS A3 累加培养道具并播报，再关闭回到列表继续扫描。强制购买会在确认前按「持有 − 价格」检查保留阈值。

> 扫描循环的 `next` 顺序即业务优先级；改行为时要看整条链，不要只改单个识别器。

## 特殊处理

### 领取信用

实现位于 `Credit/ClaimCredit.json`。进入信用交易所后先尝试领取待收取信用；没有可领项则直接进入扫描，不阻塞主流程。

### 物品勾选与初始化

1. 用户在三档 checkbox 里勾选商品，每个 case 的 `pipeline_override` 往 `CreditShoppingApplyPriorityItems` 节点写一条 `attach`（`priority1__<CaseID>: <CaseID>`）。
2. `Flow/ApplyPriorityItems.json` 的 CustomAction 读取该节点的 `attach`，把 CaseID 换成 IconRecognition catalog 里的 `item_ids`，再用 `OverridePipeline` 覆盖 `CreditShoppingPriority1/2/3Item`。Go 只做这一次复制，不参与任何购买判断。
3. 三档都设了 `min_count: 1`，界面上每档至少要勾 1 件。某档没有勾选时不会覆盖该档节点，节点保留 Pipeline 默认 `item_ids`（`Priority1` 默认 `item_gold`）。
4. `item_ids` 的数组顺序取决于 `attach` 的遍历顺序，不影响识别（节点用 `order_by: natural`，按货架位置排序）。

### 商品识别链

实现位于 `Item/` 与 `Item/ShelfChecks.json`，识别分两层：

1. **物品**：`IconRecognition`（`grid_type: credit_trade`）在 `roi` 内识别整排卡片，返回每格的 `cell_box` / `row` / `column` / `item_id`。`CreditShoppingPriority{N}Item` 只认该档 `item_ids`，`CreditShoppingShelfAnyItem` 认全 catalog，两者都取 `index: 0`（货架自然顺序的第一个命中）。
2. **判定**：`ShelfChecks.json` 以物品框为基准偏移，依次给出
    - 买得起 `*CanAfford` / 买不起 `*CanNotAfford`（价格文字颜色）
    - 折扣 `*Discount`（OCR 角标）

售罄卡（卡片变暗 + 「售罄」）不会被 IconRecognition 命中，所以 Pipeline 不再单独判断未售罄。

`CreditShoppingBuyPriority{N}` 是 And 链：物品 → 买得起 → 折扣 → 保留阈值门控，`box_index: 0` 保证点击落在物品格中心。**前层未命中则整条链失效**，排查时按 物品 → 买得起 → 折扣 顺序看。

控制器差异分开维护：ADB 的 IconRecognition ROI 在 `assets/resource_adb/pipeline/CreditShopping/IconRecognitionRoi.json`，云端颜色阈值在 `assets/resource_cloud/pipeline/CreditShopping/Item.json`。Pipeline 与 Go 都不写死 ROI，改区域时只改对应资源覆盖。

### 三档购买优先级

三档结构相同，默认策略不同（`CreditShopping.json`）：

| 档位 | 默认「无条件购买」 | 默认「自动获取信用点」 | 典型用途 |
| ---------- | ------------------ | ------------------ | ------------------------ |
| 购买物品选项1 | 开 | 开 | 信用快见底也值得买的刚需 |
| 购买物品选项2 | 关 | 关 | 需满足保留阈值才买 |
| 购买物品选项3 | 关 | 关 | 同上，更低优先级 |

每档可独立配置：勾选商品（每档至少 1 件）、最低折扣、是否跳过保留阈值、买不起时是否允许补信用。  
三档都未命中后，才由统一的保留阈值节点负责兜底退出。

### 保留信用点阈值

`CreditShoppingReserve` 改写保留阈值表达式（`Credit/Reserve.json`）。  
各档是否受阈值限制，由该档的「无条件购买」开关控制，而非扫描 `next` 顺序。  
想某档无视阈值时，改对应档位的「无条件购买」，不要把阈值判断插回购买链中间。

### 自动补信用

某档开启了「自动获取信用点」，且该档「买得起」链命中时，经 `GoToNeedCredit`（`Credit/AutoGetCredits.json`）跳转到 `NeedCredit` 节点（`DijiangRewards/Entry.json`）：

1. 回基建会客室，按配置赠送线索或开启线索交流。
2. 逐条赠送次数由 `CreditShoppingClueSend` 控制（`0` = 不逐条赠送；开启「快速赠予重复线索」时仍会一键赠送）。
3. 可赠送的线索库存下限由 `CreditShoppingClueStockLimit` 控制（默认保留 2 个，即库存 ≥ 3 才送）。
4. 「快速赠予重复线索」由 `CreditShoppingQuickGiveDuplicateClues` 控制（默认关；开启后进入赠予界面优先一键赠送）。

三档各自独立开关；刷新费不足**不会**触发补信用（旧版 `RefreshGetCredits` 已移除）。

### 折扣选项

每档的折扣选项改写对应档位的折扣 OCR `expected`，或切到「任意」档。
数值档只改写 `expected`（如「-25%及以上」）；「任意」档把折扣节点整体换成 `DirectHit`，**不再检查折扣角标**——未打折的商品也会被购买，与 `AutoStockStaple` 的「任意」语义一致。
折扣规则须同时覆盖买得起与买不起两侧。

### 强制策略与刷新

三档均未命中后的兜底，由 `CreditShoppingForce` 决定（`Flow/BuyAction.json` + `Flow/Special.json`）：

| 策略 | 行为 |
| ---------- | -------------------------------- |
| 退出 | 不买任意品、不刷新，直接结束 |
| 购买所有物品 | 购买任意买得起的商品（售罄卡不会被识别）；确认前用 `BuyItemBelowReserve` 按「持有 − 价格」检查保留阈值，不足则取消并停止 |
| 执行刷新 | 尝试刷新货架；可展开「稳健模式刷新」「直购前检查价格」 |

**稳健刷新**：若「当前信用 − 刷新花费 < 稳健刷新阈值」且架上仍有可买品，则不刷新、改为直接购买。
该阈值与「保留信用点」是两套独立条件，不要混用。

**直购前检查价格**（刷新子选项，默认开）：开启后，刷新用尽直购 / 稳健改直购在确认前按「持有 − 价格」检查保留阈值，不足则取消并停止；关闭则先买，买完后再由「保留信用点」节点决定是否结束。

## 新增商品时需改的路径

1. `assets/tasks/CreditShopping.json` — 在对应档位 checkbox 增加 case，`pipeline_override` 写一条 `attach`（`priority<N>__<CaseID>`）
2. `assets/locales/interface/*.json` — `option.CreditShoppingItems.cases.*.label`

勾选后由 Go 复制成 `item_ids` 即可购买；奖励播报与数量累加由 IMS A3（`ClaimCredit.json`）负责，不必再为弹窗单独加商品 OCR。
新增 case 后记得检查 `default_case` 是否也要纳入新商品。

## 维护要点

| 现象 | 优先查 |
| ---------------- | --------------------------------------------------------------- |
| 识别不到目标商品 | 该档 `item_ids`（勾选 → Go 覆盖后的节点）与 `IconRecognition` ROI |
| 买得起却不买 | 该档保留阈值 / 无条件购买开关 |
| 买不起不补信用 | 该档 AutoGetCredits 开关；买不起侧折扣与价格颜色阈值 |
| 刷新行为异常 | `CreditShoppingForce`；稳健刷新阈值 |
| 选项间行为不一致 | `CreditShopping.json` 的 `pipeline_override` 与扫描 `next` 顺序 |

维护时分四层定位：入口（`GoToShop.json`）→ 扫描决策（`Flow/`）→ 识别链（`Item/` + `ShelfChecks.json`）→ 参数装配（任务选项 + Go）。
