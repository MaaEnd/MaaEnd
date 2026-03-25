# 开发手册 - AutoStockpile 维护文档

本文说明 `AutoStockpile`（自动囤货）的商品模板、商品映射、价格阈值与地区扩展应如何维护。

当前实现由两部分协作组成：

- `assets/resource/pipeline/AutoStockpile/` 负责进入界面、切换地区、执行购买流程，并在 `Helper.json` 中维护识别节点默认参数。
- `agent/go-service/autostockpile/` 负责运行时覆盖识别节点参数、解析识别结果并决定买什么。

## 概览

AutoStockpile 的核心维护点如下：

| 模块                | 路径                                                 | 作用                                             |
| ------------------- | ---------------------------------------------------- | ------------------------------------------------ |
| 商品名称映射        | `agent/go-service/autostockpile/item_map.json`       | 将 OCR 商品名映射到内部商品 ID                   |
| 商品模板图          | `assets/resource/image/AutoStockpile/Goods/`         | 商品详情页模板匹配用图                           |
| 地区与价格选项      | `assets/tasks/AutoStockpile.json`                    | 用户可配置的地区开关、价格阈值与保留调度券       |
| 地区入口 Pipeline   | `assets/resource/pipeline/AutoStockpile/Main.json`   | 定义各地区子任务入口                             |
| 囤货主流程 Pipeline | `assets/resource/pipeline/AutoStockpile/Task.json`   | 执行识别、点击、购买等流程                       |
| 识别节点默认配置    | `assets/resource/pipeline/AutoStockpile/Helper.json` | 溢出检测、商品 OCR、模板匹配等识别节点的默认参数 |
| Go 识别/决策逻辑    | `agent/go-service/autostockpile/`                    | 运行时覆盖识别节点、解析结果、应用阈值           |
| 多语言文案          | `assets/locales/interface/*.json`                    | AutoStockpile 任务与选项文案                     |

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
> `agent/go-service/autostockpile` 会在注册阶段初始化 `InitItemMap("zh_cn")` 与 `InitAbortReasonCatalog()`。商品映射目前必须包含 `zh_cn` 项，其资源文件 `item_map.json` 已嵌入二进制中。

## 阈值解析机制

系统按以下优先级决定购买阈值：

1. **显式地区阈值**：读取任务选项中配置的 `price_limits_{Region}.Tier{N}`。
2. **地区回退阈值**：若未配置当前档位阈值，则取该地区所有已配置价格中的最小正值。
3. **全局默认值**：若上述均不可用，回退至 `defaultFallbackBuyThreshold` (800)。

默认的按档位阈值表（如 `ValleyIVTier1` 对应 800）维护在 `agent/go-service/autostockpile/thresholds.go` 中，而非 `options.go`。

## 保留调度券 (Stock Bill)

AutoStockpile 支持保留一定数量的调度券。

- **输入单位**：用户在界面输入的值单位为“万”（如输入 60 表示 60万）。
- **解析逻辑**：Go 代码解析 `reserve_stock_bill_{Region}` 选项，将其数值乘以 10000 得到实际保留额度。
- **购买限制**：若当前调度券余额扣除保留额度后不足以购买目标商品，将限制购买数量或跳过。

## 运行时覆盖行为

Go Service 在运行时会动态覆盖 Pipeline 节点的参数，不仅限于模板：

- **AutoStockpileLocateGoods**：覆盖 `template` 列表与 `roi`。
- **AutoStockpileSelectedGoodsClick**：覆盖 `template`、`roi` 的 `y` 坐标以及 `enabled` 状态。
- **AutoStockpileSwipeSpecificQuantity**：覆盖 `Target` 数值与 `enabled` 状态。
- **AutoStockpileGetGoods**：覆盖识别 `roi`。

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

当前识别流程会先尝试用 OCR 商品名绑定价格；只有当前地区中仍未绑定成功的商品 ID，才会继续通过 `BuildTemplatePath()` 拼出的模板做补充匹配。运行时 Go 会覆盖相关识别节点的模板与 ROI，因此通常只需要补齐 `item_map.json` 和模板图。

## 添加价值变动幅度

如果只是给现有商品补一个新档位（例如某商品新增 `Tier3`），通常按“添加商品”的方式维护即可：

- 在 `item_map.json` 中新增对应的 `{BaseName}.Tier{N}` 映射。
- 在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下新增对应模板图。

如果是要让某个地区的任务配置支持一个新的通用档位（例如给 `Wuling` 增加 `Tier3` 输入项），还需要继续维护以下内容：

1. 在 `assets/tasks/AutoStockpile.json` 中补充对应地区的 `price_limits_{Region}.Tier{N}` 输入与 `pipeline_override.attach` 键。
2. 在 `agent/go-service/autostockpile/thresholds.go` 的 `autoStockpileDefaultPriceLimits` 中补充该档位默认值。
3. 在 `assets/locales/interface/*.json` 中补充新档位的 label / description。

如果新档位没有配置专属阈值，运行时会按“当前地区最小正阈值 -> `defaultFallbackBuyThreshold` (800)”的顺序回退；流程可以继续，但购买结果不一定符合预期。

---

## 添加地区

新增地区需要同步打通多个环节：

### 1. 准备资源

- 建立 `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` 目录并放入模板。
- 在 `agent/go-service/autostockpile/item_map.json` 中加入映射。

### 2. 配置任务入口

文件：`assets/tasks/AutoStockpile.json`

- 新增 `AutoStockpile{NewRegion}` 开关。
- 新增 `price_limits_{NewRegion}.Tier{N}` 价格输入项。
- 如果需要支持保留调度券，需增加 `reserve_stock_bill_{NewRegion}` 选项。

### 3. Pipeline 节点

文件：`assets/resource/pipeline/AutoStockpile/Main.json`

- 在 `AutoStockpileMain` 的 `sub` 列表加入新地区。
- 定义对应的地区节点。

### 4. Go 逻辑注册

文件：`agent/go-service/autostockpile/recognition.go`

- 在 `resolveGoodsRegion()` 中补上对应的 anchor 分支（如 `GoToNewRegion` -> `NewRegion`）。
- **注意**：此处不设回退逻辑，无法识别的 anchor 将直接导致错误。

### 5. 补充默认值

文件：`agent/go-service/autostockpile/thresholds.go`

- 在 `autoStockpileDefaultPriceLimits` 中为新地区各档位补齐默认价格。

### 6. 国际化

- 在 `assets/locales/interface/` 下补齐所有新增选项的 label 和 description。

## 自检清单

改完后至少检查以下几项：

1. `item_map.json` 中的 value 是否是 `{Region}/{BaseName}.Tier{N}`，且与图片文件名一致。
2. 模板图是否放在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下。
3. `assets/tasks/AutoStockpile.json` 中的键名是否为 `price_limits_{Region}.Tier{N}`；若启用保留调度券，对应 `reserve_stock_bill_{Region}` 是否也已补齐。
4. 新增档位时，`agent/go-service/autostockpile/thresholds.go` 与 `assets/locales/interface/*.json` 是否同步修改。
5. 新增地区时，`Main.json`、`recognition.go`、`assets/tasks/AutoStockpile.json`、`assets/locales/interface/*.json` 是否同步修改。

## 常见坑

- **只加图片，不加 `item_map.json`**：OCR 名称无法映射到商品 ID，识别结果不完整。
- **只加 `item_map.json`，不加图片**：能匹配到名称，但无法完成模板点击。
- **新增地区但没改 `resolveGoodsRegion()`**：运行时会因未知 anchor 直接报错并中止识别/任务。
- **新增档位但没配阈值**：虽然流程可能继续执行，但购买阈值会退回 fallback，不一定符合预期。
- **新增地区但漏配 `reserve_stock_bill_{Region}`**：价格阈值可以独立工作，但该地区无法通过任务选项启用“保留调度券”。
- **文件名里带额外 `.`**：会影响商品名与 `Tier` 的解析。
