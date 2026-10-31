# 开发手册 - 自动购买稳定需求物资维护文档

本文说明 `AutoStockStaple` 的文件分布与执行流程。  
以 **四号谷地** 为主线；**武陵** Pipeline 结构完全对称，仅地区后缀与场景识别不同。  
该文档更新于 2026 年 10 月 30 日。

## 文件路径

| 路径 | 作用 |
| -------------------------------------------------------------------------- | ------------------------------------------------ |
| `assets/interface.json` | 任务挂载（`regional_development` 组） |
| `assets/tasks/AutoStockStaple.json` | 任务入口、地区开关、物品勾选、上限与分组折扣选项 |
| `assets/resource/pipeline/AutoStockStaple/Main.json` | 执行周期、入口初始化、地区子任务调度 |
| `assets/resource/pipeline/AutoStockStaple/ValleyIV.json` | 四号谷地列表扫描循环 |
| `assets/resource/pipeline/AutoStockStaple/Wuling.json` | 武陵列表扫描循环 |
| `assets/resource/pipeline/AutoStockStaple/General/Item.json` | 商品锚点、名称/折扣识别、BetterSliding、确认购买 |
| `assets/resource/pipeline/AutoStockStaple/General/Goods.json` | 购买弹窗内物品 OCR |
| `assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json` | 弹窗右上角持有数量 OCR |
| `assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json` | 弹窗分支调度、排除物品、确认购买 |
| `assets/resource/pipeline/AutoStockStaple/General/Template.json` | 售罄、调度券、确认购买等通用模板 |
| `assets/resource/pipeline/Interface/InScene/StockStaple.json` | 地区与稳定物资界面场景识别 |
| `assets/resource_adb/pipeline/AutoStockStaple/` | ADB ROI 偏移镜像（需与 Win32 同步检查） |
| `agent/go-service/autostockstaple/action.go` | 计算购买数量并驱动 BetterSliding |
| `agent/go-service/common/attachregex/action.go` | attach 关键词合并为 OCR 白名单正则 |
| `assets/locales/interface/*.json` | 任务、选项与 focus 文案 |

## 执行流程

1. 检查今日是否在[执行周期](#执行周期)内；未命中则直接结束。
2. 读取用户勾选的购买物品，[合并为各分组的商品名 OCR 白名单](#attach-与白名单初始化)（实现见 `Main.json` + `attachregex/action.go`）。
3. 按选项依次进入已启用的地区（四号谷地 / 武陵），跳转到该地区稳定物资界面。
4. 在列表页循环扫描，每轮按顺序判断：
    - 剩余调度券是否[低于保留阈值](#调度券保留阈值) → 停止本地区扫描。
    - 是否[识别到可买目标商品](#商品识别链与折扣分组) → 点击进入购买弹窗；同一轮按折扣分组顺序依次判定。
    - 是否已售罄 → 停止本地区扫描。
    - 否则向下滑动列表继续找，单地区最多滑 25 次（`AutoStockSwipe{Region}.max_hit`）；次数用完仍在物资调度页即视为列表已到底。
5. 购买弹窗内按[数量控制三分支](#数量控制三分支)处理：券不足退出 / 未达上限则购买 / 已达上限则排除。
6. 所有启用地区完成后结束。

> 列表里「识别到商品并点击」不等于「购买成功」；只有数量控制走到确认购买才算下单。

## 特殊处理

### 执行周期

实现位于 `Main.json`。用户勾选的星期几会写入周期节点的 `attach`，由 `ScheduleRecognition` 判断今天是否执行；未勾选的日子任务直接结束，不进入购买流程。

### attach 与白名单初始化

本任务不靠运行时拼用户输入字符串，而是：

1. 用户在界面勾选物品（类目 switch 开启后，物资多选框嵌套其下）→ `assets/tasks/AutoStockStaple.json` 把各语言商品名写入**所属折扣分组**的商品名节点：`AutoStockInStapleItemName{Group}.attach.{slug}`。
2. 任务入口执行 `AttachToExpectedRegexAction`，`targets` 列出全部分组商品名节点，逐个读取该节点的 attach 关键词，合并为 `^(别名1|别名2|...)$` 正则覆盖回同一节点。
3. `attach` 为 `false` 的键会被排除，不再进入白名单。

Exclude 分支（物品已达标或券不足被剔除）也会触发重新初始化，保证后续列表扫描不会再点已排除的物品。  
排除动作通过 `PipelineOverrideAction` 将该物品所属分组的 `attach.{slug}` 设为 `false`。

### 调度券保留阈值

实现位于 `ValleyIV.json` / `Wuling.json`（武陵节点名后缀为 `Wuling`）。

扫描循环的**第一项**判断：右上角调度券 OCR 读数，与用户配置的保留阈值做表达式比较。  
若「保留阈值 > 当前剩余券」，表示券已不够继续买，结束本地区扫描；否则继续找商品。

列表扫描阶段**不**判断单价是否买得起；买得起与否留到购买弹窗内处理。

### 商品识别链与折扣分组

实现位于 `General/Item.json`（商品名与折扣节点）+ 地区 JSON 中的 `AutoStockBuyItem{Region}Task{Group}`。思路与[信用点商店](./credit-shopping-maintain.md)类似：**先找锚点，再偏移识别后续字段**，但锚点是商品卡片左上角的**剩余刷新时间框**（青绿 / 黄 / 红三组颜色的 ColorMatch），不是信用点图标。

```text
剩余时间锚点 → 商品名（颜色 + 分组 OCR 白名单） → 折扣（OCR 匹配该分组所选档位的折扣值）
```

折扣按分组独立设置，分组与[数量上限分组](#数量控制购买弹窗)一致：

| 分组 `{Group}` | 覆盖商品 | 商品名节点 | 折扣节点 |
| --- | --- | --- | --- |
| `Daily` | 日用消耗中除刻写券外的物资 | `AutoStockInStapleItemNameDaily` | `AutoStockInStapleItemDiscounts{Region}Daily` |
| `Industrial` | 工业货品中除精锻助剂外的物资 | `AutoStockInStapleItemNameIndustrial` | `AutoStockInStapleItemDiscounts{Region}Industrial` |
| `Cultural` | 人文物产 | `AutoStockInStapleItemNameCultural` | `AutoStockInStapleItemDiscounts{Region}Cultural` |
| `EngravingPermit` | 刻写券（`item_gem_stable_energy_point_*`） | `AutoStockInStapleItemNameEngravingPermit` | `AutoStockInStapleItemDiscounts{Region}EngravingPermit` |
| `ArtificingCatalyst` | 精锻助剂（`item_equip_enhance_*`） | `AutoStockInStapleItemNameArtificingCatalyst` | `AutoStockInStapleItemDiscounts{Region}ArtificingCatalyst` |

`{Region}` 为 `ValleyIV` / `Wuling`。精锻助剂目前只有武陵售卖，因此没有 `AutoStockInStapleItemDiscountsValleyIVArtificingCatalyst`。

1. **锚点**：定位列表中每个商品卡片的时间区域，作为后续偏移基准。
2. **商品名**：锚点 → 名称标签色 → 名称文字色 → 该分组的商品名 OCR；只命中写入该分组白名单的商品，`AutoStockInStapleItemName{Group}_Expected` 把这三步组合成「该分组存在可买商品」。
3. **折扣**：从该分组商品名区域偏移到折扣位，OCR 的 `expected` 只列该分组所选档位及更大的折扣值（如 `-90%` 档为 `-99` / `-95` / `-90`，`order_by: Expected`）；负号漏识的读数不会命中，漏买而非错买。
4. **折扣档位**：`AutoStockMinDiscount{Region}{Group}` 的数值档直接改写该分组折扣节点的 `expected`，没有单独的比较节点；无折扣徽标的物资 OCR 无文本、不命中，除「任意」档外都不会购买。
5. **购买任务**：`AutoStockBuyItem{Region}Task{Group}` = 锚点 + `AutoStockInStapleItemName{Group}_Expected` + 该分组折扣节点（`box_index: 2`）。地区扫描循环按 `EngravingPermit` → `ArtificingCatalyst` → `Daily` → `Industrial` → `Cultural` 的顺序依次尝试，先命中先买；分组之间商品互不重叠，所以顺序只影响同一屏多家可买时的先后。

锚点、商品名、折扣三者同时命中才点击商品，进入数量控制。

> [!NOTE]
> 分组判定只决定「用哪一档折扣」，不影响数量上限：刻写券走刻写券上限、精锻助剂走强化催化剂上限，其余按类目上限，规则见[新增物品](#新增物品)。

### 数量控制三分支

实现分布在 `General/QuantityControl.json`、`Goods.json`、`GoodsCountValidate.json` 与 `autostockstaple/action.go`。  
弹窗打开后，依次尝试各物品的专用分支，每个物品固定三路：

#### 分支 1：调度券不足

弹窗内识别到底部红色「调度券不足」提示时：

1. 将该物品从所属分组的 attach 白名单排除。
2. 重新合并全部分组的正则白名单。
3. 关闭弹窗，回到列表——避免反复点击买不起的商品。

#### 分支 2：持有量未达上限，执行购买

读取弹窗右上角当前持有数量 OCR，与用户配置的上限做表达式比较（`上限 > 当前持有量` 时命中）：

1. Go 动作计算 `需购数量 = 上限 − 当前持有量`。
2. 将结果写入 BetterSliding 的 `TargetQuantity`，平滑调节购买数量滑条。
3. 点击确认购买，关闭奖励弹窗，回到列表继续扫描。

#### 分支 3：持有量已达上限，排除

`上限 <= 当前持有量` 时命中：

1. 从所属分组的 attach 白名单排除该物品。
2. 重新合并全部分组的正则白名单。
3. 关闭弹窗，不购买，继续扫描其他物品。

> **举例**：谷地刻写券上限 200、当前持有 198，分支 2 会算出再买 2 个；若已持有 200，走分支 3 直接剔除，本轮不再点击该商品。

### 运行时 Override 一览

| 时机 | 动作 | 作用 |
| ------------ | ------------------------------------------------------------- | ---------------------------------------- |
| 任务入口 | `AttachToExpectedRegexAction` | 逐个分组合并 attach → 该分组商品名 OCR 正则 |
| 任务入口 | 类目 switch / 分组折扣 select 的 `pipeline_override` | 类目关闭时不收集该类 override；分组折扣数值档改写该分组折扣 OCR 的 `expected`，「任意」档把该分组折扣识别节点替换为恒定命中的 `ColorMatch` |
| 物品被排除后 | `PipelineOverrideAction` + 再次 `AttachToExpectedRegexAction` | 在所属分组剔除 attach 键并刷新全部分组白名单 |
| 确认购买前 | `AutoStockStapleQuantityControlAction` | 算差值并 override BetterSliding 目标数量 |

## 数据来源与增量更新

商品名单、分类、价格与五语名称由 `tools/zmdmap/datasets/stock_staple.py` 从原始 TableCfg 生成，消费端不维护固定商品绑定表。数据关系如下：

```text
DomainDataTable → ShopGroupTable → ShopTable → ShopGoodsTable
                                           ├ RewardTable → ItemTable → 五语 I18nTextTable
                                           ├ ShopGoodsTagTable → 五语分类名称
                                           └ ShopChannelDevelopmentTable → 渠道等级供货量
                  ↓
stock_staple.json → AutoStockStaple/generate.mjs
                  ├ assets/tasks/AutoStockStaple.json
                  ├ General/{Goods,GoodsCountValidate,QuantityControl}.json
                  └ assets/locales/interface/*.json 中的任务文本
```

只选择地区常规商店（`shopRefreshType=None/0`）中每周补货（`limitCountRefreshType=Weekly/2`）的商品；`Forever`（整数形态为 0）等非每周补货的商品与弹性商店不纳入。TableCfg 的枚举可能写成名字或整数，筛选两种写法都认。`goods` 按售卖配置 ID 保留价格、折扣、币种 ID、奖励数量、刷新类型、解锁条件和渠道供货记录；任务选项再按 `item_id` 去重，因此同物品的不同价格档不会丢失。

现有任务、Pipeline 与语言文件作为基线。生成器从任务选项的 `resource/image/UI/Item/<item_id>.png` 路径识别已收录物品，只补上游新增项，不另外维护商品绑定表。已有商品的 key、名称、OCR 特例、ROI、节点顺序与数量策略原样保留；上游改名、分类变化或移除商品不会自动改写或删除已有配置，因此原来的勾选不会失效。

`tools/pipeline-generate/AutoStockStaple/pipeline-template.json` 只保存新增商品的节点模板，`messages.json` 保存五语提示句式。新增选项名由上游英文名称去掉撇号后转成 PascalCase；attach 和提示使用游戏 `item_id`，节点后缀使用其 PascalCase。已有生成项只补缺失节点和文案，不覆盖现存值；没有增量时不写文件。

下载上游发布数据并增量补充任务配置：

```shell
pnpm generate:AutoStockStaple
```

有本地原表时，可离线重建数据并完成同样的增量更新；只需要上图的八张业务表及 `I18nTextTable_{CN,TC,EN,JP,KR}.json`，不需要 JsonData、navmesh、其他精简表或网站查询接口：

```shell
uv run zmdmap generate --only stock_staple --table-cfg-dir /path/to/TableCfg
node tools/pipeline-generate/AutoStockStaple/generate.mjs
```

基线文件整份缺失或损坏时，先从 Git 恢复，再运行增量更新；上游商品表不保存历史 OCR 修正，不能用于还原这些手写配置。生成器在所有输入可解析、所有新增项构建成功后才写入，保留 JSONC 注释和其他任务的语言内容。UI 图标继续由仓库既有图标发布链维护。

`.github/workflows/sync-zmdmap.yml` 在现有数据同步任务中运行同一命令。`stock_staple` 与其他数据集一样，由 zmdmap 自动发现、生成并纳入发布清单，再通过现有 `fetch` 下载入库。有效缓存可重建缺失、损坏或残旧的入库副本；清单指纹变化会重新下载，即使版本号未变。

静态价格用于保留完整售卖数据；运行时购买仍按原流程识别实际折扣、持有量和调度券，不用配置价格推算当次成交结果。

## 新增地区时需改的路径

对照四号谷地复制一套即可（武陵即为现成镜像）：

```text
锚点 AutoStockInStapleItem
  -> 分组商品名 AutoStockInStapleItemName{Group}_Expected（每个分组一条）
  -> 分组折扣 AutoStockInStapleItemDiscounts{Region}{Group}
  -> 点击并进入数量控制（AutoStockBuyItem{Region}Task{Group}）
```

### 1. 锚点：剩余刷新时间框

`AutoStockInStapleItem` 使用 `ColorMatch` 识别商品卡片左上角的剩余时间区域（青绿 / 黄 / 红三组 HSV 范围取并集的连通域，`order_by: Vertical`），作为后续所有偏移识别的基准 box。

### 2. 偏移识别商品名

在锚点基础上依次偏移：

1. `AutoStockInStapleItemNameLabelColor`：名称标签底色。
2. `AutoStockInStapleItemNameTextColor`：名称文字色。
3. `AutoStockInStapleItemName{Group}`：OCR 识别商品名，`expected` 由运行时初始化写入；每个折扣分组各有一个这样的节点。
4. `AutoStockInStapleItemName{Group}_Expected`：`And` 组合上述三者，`box_index: 2` 取商品名 OCR 的 box。

用户勾选的物品名通过 `AutoStockInStapleItemName{Group}.attach.{slug}` 写入所属分组的各语言别名；任务开始时 `AttachToExpectedRegexAction` 对每个分组节点分别合并为：

```text
^(别名1|别名2|...)$
```

未勾选的物品不会进入白名单，OCR 不会命中。

### 3. 偏移识别折扣

`AutoStockInStapleItemDiscounts{Region}{Group}` 以对应分组的 `AutoStockInStapleItemName{Group}` 的 box 为基准，`roi_offset` 偏移到折扣区域，OCR 的 `expected` 默认列出 `-99` 到 `-50` 的全部折扣值（`order_by: Expected`）。

每个分组各有一个由 `AutoStockMinDiscount{Region}{Group}`（select）控制的折扣阈值，与信用点商店共用 `option.DiscountValue.*` 文案键族：

- **数值档**（`-95%` … `-50%`）：把该分组折扣节点的 `expected` 改写为该档及更大的折扣值（如 `-90%` → `-99`、`-95`、`-90`），OCR 命中其一才算满足。
- **任意**（`Any`）：将该分组折扣 OCR 节点整体替换为 `ColorMatch`（全范围阈值 `[0,0,0]`–`[255,255,255]` 且 `count` 默认 1，非空 ROI 恒定通过，折扣色块是否存在不影响结果）——行为等同于购买该分组所有勾选物资，不再检查折扣。

档位只列稳定物资实际出现的折扣（`-95/-90/-85/-80/-75/-70/-65/-60/-50`），默认档位由各分组 select 的 `default_case` 决定（目前谷地全部分组 `-50%`；武陵刻写券与人文物产 `-95%`、工业货品 `-75%`、精锻助剂 `-90%`、日用消耗 `-50%`）。无折扣徽标的物资在任何数值档下 OCR 都无文本、不命中，需选「任意」才会购买。

### 4. “能否买得起”的判断

与信用点商店不同，稳定物资列表扫描 **没有** 单独的“单价 ColorMatch / CanAfford”节点。买得起与否分两层处理：

| 阶段 | 机制 |
| ---------- | --------------------------------------------------------------------------------- |
| 列表扫描前 | `AutoStockTargetCanNotBuyValleyIV`：剩余调度券是否仍高于保留阈值 |
| 购买弹窗内 | `AutoStockStapleGoodsStockBillInsufficientValidate`：识别底部红色“调度券不足”提示 |

因此，`AutoStockBuyItemValleyIVTask{Group}` 的 `And` 条件为：

- `AutoStockInStapleItem`
- `AutoStockInStapleItemName{Group}_Expected`
- `AutoStockInStapleItemDiscountsValleyIV{Group}`（该分组的折扣 OCR；`box_index: 2` 指向该节点）

三者同时命中后，点击商品卡片（`target_offset: [-45, 85, -30, 0]`），`next` 进入 `AutoStockStapleQuantityControl`。

> [!IMPORTANT]
> `AutoStockBuyItemValleyIVTask{Group}` 只表示“识别到候选商品并进入购买判定”，**不等于** 已完成购买。是否真正下单，要看数量控制分支是否走到 `AutoStockStapleQuantityControlConfirmBuy`。

### 5. 售罄与滑动

- `SoldOut`：OCR 识别左侧“已售罄 / Sold Out”等文案，命中后不再滑动。
- `AutoStockSwipe{Region}`：在地区页内向下滑动，`post_wait_freezes` 等待列表区域稳定后再进入下一轮识别。`max_hit` 为 `25`，即单个地区最多滑 25 次；`max_hit` 在同一任务内累计，购买往返不消耗次数。
    - 循环只有两个结束信号：`SoldOut` 命中，或 25 次滑完后由 `AutoStockNoMoreTarget{Region}` 接住（它排在滑动节点之后，只在滑动节点被 `max_hit` 跳过时才命中），按「列表已到底且没有符合条件的商品」结束本地区扫描。
    - 所以 `max_hit` 与 `AutoStockNoMoreTarget{Region}` 要成对保留：这是「滑完仍没有结果」的唯一出口，缺了它就该地区反复空转；列表比 25 屏还长时才需要调大 `max_hit`。
    - 曾短暂改用 `ListCompleteRecognition`（比较相邻两帧判断到底）替代这个次数上限：滑动本身没生效时前后两帧同样静止，会被误判成「已到底」而漏扫整片列表，因此回退为次数上限。

## 数量控制（购买弹窗）

点击商品后进入购买弹窗，`AutoStockStapleQuantityControl` 以标题 OCR（“购买商品 / Purchase”）确认弹窗已打开，再按 `next` 列表依次尝试各物品的 `AutoStockStapleQuantityControl{Item}` 节点。

以 `AutoStockStapleQuantityControlValleyEngravingPermit`（谷地刻写券）为例，其 `next` 顺序固定为：

```text
AutoStockStapleQuantityControlValleyEngravingPermitStockBillInsufficient
  -> AutoStockStapleQuantityControlValleyEngravingPermitBuy
  -> AutoStockStapleQuantityControlValleyEngravingPermitExclude
```

### 读取当前持有数量

弹窗右上角持有数量由 `AutoStockStapleGoodsCountValidate` 识别：

- `AutoStockStapleGoodsCountValidateArea` → `AutoStockStapleGoodsCountValidateColor`：先在固定 ROI 内找灰色数量底框，再取框内亮色数字区域。
- `AutoStockStapleGoodsCountValidateText`：OCR 读取 `\d+`。

各物品的 Buy / Exclude 分支通过 `ExpressionRecognition` 与用户配置的上限比较，例如：

```text
Buy:     {ValleyEngravingPermitLimit} > {AutoStockStapleGoodsCountValidate}
Exclude: {ValleyEngravingPermitLimit} <= {AutoStockStapleGoodsCountValidate}
```

### 分支 1：调度券不足，直接退出

`AutoStockStapleQuantityControl{Item}StockBillInsufficient` 组合：

- 当前物品 OCR（如 `AutoStockStapleGoodsValleyEngravingPermit`）
- `AutoStockStapleGoodsStockBillInsufficientValidate`（底部红色区域 ColorMatch）

命中后：

1. `[JumpBack]` 到 `{Item}RemoveFilter`，从 `AutoStockInStapleItemName.attach` 中 **排除** 该物品（`attach.{slug}: false`），并触发 `AutoStockStapleQuantityControlResetRecognitionParams` 重新生成白名单正则。
2. 关闭购买弹窗（`AutoStockStapleQuantityControlCloseBuyWindow`）。

这样后续列表扫描不会再反复点击买不起的商品。

### 分支 2：数量低于目标，执行购买

`AutoStockStapleQuantityControl{Item}Buy` 在物品 OCR + `Validate` 表达式同时成立时命中，执行专用 Custom 动作 `AutoStockStapleQuantityControlAction`（`agent/go-service/autostockstaple/action.go`）。

动作逻辑：

1. 读取对应 `AutoStockStapleGoods{Item}Validate` 节点表达式，解析 **目标上限** 与 **数量 OCR 节点名**。
2. 对当前截图运行数量 OCR，得到 **当前持有数量**。
3. 计算 `target = 目标上限 - 当前持有数量`。
4. 若 `target <= 0`，只记日志并返回成功，不做 override（`AutoStockStapleBetterSliding` 保持原状态）。
5. 否则通过 `OverridePipeline` 启用 `AutoStockStapleBetterSliding`，并将 `target` 写入其 `attach.TargetQuantity`。

Go **不再** `RunTask` 执行滑动；数量调节由低代码分支完成：

```text
{Item}Buy (Go: 计算 target + OverridePipeline)
  ├ AutoStockStapleCheckSliding              （滑条隐藏且默认数量为 1 时跳过滑动）
  └ AutoStockStapleBetterSliding             （Go 启用后执行 BetterSliding）
  → AutoStockStapleQuantityControlConfirmBuy
```

`AutoStockStapleBetterSliding` 定义在 `General/Item.json`，默认 `enabled: false`，仅在 Go override 后启用；`attach.TargetQuantity` 的默认值只是占位。

购买数量调节完成后，`next` 进入 `AutoStockStapleQuantityControlConfirmBuy` 点击黄色确认按钮，再关闭奖励弹窗回到列表。

### 分支 3：数量已达到或超过目标，排除并重新初始化

`AutoStockStapleQuantityControl{Item}Exclude` 在 `ExcludeValidate` 成立时命中（当前持有数量 **不低于** 用户上限）。

流程：

1. `{Item}RemoveFilter`：调用 `PipelineOverrideAction`，将该物品在所属分组的 attach 键设为 `false`，等效于从该组白名单移除。
2. `AutoStockStapleQuantityControlResetRecognitionParams`：再次执行 `AttachToExpectedRegexAction`（`targets` 覆盖全部分组），基于最新 attach 状态 **重新生成** 各分组 `AutoStockInStapleItemName{Group}.expected` 正则。
3. 关闭购买弹窗，回到列表继续扫描其他物品。

Exclude 分支 **不会** 购买，仅把“已达标”的物品从本轮扫描目标中剔除。

## 初始化与 Override 机制小结

本任务存在多类运行时 override，维护时不要混淆：

| 动作 | 触发位置 | 作用 |
| -------------------------------------- | ------------------------------------------------- | ----------------------------------------------------------------- |
| 选项的 `pipeline_override`（`interface.json` 声明） | 任务选项收集（MXU 渲染阶段） | 类目关闭时不收集该类 override；分组折扣数值档改写该分组折扣 OCR 的 `expected`；「任意」档把该分组折扣识别节点替换为 `ColorMatch` |
| `AttachToExpectedRegexAction` | `AutoStockStapleMain` 入口；Exclude 后 Reset 节点 | 逐个分组合并 attach 关键词 → 该分组 OCR 白名单正则 |
| `PipelineOverrideAction` | 各物品 `{Item}RemoveFilter` | 在所属分组将指定 attach 键设为 `false`，排除该物品 |
| `AutoStockStapleQuantityControlAction` | 各物品 `{Item}Buy` | 计算差值并 override BetterSliding 的 `TargetQuantity` / `enabled` |

`attach` 语义（见 `attachregex/action.go`）：

- `string` / `string[]`：加入白名单关键词。
- `false`：显式排除该 attach 键，不再参与合并。
- `true`：当前实现下不追加关键词。

## 新增物品

上游原表新增商品后，重新发布 `stock_staple.json`，再运行 `pnpm generate:AutoStockStaple`；商品 case、名称 OCR、数量比较节点、三路购买分支和五语文案会一起生成。不再手工添加商品名、绑定表或旧别名。

现有数量分组沿用任务策略：生活用品中的刻写券 ID 家族使用刻写券上限，其余使用食物增益上限；工业用品中的装备强化材料 ID 家族使用强化催化剂上限，其余使用探测器上限；文化用品使用礼物上限。折扣分组与数量分组一致（刻写券、精锻助剂各自成组，其余按类目分组），因此新增商品会自动落到对应分组的折扣控制下，不需要新增折扣选项。新增类目或需要新导航的地区会明确报错，应先适配任务选项与流程。若有截图证明某件商品需要 OCR 修正，直接修改该商品节点；增量更新会保留这些修正。

已有流程仍在任务与 Pipeline 文件中维护；单商品模板只决定以后新增的节点，不会覆盖已有节点。

## 新增地区（参考四号谷地）

若将来增加第三个地区的稳定物资购买，可对照四号谷地复制一套节点：

1. 新建 `assets/resource/pipeline/AutoStockStaple/{Region}.json`：
    - `AutoStockInStaple{Region}` 扫描循环（`next` 顺序不变，末尾固定为 `[JumpBack]AutoStockSwipe{Region}` + 滑动次数用完后的 `AutoStockNoMoreTarget{Region}`；分组判定项变成多条 `[JumpBack]AutoStockBuyItem{Region}Task{Group}`）。
    - `AutoStockTargetCompare{Region}` / `AutoStockTargetCanNotBuy{Region}`。
    - 每个折扣分组一条 `AutoStockBuyItem{Region}Task{Group}`（替换分组折扣节点名）。
    - `AutoStockSwipe{Region}`（`max_hit` 按列表长度给，参考四号谷地的 25）与兜底出口 `AutoStockNoMoreTarget{Region}`。
2. 在 `Main.json` 增加 `[JumpBack]AutoStockStaple{Region}` 子任务与 SceneManager 跳转。
3. 在 `StockStaple.json` 或地区 InScene 文件中补充场景 OCR。
4. 在 `Item.json` 为每个分组增加 `AutoStockInStapleItemDiscounts{Region}{Group}`（`Daily` / `Industrial` / `Cultural` / `EngravingPermit`，有精锻助剂商品时再加 `ArtificingCatalyst`；布局相同也要单独建，`AutoStockMinDiscount{Region}{Group}` 按分组改写该节点），并在 `resource_adb` 的 `Item.json` 补它的 `roi_offset`。
5. 在 `assets/tasks/AutoStockStaple.json` 增加地区 switch、空物品选项组与分组折扣选项（嵌在对应类目 switch 下），在生成器中接入地区 ID 与导航后缀，再运行增量更新。

武陵现有实现即为四号谷地的镜像，可直接 diff `ValleyIV.json` 与 `Wuling.json` 查看差异点。

## 调试建议

| 现象 | 优先检查 |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| 列表中识别不到目标商品 | `go-service.log` 中 `AttachToExpectedRegexAction` 打印的各分组 `expected` 正则；锚点 `AutoStockInStapleItem` 是否命中 |
| 只有某一类物资不买 | 该类目 switch 是否开启；`AutoStockMinDiscount{Region}{Group}` 所选档位与该分组折扣节点 `expected` 是否覆盖实际折扣 |
| 列表没到底就停止扫描 | `AutoStockSwipe{Region}.max_hit` 是否太小（列表长、单次行程小）；`AutoStockNoMoreTarget{Region}` 是否被提前命中 |
| 一直滑动不结束 | `AutoStockSwipe{Region}.max_hit` 是否被删掉或改得过大；`AutoStockNoMoreTarget{Region}` 是否存在且排在滑动节点之后 |
| 识别到商品但未购买 | 数量控制是否走 `Exclude` 或 `StockBillInsufficient`；`maafw*.log` 中 `AutoStockStapleQuantityControl{Item}Buy/Exclude` |
| 购买数量不对 | `AutoStockStapleQuantityControlAction` 日志中的 `threshold/current_count/target`；BetterSliding ROI |
| 调度券明明够却停止扫描 | `AutoStockTargetCompareValleyIV` 表达式与用户输入的 `{ReserveValleyIV}` |
| 反复点击同一已达标商品 | Exclude 后 `{Item}RemoveFilter` 与 `ResetRecognitionParams` 是否执行 |

日志分析可参考 skill：`.agents/skills/autostockstaple-log-analysis/SKILL.md`。

## 与 AutoStockpile 的区别

| 项目 | AutoStockStaple（稳定需求） | AutoStockpile（弹性囤货） |
| -------- | --------------------------- | ------------------------- |
| 决策主体 | Pipeline + 少量 Go | Go Service 主导 |
| 商品定位 | 列表时间锚点 + OCR 偏移链 | 模板匹配 + OCR 映射 |
| 数量控制 | 弹窗 BetterSliding + 表达式 | Go 解析详情页调节 |

两者界面相似但逻辑独立；日志分析见 `.agents/skills/autostockstaple-log-analysis/SKILL.md`。
