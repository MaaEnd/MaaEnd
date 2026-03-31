# 开发手册 - AutoStockpile 维护文档

本文说明 `AutoStockpile`（自动囤货）的商品模板、商品映射、任务选项（全局开关 / 地区阈值 / 保留调度券）与地区扩展应如何维护。

当前实现由两部分协作组成：

- `assets/resource/pipeline/AutoStockpile/` 负责进入界面、切换地区、执行购买流程，并在 `Helper.json` 中维护识别节点默认参数。
- `agent/go-service/autostockpile/` 负责运行时覆盖识别节点参数、解析识别结果并决定买什么。

## 概览

AutoStockpile 的核心维护点如下：

| 模块              | 路径                                                       | 作用                                                 |
| ----------------- | ---------------------------------------------------------- | ---------------------------------------------------- |
| 商品名称映射      | `agent/go-service/autostockpile/item_map.json`             | 将 OCR 商品名映射到内部商品 ID                       |
| 商品模板图        | `assets/resource/image/AutoStockpile/Goods/`               | 商品详情页模板匹配用图                               |
| 任务选项          | `assets/tasks/AutoStockpile.json`                          | 用户可配置的全局开关、地区开关、价格阈值与保留调度券 |
| 地区入口 Pipeline | `assets/resource/pipeline/AutoStockpile/Main.json`         | 定义各地区子任务入口与锚点映射                       |
| 囤货入口 Pipeline | `assets/resource/pipeline/AutoStockpile/Entry.json`        | 进入弹性物资调度界面并滑动至底部                     |
| 决策循环 Pipeline | `assets/resource/pipeline/AutoStockpile/DecisionLoop.json` | 执行识别、决策、复核、跳过等核心流程                 |
| 购买流程 Pipeline | `assets/resource/pipeline/AutoStockpile/Purchase.json`     | 执行购买数量调整、购买、取消等操作                   |
| 识别节点默认配置  | `assets/resource/pipeline/AutoStockpile/Helper.json`       | 溢出检测、商品 OCR、模板匹配等识别节点的默认参数     |
| Go 识别/决策逻辑  | `agent/go-service/autostockpile/`                          | 运行时覆盖识别节点、解析结果、应用阈值               |
| 多语言文案        | `assets/locales/interface/*.json`                          | AutoStockpile 任务与选项文案                         |

## 命名规则

### 商品 ID

`item_map.json` 中保存的不是图片路径，而是**内部商品 ID**，格式固定为：

```text
{Region}/{BaseName}.Tier{N}
```

例如：

```text
ValleyIV/OriginiumSaplings.Tier3
Wuling/WulingFrozenPears.Tier1
```

其中：

1. `Region`：地区 ID。
2. `BaseName`：英文文件名主体。
3. `Tier{N}`：价值变动幅度。

### 模板图片路径

Go 代码会根据商品 ID 自动拼出模板路径：

```text
AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

仓库中的实际文件位置为：

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

### 地区与价格选项

当前仓库内已使用的地区与档位：

| 中文名   | Region ID  | 包含档位                  |
| -------- | ---------- | ------------------------- |
| 四号谷地 | `ValleyIV` | `Tier1`, `Tier2`, `Tier3` |
| 武陵     | `Wuling`   | `Tier1`, `Tier2`          |

> [!NOTE]
>
> `agent/go-service/autostockpile` 会在注册阶段调用 `InitItemMap("zh_cn")`。初始化失败仅记录警告日志，不会阻止服务启动。但若后续需要解析商品名称或验证地区时 `item_map` 仍不可用，相关操作会失败。商品映射文件 `item_map.json` 已嵌入二进制中。

### 当前任务选项与 attach 键

当前 `assets/tasks/AutoStockpile.json` 中，任务选项通过 `pipeline_override` 写入 `AutoStockpileDecisionAttach` 节点的 `attach` 字段，供 Go 服务统一读取。实际写入的键如下：

| 作用               | 任务选项 / 输入项                             | attach 键                         |
| ------------------ | --------------------------------------------- | --------------------------------- |
| 溢出时放宽阈值     | `AutoStockpileOverflowBuyLowPriceGoods`       | `overflow_mode=true`              |
| 周日放宽阈值       | `AutoStockpileBuyAllGoodsOnSunday`            | `sunday_mode=true`                |
| 四号谷地价格阈值   | `ValleyIVTier1PriceLimit` / `Tier2` / `Tier3` | `price_limits_ValleyIV.Tier1/2/3` |
| 四号谷地保留调度券 | `ValleyIVReserveStockBillAmount`              | `reserve_stock_bill_ValleyIV`     |
| 武陵价格阈值       | `WulingTier1PriceLimit` / `Tier2`             | `price_limits_Wuling.Tier1/2`     |
| 武陵保留调度券     | `WulingReserveStockBillAmount`                | `reserve_stock_bill_Wuling`       |

`AutoStockpileValleyIV`、`AutoStockpileWuling` 两个地区开关本身不写入 `attach`，而是通过 `pipeline_override.enabled` 分别控制 `Main.json` 中对应地区节点是否启用。

## 阈值解析机制

系统按以下优先级决定购买阈值：

1. **显式地区档位阈值**：读取任务选项中配置的 `price_limits_{Region}.Tier{N}`。
2. **地区最小正阈值**：若未配置当前档位阈值，则取该地区所有已配置价格中的最小正值。
3. **attach 中的 fallback_threshold**：若存在且为正整数，使用该值。当前任务选项未暴露此配置，但 Go 解析逻辑支持从 attach 中读取。
4. **全局默认值**：若上述均不可用，回退至 `defaultFallbackBuyThreshold` (800)。

默认的按档位阈值表（如 `ValleyIVTier1` 对应 800）维护在 `agent/go-service/autostockpile/thresholds.go` 中，而非 `options.go`。

> [!TIP]
>
> 阈值相关输入与 attach 值都必须是**正整数**。空字符串、`0`、负值不会触发"回退"，而是会被任务输入校验或 Go 解析逻辑直接判为无效配置。

## 保留调度券 (Stock Bill)

AutoStockpile 支持保留一定数量的调度券。

- **输入单位**：用户在界面输入的值单位为"万"（如输入 60 表示 60万）。
- **解析逻辑**：Go 代码解析 `reserve_stock_bill_{Region}` 选项，将其数值乘以 10000 得到实际保留额度。若结果超过 `math.MaxInt/10000` 会报错。
- **前置依赖**：保留调度券功能仅在对应地区启用保留开关时生效。启用后必须能正确识别当前调度券余额（OCR）。若 OCR 失败，或余额小于等于保留额度，识别阶段会提前结束并跳过购买流程。
- **购买限制**：若当前调度券余额扣除保留额度后不足以购买目标商品，将限制购买数量或跳过。

## 运行时覆盖行为

Go Service 在运行时会动态覆盖 Pipeline 节点的参数：

- **AutoStockpileLocateGoods**：覆盖 `template` 列表与 `roi`。
- **AutoStockpileGetGoods**：覆盖识别 `roi`。
- **AutoStockpileSelectedGoodsClick**：覆盖 `template`、ROI 的 `y` 坐标以及 `enabled` 状态。
- **AutoStockpileRelayNodeDecisionReady**：覆盖 `enabled` 状态。
- **AutoStockpileSwipeSpecificQuantity**：覆盖 `Quantity.Target` 数值与 `enabled` 状态。
- **AutoStockpileSwipeMax**：覆盖 `enabled` 状态。

当决策未找到合格商品或需要跳过时，Go 会重置购买分支相关节点（`AutoStockpileRelayNodeDecisionReady`、`AutoStockpileSelectedGoodsClick`、`AutoStockpileSwipeSpecificQuantity`、`AutoStockpileSwipeMax`）的启用状态（全部设为 `enabled: false`），并通过 `OverrideNext` 将流程导向跳过分支。

## 添加商品

添加新商品时，至少需要维护**商品映射**和**模板图片**两部分。

### 1. 修改 `item_map.json`

文件：`agent/go-service/autostockpile/item_map.json`

在 `zh_cn` 下新增商品名称到商品 ID 的映射：

```json
{
    "zh_cn": {
        "{商品中文名}": "{Region}/{BaseName}.Tier{N}"
    }
}
```

注意：

- value 里**不要**写 `AutoStockpile/Goods/` 前缀。
- value 里**不要**写 `.png` 后缀。
- 商品中文名要与 OCR 能稳定识别到的名称尽量一致。

### 2. 添加模板图片

将商品详情页截图保存到对应目录：

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

注意：

- 图片命名必须与 `item_map.json` 中的商品 ID 完全对应。
- 基准分辨率为 **1280×720**。
- 文件名中的 `BaseName` 不要再额外包含 `.`，否则会干扰解析。

### 3. 是否需要修改 Pipeline

**普通新增商品通常不需要修改 Pipeline。**

当前识别流程会先尝试用 OCR 商品名绑定价格。只有当前地区中仍未绑定成功的商品 ID，才会继续通过 `BuildTemplatePath()` 拼出的模板做补充匹配。运行时 Go 会覆盖相关识别节点的模板与 ROI，因此通常只需要补齐 `item_map.json` 和模板图。

## 添加价值变动幅度

如果只是给现有商品补一个新档位（例如某商品新增 `Tier3`），通常按"添加商品"的方式维护即可：

- 在 `item_map.json` 中新增对应的 `{BaseName}.Tier{N}` 映射。
- 在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下新增对应模板图。

如果是要让某个地区的任务配置支持一个新的通用档位（例如给 `Wuling` 增加 `Tier3` 输入项），还需要继续维护以下内容：

1. 在 `assets/tasks/AutoStockpile.json` 中补充对应地区的 `price_limits_{Region}.Tier{N}` 输入与 `pipeline_override.attach` 键。
2. 在 `agent/go-service/autostockpile/thresholds.go` 的 `autoStockpileDefaultPriceLimits` 中补充该档位默认值。
3. 在 `assets/locales/interface/*.json` 中补充新档位的 label / description。

如果新档位没有配置专属阈值，运行时会按"当前地区最小正阈值 -> `fallback_threshold`（如有）-> `defaultFallbackBuyThreshold` (800)"的顺序回退。流程可以继续，但购买结果不一定符合预期。

---

## 添加地区

新增地区需要同步打通多个环节：

### 1. 准备资源

- 建立 `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` 目录并放入模板。
- 在 `agent/go-service/autostockpile/item_map.json` 中加入映射。

### 2. 配置任务入口

文件：`assets/tasks/AutoStockpile.json`

- 新增 `AutoStockpile{NewRegion}` 开关。
- 新增 `price_limits_{NewRegion}.Tier{N}` 对应的价格输入项与 `pipeline_override.attach` 键。
- 如需支持保留调度券，增加对应的开关 / 输入项，以及 `reserve_stock_bill_{NewRegion}` attach 键。

### 3. Pipeline 节点

文件：`assets/resource/pipeline/AutoStockpile/Main.json`、`assets/resource/pipeline/AutoStockpile/DecisionLoop.json`

- 在 `Main.json` 的 `AutoStockpileMain` 的 `next` 列表中加入 `[JumpBack]AutoStockpile{NewRegion}`。
- 在 `Main.json` 中定义对应的地区节点（如 `AutoStockpileValleyIV`），设置 `anchor` 字段将 `AutoStockpileDecision` 指向 `DecisionLoop.json` 中对应的决策节点（如 `AutoStockpileDecisionValleyIV`）。
- 在 `DecisionLoop.json` 中新增对应的 `AutoStockpileDecision{NewRegion}` 节点，并在其 `action.param.custom_action_param.Region` 中写入 `{NewRegion}`。

注意：Pipeline 仍通过 `Main.json` 中的 `anchor` 字段硬编码维护地区到决策节点的映射关系。

### 4. Go 逻辑

文件：`agent/go-service/autostockpile/params.go`

- Go 会直接从 `AutoStockpileDecision{Region}` 节点的 `custom_action_param.Region` 读取地区，并校验该值是否存在于 `item_map.json` 中。
- `normalizeCustomActionParam()` 支持接收 map 或 JSON 字符串格式的参数。
- **注意**：此处没有回退逻辑。`Region` 缺失、为空或未出现在 `item_map.json` 中，都会直接导致识别/任务报错。

### 5. 补充默认值

文件：`agent/go-service/autostockpile/thresholds.go`

- 在 `autoStockpileDefaultPriceLimits` 中为新地区各档位补齐默认价格。

### 6. 国际化

- 在 `assets/locales/interface/` 下补齐所有新增选项的 label 和 description。

## 自检清单

改完后至少检查以下几项：

1. `item_map.json` 中的 value 是否是 `{Region}/{BaseName}.Tier{N}`，且与图片文件名一致。
2. 模板图是否放在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下。
3. `assets/tasks/AutoStockpile.json` 中的键名是否为 `price_limits_{Region}.Tier{N}`。若启用保留调度券，对应 `reserve_stock_bill_{Region}` 是否也已补齐。
4. 新增档位时，`agent/go-service/autostockpile/thresholds.go` 与 `assets/locales/interface/*.json` 是否同步修改。
5. 新增地区时，`Main.json`、`DecisionLoop.json`（尤其是 `AutoStockpileDecision{Region}.action.param.custom_action_param.Region`）、`assets/tasks/AutoStockpile.json`、`item_map.json`、`thresholds.go`、`assets/locales/interface/*.json` 是否同步修改。

## 常见坑

- **只加图片，不加 `item_map.json`**：OCR 名称无法映射到商品 ID，识别结果不完整。
- **只加 `item_map.json`，不加图片**：能匹配到名称，但无法完成模板点击。
- **新增地区但没在 `DecisionLoop.json` 的 `AutoStockpileDecision{Region}` 节点设置 `custom_action_param.Region`**：运行时会因地区缺失或非法直接报错并中止识别/任务。
- **新增档位但没配阈值**：虽然流程可能继续执行，但购买阈值会退回 fallback，不一定符合预期。
- **新增地区但漏配 `reserve_stock_bill_{Region}`**：价格阈值可以独立工作，但该地区无法通过任务选项启用"保留调度券"。
- **文件名里带额外 `.`**：会影响商品名与 `Tier` 的解析。
