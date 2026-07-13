# 开发手册 - 信用点商店维护文档

本文说明 `CreditShopping` 的文件分布与执行流程。  
购买条件不是几个独立开关，而是从 `Item/` 串到 `Flow/` 的整条筛选链，维护时需按链路理解。  
该文档更新于 2026 年 7 月 13 日。

## 文件路径

```
CreditShopping/
├── Flow/                  # 主购物流程
│   ├── Entry.json         # 入口检测 + 白名单初始化
│   ├── Scan.json          # 货架扫描门控 + 快照（触发购买决策循环）
│   ├── BuyAction.json     # 三档购买 / 黑名单 / 稳健刷新 / 无物可买
│   ├── BuyItem.json       # 购买弹窗确认 / 失败 / 关闭
│   └── Special.json       # ADB 滑动 / 刷新时无法购买 / 刷新商品
├── Credit/                # 信用点管理
│   ├── Reserve.json       # 保留阈值检测 + 当前信用点 OCR
│   ├── AutoGetCredits.json# 自动补信用（买不起时跳基建）
│   └── ClaimCredit.json   # 领取待收取信用
├── Item/                  # 货架商品识别（识别链）
│   ├── ShelfBase.json     # 锚点 CreditIcon + 售罄 + 价格判断
│   ├── Priority1.json     # 优先购买 1 白名单 + 折扣
│   ├── Priority2.json     # 优先购买 2 白名单 + 折扣
│   ├── Priority3.json     # 优先购买 3 白名单 + 折扣
│   └── Blacklist.json     # 黑名单过滤 + 折扣
├── BuyItem/               # 购买弹窗（每商品一文件）
│   ├── ArsenalTicket.json # 各商品弹窗 OCR
│   ├── Oroberyl.json
│   ├── TCreds.json
│   ├── ElementaryCombatRecord.json
│   ├── IntermediateCombatRecord.json
│   ├── ElementaryCognitiveCarrier.json
│   ├── ArmsInspector.json
│   ├── ArmsINSPKit.json
│   ├── CastDie.json
│   ├── HeavyCastDie.json
│   ├── Protodisk.json
│   ├── Protoset.json
│   ├── Protoprism.json
│   └── Protohedron.json
├── GoToShop.json           # 导航到商店并切到信用交易所页签
├── record.json             # 货架快照时记录商品名与折扣
└── Reflash.json            # 刷新按钮 / 花费 / 次数用尽 / 无法刷新
```

其他关联文件：

| 路径                                              | 作用                                   |
| ------------------------------------------------- | -------------------------------------- |
| `assets/interface.json`                           | 任务挂载（`other_menu` / `daily` 组）  |
| `assets/tasks/CreditShopping.json`                | 任务入口、三档购买、保留阈值、刷新选项 |
| `assets/resource/pipeline/DijiangRewards/NeedCredit.json` | 信用不足时回基建补信用           |
| `agent/go-service/common/attachregex/action.go`   | attach 关键词合并为 OCR 白名单正则     |
| `assets/locales/interface/*.json`                 | 任务、选项与 focus 文案                |

## 执行流程

1. 进入信用交易所页签；若不在商店则先导航（`GoToShop.json`），再[领取待收取信用](#领取信用)（`Credit/ClaimCredit.json`）。
2. 进入扫描循环前，[一次性初始化各档商品名白名单](#attach-与白名单初始化)（`Flow/Entry.json` + `attachregex/action.go`）。
3. 每轮对当前货架快照，按固定优先级依次判断（`Flow/Scan.json` → `Flow/BuyAction.json`）：
    - 某档目标商品[买得起但信用不够](#自动补信用) → 跳基建补信用后回来。
    - 是否命中[优先购买 1 / 2 / 3](#三档购买优先级) → 进入购买弹窗。
    - 当前信用是否[低于保留阈值](#保留信用点阈值) → 结束任务。
    - 刷新次数是否已用尽、是否触发[稳健刷新改直购](#强制策略与刷新)。
    - 按[强制策略](#强制策略与刷新)购买任意可买品 / 刷新货架 / 直接结束。
4. 购买弹窗内确认商品、记录 focus（`Flow/BuyItem.json` + `BuyItem/*.json`），回到列表继续扫描。

> 扫描循环的 `next` 顺序即业务优先级；改行为时要看整条链，不要只改单个识别器。

## 特殊处理

### 领取信用

实现位于 `Credit/ClaimCredit.json`。进入信用交易所后先尝试领取待收取信用；没有可领项则直接进入扫描，不阻塞主流程。

### attach 与白名单初始化

1. 用户在 `CreditShopping.json` 勾选商品 → 各语言名称写入对应档位的 `attach`。
2. 扫描前串行执行 `AttachToExpectedRegexAction`，把 attach 合并为 `^(别名1|别名2|...)$` 覆盖到商品名 OCR 节点。
3. 每档同时维护「买得起」与「买不起」两侧白名单；某档未勾选任何商品时，正则改为 `a^`（永不匹配）。

Go 只负责参数装配；何时买、怎么买由 Pipeline 决定。

### 商品识别链

实现位于 `Item/` 目录。读图顺序：**先锚点，再一路偏移**；前后层依赖，前层未命中则后续全部失效。

颜色约定（维护文档与截图对照用）：

- 黑：信用点商品卡片锚点（`Item/ShelfBase.json` — `CreditIcon`）
- 蓝：是否未售罄（`Item/ShelfBase.json` — `NotSoldOut`）
- 红：买得起 / 买不起（`Item/ShelfBase.json` — `CanAfford` / `CanNotAfford`，两条链在此分叉）
- 绿：商品名 OCR（白名单，各档 `Item/Priority{N}.json` / `Item/Blacklist.json`）
- 粉：折扣 OCR（各档 `Item/Priority{N}.json` / `Item/Blacklist.json`）

#### 购买链

![购买识别链](https://github.com/user-attachments/assets/0e9f7e50-9b08-451f-abd4-2cb49b01986f)

```text
ShelfBase → 白名单 → 折扣 → 进入购买判断
```

#### 补信用链

![补信用识别链](https://github.com/user-attachments/assets/37235adf-9f1c-40ed-aaaa-9f713a80d5a7)

```text
ShelfBase → 白名单 → 折扣 → 进入补信用判断
```

两链的商品名与折扣语义必须保持一致，否则会出现「买得起时是目标、买不起时不是」的矛盾。  
每档需同时维护两侧节点；排查时按黑 → 蓝 → 红 → 绿 → 粉顺序逐层看。

### 三档购买优先级

三档结构相同，默认策略不同（`CreditShopping.json`）：

| 档位       | 默认「无条件购买」 | 默认「自动补信用」 | 典型用途                 |
| ---------- | ------------------ | ------------------ | ------------------------ |
| 优先购买 1 | 开                 | 开                 | 信用快见底也值得买的刚需 |
| 优先购买 2 | 关                 | 关                 | 需满足保留阈值才买       |
| 优先购买 3 | 关                 | 关                 | 同上，更低优先级         |

每档可独立配置：勾选商品、最低折扣、是否跳过保留阈值、买不起时是否允许补信用。  
三档都未命中后，才由统一的保留阈值节点负责兜底退出。

### 保留信用点阈值

`CreditShoppingReserve` 改写保留阈值表达式（`Credit/Reserve.json`）。  
各档是否受阈值限制，由该档的「无条件购买」开关控制，而非扫描 `next` 顺序。  
想某档无视阈值时，改对应档位的「无条件购买」，不要把阈值判断插回购买链中间。

### 自动补信用

某档开启了「自动补信用」，且[补信用识别链](#商品识别链)命中时，跳转到 `NeedCredit.json`（`Credit/AutoGetCredits.json`）：

1. 回基建会客室，按配置赠送线索或开启线索交流。
2. 赠送次数由 `CreditShoppingClueSend` 控制（`0` = 不送）。
3. 可赠送的线索库存下限由 `CreditShoppingClueStockLimit` 控制（默认保留 2 个，即库存 ≥ 3 才送）。

三档各自独立开关；刷新费不足**不会**触发补信用（旧版 `RefreshGetCredits` 已移除）。

### 折扣选项

每档的折扣选项改写对应档位的折扣 OCR `expected`，或改为 ColorMatch（「任意折扣」）。  
「任意折扣」用颜色匹配而非宽松 OCR，是为保留偏移锚点 ROI。  
折扣规则须同时覆盖买得起与买不起两侧。

### 强制策略与刷新

三档均未命中后的兜底，由 `CreditShoppingForce` 决定（`Flow/BuyAction.json` + `Flow/Special.json`）：

| 策略       | 行为                             |
| ---------- | -------------------------------- |
| 退出       | 不买任意品、不刷新，直接结束     |
| 忽略黑名单 | 购买任意买得起且未售罄的商品     |
| 刷新       | 尝试刷新货架；可展开「稳健刷新」 |

**稳健刷新**：若「当前信用 − 刷新花费 < 稳健刷新阈值」且架上仍有可买品，则不刷新、改为直接购买。  
该阈值与「保留信用点」是两套独立条件，不要混用。

## 新增商品时需改的路径

1. `assets/tasks/CreditShopping.json` — 在对应档位 checkbox 增加 case，同时写「买得起」与「买不起」两侧的 `attach`
2. `assets/resource/pipeline/CreditShopping/Flow/BuyItem.json` — `next` 列表加入该商品弹窗分支
3. `assets/resource/pipeline/CreditShopping/BuyItem/` — 新建对应商品文件（弹窗 OCR + focus 文案），参考已有文件结构
4. `assets/locales/interface/*.json` — `option.CreditShoppingItems.cases.*.label`

只改列表白名单、不改弹窗确认时，会出现能点开但 focus 缺失的问题。  
新增 case 后记得检查 `default_case` 是否也要纳入新商品。

## 维护要点

| 现象             | 优先查                                                          |
| ---------------- | --------------------------------------------------------------- |
| 识别不到目标商品 | attach 合并后的正则；识别链黑→粉逐层                            |
| 买得起却不买     | 该档保留阈值 / 无条件购买开关                                   |
| 买不起不补信用   | 该档 AutoGetCredits 开关；买不起侧白名单与折扣                  |
| 刷新行为异常     | `CreditShoppingForce`；稳健刷新阈值                             |
| 选项间行为不一致 | `CreditShopping.json` 的 `pipeline_override` 与扫描 `next` 顺序 |

维护时分四层定位：入口（`GoToShop.json`）→ 扫描决策（`Flow/`）→ 识别链（`Item/`）→ 参数装配（任务选项 + Go）。
