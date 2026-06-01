# 开发手册 - 自动购买稳定需求物资维护文档

本文说明 `AutoStockStaple`（自动购买稳定需求物资）的整体结构、任务选项如何覆盖 Pipeline 行为，以及商品识别与数量控制的核心逻辑，便于后续维护与扩展。

本文以 **四号谷地（ValleyIV）** 为主线介绍。武陵（Wuling）的 Pipeline 结构与四号谷地完全一致，仅节点名后缀、场景识别与折扣节点名不同；新增地区时可对照四号谷地实现。

## 文件概览

| 模块               | 路径                                                                 | 作用                                                                 |
| ------------------ | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| 项目接口挂载       | `assets/interface.json`                                              | 将 `tasks/AutoStockStaple.json` 挂到任务组                           |
| 任务与选项定义     | `assets/tasks/AutoStockStaple.json`                                  | 定义任务入口、地区开关、物品勾选、数量上限、折扣策略、`pipeline_override` |
| 任务入口           | `assets/resource/pipeline/AutoStockStaple/Main.json`                 | 调度周期、主入口初始化、四号谷地/武陵子任务入口                      |
| 地区扫描循环       | `assets/resource/pipeline/AutoStockStaple/ValleyIV.json`             | 四号谷地稳定物资列表扫描、购买点击、滑动                               |
| 地区扫描循环       | `assets/resource/pipeline/AutoStockStaple/Wuling.json`               | 武陵稳定物资列表扫描（结构与 ValleyIV 对称）                         |
| 商品列表识别       | `assets/resource/pipeline/AutoStockStaple/General/Item.json`         | 锚点、商品名、折扣、BetterSliding、确认购买等                        |
| 购买弹窗物品识别   | `assets/resource/pipeline/AutoStockStaple/General/Goods.json`          | 购买弹窗内各物品的 OCR 识别                                          |
| 持有数量识别       | `assets/resource/pipeline/AutoStockStaple/General/GoodsCountValidate.json` | 弹窗右上角当前持有数量 OCR + 各物品 Buy/Exclude 表达式验证       |
| 数量控制           | `assets/resource/pipeline/AutoStockStaple/General/QuantityControl.json` | 购买弹窗打开后的分支调度、排除物品、确认购买                     |
| 通用模板           | `assets/resource/pipeline/AutoStockStaple/General/Template.json`     | 售罄、调度券 OCR、确认购买文案等                                     |
| 场景识别           | `assets/resource/pipeline/Interface/InScene/StockStaple.json`        | `InValleyIVText`、`InWulingText`、`InStapleColor`                   |
| Go 数量控制动作    | `agent/go-service/autostockstaple/action.go`                         | 计算需购数量并 override BetterSliding 的 `Target`                    |
| Go 正则初始化      | `agent/go-service/common/attachregex/action.go`                      | `AttachToExpectedRegexAction`：将 attach 关键词合并为 OCR 白名单正则 |
| 节点代码生成       | `tools/pipeline-generate/AutoStockStaple/General/`                   | 批量生成 `Goods.json`、`GoodsCountValidate.json`、`QuantityControl.json` |
| 多语言文案         | `assets/locales/interface/*.json`                                    | 任务名、选项与 focus 文案                                            |

> [!NOTE]
> ADB 控制器下部分 ROI 偏移位于 `assets/resource_adb/pipeline/AutoStockStaple/`，Win32 与 ADB 维护时需同步检查。

## 总体执行逻辑

任务入口为 `Main.json` 中的 `AutoStockStapleMain`：

1. **初始化正则白名单**：执行 `AttachToExpectedRegexAction`，读取 `AutoStockInStapleItemName` 节点上所有 `attach` 关键词（来自用户勾选的物品选项），合并后 override 到该节点的 `expected` 正则。
2. **按地区子任务执行**：依次尝试 `[JumpBack]AutoStockStapleValleyIV`、`[JumpBack]AutoStockStapleWuling`；未启用的地区节点默认 `enabled: false`。
3. **进入稳定物资界面**：子任务通过 SceneManager 跳转到对应地区的物资调度界面，再进入 `AutoStockInStapleValleyIV` / `AutoStockInStapleWuling` 扫描循环。
4. **全部完成后**命中 `AutoStockStapleDone` 结束。

### 任务选项如何写入 Pipeline

`assets/tasks/AutoStockStaple.json` 中的选项通过 `pipeline_override` 直接改写节点字段，典型包括：

| 选项类型           | 覆盖目标示例                                      | 作用                                       |
| ------------------ | ------------------------------------------------- | ------------------------------------------ |
| 地区开关           | `AutoStockStapleValleyIV.enabled`                 | 是否执行四号谷地购买                         |
| 调度券保留阈值     | `AutoStockTargetCompareValleyIV` 的 `expression`  | 剩余调度券低于阈值时停止继续购买             |
| 勾选购买物品       | `AutoStockInStapleItemName.attach.{slug}`         | 向 attach 写入各语言商品名，供初始化合并   |
| 物品持有上限       | `AutoStockStapleGoods{Item}Validate` 等           | 改写 `{Limit} > {AutoStockStapleGoodsCountValidate}` |
| 折扣策略           | `AutoStockInStapleItemDiscountsValleyIV`          | 改写折扣 OCR 的 `expected` 或改为 ColorMatch |

初始化动作 **不会** 直接读取用户输入的字符串，而是依赖 interface 已写入目标节点 `attach` 的内容；Go 侧再将其转换为 OCR 正则。

## 四号谷地列表扫描循环

进入四号谷地稳定物资界面后，`AutoStockInStapleValleyIV` 在同一心跳内按 `next` 顺序依次判断：

```text
AutoStockTargetCanNotBuyValleyIV
  -> [JumpBack]AutoStockBuyItemValleyIVTask
  -> SoldOut
  -> [JumpBack]AutoStockSwipeValleyIV
```

| 顺序 | 节点                               | 含义                                                         |
| ---- | ---------------------------------- | ------------------------------------------------------------ |
| 1    | `AutoStockTargetCanNotBuyValleyIV` | 当前剩余调度券是否 **低于** 用户配置的保留阈值                 |
| 2    | `AutoStockBuyItemValleyIVTask`     | 是否识别到 **可购买的目标商品**                               |
| 3    | `SoldOut`                          | 是否看到 **已售罄** 标志，命中后任务在该地区停止继续扫描       |
| 4    | `AutoStockSwipeValleyIV`           | 向下滑动列表，继续寻找商品（`max_hit: 25`）                   |

`AutoStockInStapleValleyIV` 的识别条件为：`InValleyIVText` + `InStapleColor` + `InStockStaple`，确保当前处于四号谷地稳定物资页。

### 调度券阈值判断

`AutoStockTargetCanNotBuyValleyIV` 组合 `InStapleColor` 与 `AutoStockTargetCompareValleyIV`。

`AutoStockTargetCompareValleyIV` 使用 `ExpressionRecognition`：

```text
{ReserveValleyIV} > {AutoStockCurrentStockBill}
```

- `AutoStockCurrentStockBill`：右上角调度券 OCR（`CurrentStockBillColor` + `CurrentStockBillText`）。
- `{ReserveValleyIV}`：用户在 `AutoStockReserveValleyIV` 输入的保留阈值，默认 `240000`。
- 表达式成立表示“剩余调度券已 **低于** 保留阈值，需要停止继续购买”，因此会命中 `AutoStockTargetCanNotBuyValleyIV` 结束本地区扫描；不成立则表示仍可继续买。

## 商品识别链（是否有物品可以购买）

`AutoStockBuyItemValleyIVTask` 的实现思路与 [信用点商店](./credit-shopping-maintain.md) 的商品扫描类似：**先找锚点，再基于锚点偏移识别后续字段**。稳定物资列表页以商品左上角的 **剩余刷新时间框** 作为锚点，而不是信用点商店的 `CreditIcon`。

读图顺序建议：

```text
锚点 AutoStockInStapleItem
  -> 商品名 AutoStockInStapleItemName_Expected
  -> 折扣 AutoStockInStapleItemDiscountsValleyIV
  -> 点击并进入数量控制
```

### 1. 锚点：剩余刷新时间框

`AutoStockInStapleItem` 使用 `ColorMatch` 识别商品卡片左上角的剩余时间区域（青绿色连通域，`order_by: Vertical`），作为后续所有偏移识别的基准 box。

### 2. 偏移识别商品名

在锚点基础上依次偏移：

1. `AutoStockInStapleItemNameLabelColor`：名称标签底色。
2. `AutoStockInStapleItemNameTextColor`：名称文字色。
3. `AutoStockInStapleItemName`：OCR 识别商品名，`expected` 由运行时初始化写入。
4. `AutoStockInStapleItemName_Expected`：`And` 组合上述三者，`box_index: 2` 取商品名 OCR 的 box。

用户勾选的物品名通过 `AutoStockInStapleItemName.attach.{slug}` 写入各语言别名；任务开始时 `AttachToExpectedRegexAction` 将其合并为：

```text
^(别名1|别名2|...)$
```

未勾选的物品不会进入白名单，OCR 不会命中。

### 3. 偏移识别折扣

`AutoStockInStapleItemDiscountsValleyIV` 以 `AutoStockInStapleItemName` 的 box 为基准，`roi_offset` 偏移到折扣区域，默认用 OCR 识别 `95/90/85/...` 等折扣数值。

`AutoStockUseDiscountsValleyIV` 选项可改写该节点：

- 选 **任意折扣**：将识别类型改为 `ColorMatch`，只要折扣区域有内容即通过。
- 选具体折扣档：改写 `expected` 列表，仅允许不低于该档的折扣（含 `-99` 等占位符处理）。

### 4. “能否买得起”的判断

与信用点商店不同，稳定物资列表扫描 **没有** 单独的“单价 ColorMatch / CanAfford”节点。买得起与否分两层处理：

| 阶段       | 机制                                                                 |
| ---------- | -------------------------------------------------------------------- |
| 列表扫描前 | `AutoStockTargetCanNotBuyValleyIV`：剩余调度券是否仍高于保留阈值       |
| 购买弹窗内 | `AutoStockStapleGoodsStockBillInsufficientValidate`：识别底部红色“调度券不足”提示 |

因此，`AutoStockBuyItemValleyIVTask` 的 `And` 条件为：

- `AutoStockInStapleItem`
- `AutoStockInStapleItemName_Expected`
- `AutoStockInStapleItemDiscountsValleyIV`

三者同时命中后，点击商品卡片（`target_offset: [-50, 95, 0, 0]`），`next` 进入 `AutoStockStapleQuantityControl`。

> [!IMPORTANT]
> `AutoStockBuyItemValleyIVTask` 只表示“识别到候选商品并进入购买判定”，**不等于** 已完成购买。是否真正下单，要看数量控制分支是否走到 `AutoStockStapleQuantityControlConfirmBuy`。

### 5. 售罄与滑动

- `SoldOut`：OCR 识别左侧“已售罄 / Sold Out”等文案，命中后不再滑动。
- `AutoStockSwipeValleyIV`：在四号谷地页内向下滑动，`post_wait_freezes` 等待列表区域稳定后再进入下一轮识别。

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

- `AutoStockStapleGoodsCountValidateColor`：数量区域颜色锚点。
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
4. 若 `target <= 0`，跳过滑动。
5. 否则通过 `OverridePipeline` 将 `target` 写入 `AutoStockStapleBetterSliding.attach.Target`，并 `RunTask` 执行 BetterSliding 平滑调节购买数量。

`AutoStockStapleBetterSliding` 定义在 `General/Item.json`，使用 `BetterSliding` 向右滑到指定数量；`attach.Target` 的默认值只是占位，运行时会被 Custom 动作覆盖。

购买数量调节完成后，`next` 进入 `AutoStockStapleQuantityControlConfirmBuy` 点击黄色确认按钮，再关闭奖励弹窗回到列表。

### 分支 3：数量已达到或超过目标，排除并重新初始化

`AutoStockStapleQuantityControl{Item}Exclude` 在 `ExcludeValidate` 成立时命中（当前持有数量 **不低于** 用户上限）。

流程：

1. `{Item}RemoveFilter`：调用 `PipelineOverrideAction`，将该物品 attach 键设为 `false`，等效于从白名单移除。
2. `AutoStockStapleQuantityControlResetRecognitionParams`：再次执行 `AttachToExpectedRegexAction`，基于最新 attach 状态 **重新生成** `AutoStockInStapleItemName.expected` 正则。
3. 关闭购买弹窗，回到列表继续扫描其他物品。

Exclude 分支 **不会** 购买，仅把“已达标”的物品从本轮扫描目标中剔除。

## 初始化与 Override 机制小结

本任务有两类运行时 override，维护时不要混淆：

| 动作                             | 触发位置                                           | 作用                                           |
| -------------------------------- | -------------------------------------------------- | ---------------------------------------------- |
| `AttachToExpectedRegexAction`    | `AutoStockStapleMain` 入口；Exclude 后 Reset 节点  | 合并 attach 关键词 → OCR 白名单正则            |
| `PipelineOverrideAction`         | 各物品 `{Item}RemoveFilter`                        | 将指定 attach 键设为 `false`，排除该物品       |
| `AutoStockStapleQuantityControlAction` | 各物品 `{Item}Buy`                           | 计算差值并 override BetterSliding 的 `Target`  |

`attach` 语义（见 `attachregex/action.go`）：

- `string` / `string[]`：加入白名单关键词。
- `false`：显式排除该 attach 键，不再参与合并。
- `true`：当前实现下不追加关键词。

## 新增物品

新增一种稳定需求物资时，通常需要同步修改：

1. **`tools/pipeline-generate/AutoStockStaple/General/data.mjs`**：添加物品 `id`、`slug`、各语言 `expected`。
2. **重新生成**（在仓库根目录）：

```bash
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-count-validate-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/quantity-control-config.json
```

3. **`assets/tasks/AutoStockStaple.json`**：在对应地区 checkbox 中增加 case，写入 `AutoStockInStapleItemName.attach.{slug}` 与数量上限 override。
4. **`assets/locales/interface/*.json`**：补充选项与 focus 文案（如 `quantity_control.buy.*`）。
5. 确认 `AutoStockStapleQuantityControl.next` 列表中的物品顺序与 `data.mjs` 一致，避免生成后遍历顺序变化。

生成规则详见 [`tools/pipeline-generate/AutoStockStaple/General/README.md`](../../../../tools/pipeline-generate/AutoStockStaple/General/README.md)。

## 新增地区（参考四号谷地）

若将来增加第三个地区的稳定物资购买，可对照四号谷地复制一套节点：

1. 新建 `assets/resource/pipeline/AutoStockStaple/{Region}.json`：
    - `{Region}InStaple` 扫描循环（`next` 四分支结构不变）。
    - `AutoStockTargetCompare{Region}` / `AutoStockTargetCanNotBuy{Region}`。
    - `AutoStockBuyItem{Region}Task`（替换折扣节点名）。
    - `AutoStockSwipe{Region}`。
2. 在 `Main.json` 增加 `[JumpBack]AutoStockStaple{Region}` 子任务与 SceneManager 跳转。
3. 在 `StockStaple.json` 或地区 InScene 文件中补充场景 OCR。
4. 在 `Item.json` 增加 `AutoStockInStapleItemDiscounts{Region}`（若 UI 布局与现有地区不同）。
5. 在 `assets/tasks/AutoStockStaple.json` 增加地区 switch 及选项组。

武陵现有实现即为四号谷地的镜像，可直接 diff `ValleyIV.json` 与 `Wuling.json` 查看差异点。

## 调试建议

| 现象                         | 优先检查                                                                 |
| ---------------------------- | ------------------------------------------------------------------------ |
| 列表中识别不到目标商品       | `go-service.log` 中 `AttachToExpectedRegexAction` 的 `expected` 正则；锚点 `AutoStockInStapleItem` 是否命中 |
| 识别到商品但未购买           | 数量控制是否走 `Exclude` 或 `StockBillInsufficient`；`maafw*.log` 中 `AutoStockStapleQuantityControl{Item}Buy/Exclude` |
| 购买数量不对                 | `AutoStockStapleQuantityControlAction` 日志中的 `threshold/current_count/target`；BetterSliding ROI |
| 调度券明明够却停止扫描       | `AutoStockTargetCompareValleyIV` 表达式与用户输入的 `{ReserveValleyIV}`   |
| 反复点击同一已达标商品       | Exclude 后 `{Item}RemoveFilter` 与 `ResetRecognitionParams` 是否执行   |

日志分析可参考 skill：`.claude/skills/autostockstaple-log-analysis/SKILL.md`。

## 与 AutoStockpile 的区别

| 项目         | AutoStockStaple（稳定需求物资）     | AutoStockpile（弹性需求物资囤货）   |
| ------------ | ----------------------------------- | ----------------------------------- |
| 决策主体     | Pipeline + 少量 Go Custom           | Go Service 主导识别与决策           |
| 商品定位     | 列表页剩余时间锚点 + OCR 偏移链     | 模板匹配 + OCR 映射                 |
| 数量控制     | 弹窗内 BetterSliding + 表达式验证   | Go 侧解析详情页并调节数量           |
| 维护文档     | 本文                                | [auto-stockpile-maintain.md](./auto-stockpile-maintain.md) |

两者都进入“物资调度”界面，但购买逻辑完全独立，排查问题时不要混用日志分析流程。
