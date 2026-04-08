# 开发手册 - AutoStockpile 维护文档

本文说明 `AutoStockpile`（自动囤货）的商品模板、商品映射、任务选项（地区开关）与地区扩展应如何维护。

当前实现由两部分协作组成：

- `assets/resource/pipeline/AutoStockpile/` 负责进入界面、切换地区、执行购买流程，并在 `Helper.json` 中维护识别节点默认参数。
- `agent/go-service/autostockpile/` 负责运行时覆盖识别节点参数、解析识别结果并决定买什么。

## 概览

AutoStockpile 的核心维护点如下：

| 模块              | 路径                                                       | 作用                                             |
| ----------------- | ---------------------------------------------------------- | ------------------------------------------------ |
| 商品名称映射      | `agent/go-service/autostockpile/item_map.json`             | 将 OCR 商品名映射到内部商品 ID                   |
| 商品模板图        | `assets/resource/image/AutoStockpile/Goods/`               | 商品详情页模板匹配用图                           |
| 任务选项          | `assets/tasks/AutoStockpile.json`                          | 用户可配置的地区开关（四号谷地 / 武陵）          |
| 地区入口 Pipeline | `assets/resource/pipeline/AutoStockpile/Main.json`         | 定义各地区子任务入口与锚点映射                   |
| 囤货入口 Pipeline | `assets/resource/pipeline/AutoStockpile/Entry.json`        | 进入物资调度界面（选购弹性需求物资）并滑动至底部 |
| 决策循环 Pipeline | `assets/resource/pipeline/AutoStockpile/DecisionLoop.json` | 执行识别、决策、复核、跳过等核心流程             |
| 购买流程 Pipeline | `assets/resource/pipeline/AutoStockpile/Purchase.json`     | 执行购买数量调整、购买、取消等操作               |
| 识别节点默认配置  | `assets/resource/pipeline/AutoStockpile/Helper.json`       | 溢出检测、商品 OCR、模板匹配等识别节点的默认参数 |
| Go 识别/决策逻辑  | `agent/go-service/autostockpile/`                          | 运行时覆盖识别节点、解析结果、应用阈值           |
| 多语言文案        | `assets/locales/interface/*.json`                          | AutoStockpile 任务与选项文案                     |

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

### 当前任务选项

当前 `assets/tasks/AutoStockpile.json` 中，任务选项仅有两个地区开关：

| 任务选项                | 作用                                              |
| ----------------------- | ------------------------------------------------- |
| `AutoStockpileValleyIV` | 通过 `pipeline_override.enabled` 启用四号谷地节点 |
| `AutoStockpileWuling`   | 通过 `pipeline_override.enabled` 启用武陵节点     |

地区开关不写入 `attach`。Go Service 当前使用以下内建行为：

- **溢出时放宽阈值**：仅当识别结果中的 `Quota.Overflow > 0` 时，`selector.go` 才会自动放宽阈值；当前没有用户配置项，也没有 attach 覆盖入口。
- **价格阈值**：默认值由 `strategy.go` 中的 `buildSelectionConfig()` 按 `region_base + tier_base` 公式生成；当前任务选项和 attach 都不会覆盖它。
- **保留调度券**：当前未作为运行时决策输入实现。识别结果只传递配额与商品数据，下游决策流程也不会消费任何保留调度券状态。

如果需要调整价格策略，请直接修改 Go 代码中的默认值，而不是写手动 `attach` 覆盖。当前 AutoStockpile 流程不会读取基于 attach 的价格阈值、溢出开关或保留调度券配置。

## 阈值解析机制

系统当前使用**严格的地区-档位查表**来决定购买阈值：

1. **`strategy.go` 生成的地区-档位默认值**：`buildPriceLimitsForRegion()` 按 `region_base + tier_base` 公式生成各档位阈值。
2. **`thresholds.go` 严格命中 `price_limits`**：`resolveTierThreshold()` 会直接使用 `GoodsItem.Tier` 作为 key 查表；key 缺失、为空或阈值非法都会返回错误，并由上游按 fatal 语义中止流程。

当前生成出的默认值包括：`ValleyIV.Tier1=600`、`ValleyIV.Tier2=900`、`ValleyIV.Tier3=1200`、`Wuling.Tier1=1200`、`Wuling.Tier2=1500`。

## 运行时覆盖行为

Go Service 在运行时会动态覆盖 Pipeline 节点的参数：

- **AutoStockpileLocateGoods**：覆盖 `template` 列表与 `roi`。
- **AutoStockpileGetGoods**：覆盖识别 `roi`。
- **AutoStockpileSelectedGoodsClick**：覆盖 `template`、ROI 的 `y` 坐标以及 `enabled` 状态。
- **AutoStockpileRelayNodeDecisionReady**：覆盖 `enabled` 状态。
- **AutoStockpileSwipeSpecificQuantity**：覆盖 `Target` 数值与 `enabled` 状态。
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

如果是要让某个地区支持新的通用档位，需继续维护以下内容：

1. 在 `agent/go-service/autostockpile/strategy.go` 的 `tierBases` 中补充该档位的基础值。

如果新档位没有在 `tierBases` 中配置基础值，`buildPriceLimitsForRegion()` 就不会生成对应 key；后续一旦识别到该档位，`resolveTierThreshold()` 会因为缺少精确的 `{Region}.Tier{N}` 配置而直接报错并按 fatal 终止。

---

## 添加地区

新增地区需要同步打通多个环节：

### 1. 准备资源

- 建立 `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` 目录并放入模板。
- 在 `agent/go-service/autostockpile/item_map.json` 中加入映射。

### 2. 配置任务入口

文件：`assets/tasks/AutoStockpile.json`

- 新增 `AutoStockpile{NewRegion}` 开关，通过 `pipeline_override.enabled` 控制 `Main.json` 中对应地区节点是否启用。

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

文件：`agent/go-service/autostockpile/strategy.go`

- 在 `regionBases` 中补充新地区。
- 确认共享的 `tierBases` 已覆盖该地区需要支持的档位。

### 6. 国际化

- 在 `assets/locales/interface/` 下补齐所有新增选项的 label 和 description。

## 自检清单

改完后至少检查以下几项：

1. `item_map.json` 中的 value 是否是 `{Region}/{BaseName}.Tier{N}`，且与图片文件名一致。
2. 模板图是否放在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下。
3. 新增档位时，`agent/go-service/autostockpile/strategy.go` 的 `tierBases` 是否补充了对应基础值。
4. 新增地区时，`Main.json`、`DecisionLoop.json`（尤其是 `AutoStockpileDecision{Region}.action.param.custom_action_param.Region`）、`assets/tasks/AutoStockpile.json`、`item_map.json`、`strategy.go`、`assets/locales/interface/*.json` 是否同步修改。

## 常见坑

- **只加图片，不加 `item_map.json`**：OCR 名称无法映射到商品 ID，识别结果不完整。
- **只加 `item_map.json`，不加图片**：能匹配到名称，但无法完成模板点击。
- **新增地区但没在 `DecisionLoop.json` 的 `AutoStockpileDecision{Region}` 节点设置 `custom_action_param.Region`**：运行时会因地区缺失或非法直接报错并中止识别/任务。
- **新增档位或地区但没在 `strategy.go` 补默认阈值输入**：运行时不会为缺失档位生成对应的 `{Region}.Tier{N}` key；一旦识别到该档位，严格查表会直接失败，并按 fatal 语义中止流程。
- **文件名里带额外 `.`**：会影响商品名与 `Tier` 的解析。
