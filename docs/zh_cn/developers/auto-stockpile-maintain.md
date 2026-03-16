# 开发手册 - AutoStockpile 维护文档

本文说明 `AutoStockpile`（自动囤货）的商品模板、商品映射、价格阈值与地区扩展应如何维护。

当前实现由两部分组成：

- Pipeline 负责进入界面、切换地区、执行购买流程。
- `agent/go-service/autostockpile/` 负责识别商品、读取配置并决定买什么。

## 概览

AutoStockpile 的核心维护点如下：

| 模块                | 路径                                               | 作用                           |
| ------------------- | -------------------------------------------------- | ------------------------------ |
| 商品名称映射        | `agent/go-service/autostockpile/item_map.json`     | 将 OCR 商品名映射到内部商品 ID |
| 商品模板图          | `assets/resource/image/AutoStockpile/Goods/`       | 商品详情页模板匹配用图         |
| 地区与价格选项      | `assets/tasks/AutoStockpile.json`                  | 用户可配置的地区开关与价格阈值 |
| 地区入口 Pipeline   | `assets/resource/pipeline/AutoStockpile/Main.json` | 定义各地区子任务入口           |
| 囤货主流程 Pipeline | `assets/resource/pipeline/AutoStockpile/Task.json` | 执行识别、点击、购买等流程     |
| Go 识别/决策逻辑    | `agent/go-service/autostockpile/`                  | 读取模板、识别商品、应用阈值   |
| 多语言文案          | `assets/misc/locales/*.json`                       | AutoStockpile 任务与选项文案   |

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

### 地区 ID

当前仓库内已使用的地区 ID：

| 中文名   | Region     |
| -------- | ---------- |
| 四号谷地 | `ValleyIV` |
| 武陵     | `Wuling`   |

### 价值变动幅度 ID

当前任务配置中已使用的档位：

| 界面文本 | Tier ID |
| -------- | ------- |
| 适中     | `Tier1` |
| 较大     | `Tier2` |
| 极大     | `Tier3` |

> [!NOTE]
>
> `agent/go-service/autostockpile` 会在注册阶段固定初始化 `InitItemMap("zh_cn")`，因此商品名映射至少要维护 `zh_cn` 项，不能只加其他语言。

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

示例：

```json
{
    "zh_cn": {
        "源石树幼苗货组": "ValleyIV/OriginiumSaplings.Tier3"
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

示例：

```text
assets/resource/image/AutoStockpile/Goods/ValleyIV/OriginiumSaplings.Tier3.png
```

注意：

- 图片命名必须与 `item_map.json` 中的商品 ID 完全对应。
- 基准分辨率仍然是 **1280×720**。
- 文件名中的 `BaseName` 不要再额外包含 `.`，否则会干扰 Go 代码按 `BaseName.TierN.png` 的解析。

### 3. 是否需要改 Pipeline

**普通新增商品通常不需要改 Pipeline。**

原因是：

- Go 会自动扫描 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下的模板。
- 选择商品时会根据商品 ID 动态覆盖 `AutoStockpileSelectedGoodsClick` 等节点的模板路径。

也就是说，只要商品映射和模板图正确，已有流程就能识别并点击新商品。

## 添加价值变动幅度

如果只是给现有商品补一个新档位（例如某商品新增 `Tier3`），通常按“添加商品”的方式维护即可：

- 在 `item_map.json` 中新增对应的 `{BaseName}.Tier{N}` 映射。
- 在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下新增对应模板图。

如果是要让**整个任务界面**支持一个新的通用档位（例如新增 `Tier4`），则还需要继续维护以下内容。

### 1. 补充任务输入项

文件：`assets/tasks/AutoStockpile.json`

需要在对应地区的价格输入项中新增：

- 输入框定义；
- `pipeline_override.attach` 中的键；
- 键名格式必须为：`price_limits_{Region}.Tier{N}`。

例如：

```json
"price_limits_ValleyIV.Tier4": "{ValleyIVTier4PriceLimit}"
```

### 2. 补充默认价格

文件：`agent/go-service/autostockpile/options.go`

在 `autoStockpileDefaultPriceLimits` 中补上对应默认值，否则输入为空字符串时无法回退到默认阈值。

### 3. 补充多语言文案

文件：`assets/misc/locales/zh_cn.json` 以及其他语言文件。

至少需要补：

- 该地区价格配置项的 label；
- 新档位输入框的 label。

### 4. 关于未配置阈值的行为

`selector.go` 中 `resolveTierThreshold()` 会优先读取 `cfg.PriceLimits[tierID]`。

如果新档位没有配置专属阈值，则会退回 `FallbackThreshold`；而当前地区的 `FallbackThreshold` 又会取该地区已配置价格中的最小正值。也就是说：

- **能跑，不一定合理；**
- 想让新档位按预期购买，最好显式补上对应 `price_limits_{Region}.Tier{N}`。

## 添加地区

新增地区不是只加一个目录，而是要同时打通**地区识别、地区入口、价格配置、商品模板、文案**。

### 1. 添加商品模板目录与商品映射

先建立目录：

```text
assets/resource/image/AutoStockpile/Goods/{NewRegion}/
```

然后：

- 把该地区每个商品的模板图放进去；
- 在 `agent/go-service/autostockpile/item_map.json` 中加入对应商品映射。

### 2. 添加任务入口与价格配置

文件：`assets/tasks/AutoStockpile.json`

需要新增一整组地区配置，通常包括：

- `AutoStockpile{NewRegion}` 地区开关；
- `AutoStockpile{NewRegion}PriceLimits` 价格输入项；
- 对应的 `pipeline_override`；
- 每个档位的 `price_limits_{NewRegion}.Tier{N}`。

### 3. 添加 Pipeline 地区节点

文件：`assets/resource/pipeline/AutoStockpile/Main.json`

需要：

- 在 `AutoStockpileMain` 的 `sub` 列表中加入新地区子任务；
- 新增一个地区节点；
- 为该地区节点配置 anchor。

示意：

```json
"AutoStockpileNewRegion": {
    "enabled": false,
    "anchor": {
        "AutoStockpileGotoTargetRegion": "GoToNewRegion"
    },
    "next": ["AutoStockpileTask"]
}
```

### 4. 修改 Go 的地区解析逻辑

文件：`agent/go-service/autostockpile/recognition.go`

`resolveGoodsRegion()` 当前只识别：

- `GoToValleyIV` -> `ValleyIV`
- `GoToWuling` -> `Wuling`

新增地区时，必须在这里补上新的 anchor 分支，否则会回退到 `Wuling`。

### 5. 补默认价格

文件：`agent/go-service/autostockpile/options.go`

为新地区的每个档位添加默认 `price_limits_{NewRegion}.Tier{N}`。

### 6. 补多语言文案

文件：`assets/misc/locales/*.json`

至少需要补：

- 地区开关名；
- 地区价格配置名；
- 各档位输入框文案。

## 自检清单

改完后至少检查以下几项：

1. `item_map.json` 中的 value 是否是 `{Region}/{BaseName}.Tier{N}`，且与图片文件名一致。
2. 模板图是否放在 `assets/resource/image/AutoStockpile/Goods/{Region}/` 下。
3. `assets/tasks/AutoStockpile.json` 中的键名是否为 `price_limits_{Region}.Tier{N}`。
4. 新增地区时，`Main.json`、`recognition.go`、`options.go`、`assets/misc/locales/*.json` 是否同步修改。

## 常见坑

- **只加图片，不加 `item_map.json`**：OCR 名称无法映射到商品 ID，识别结果不完整。
- **只加 `item_map.json`，不加图片**：能匹配到名称，但无法完成模板点击。
- **新增地区但没改 `resolveGoodsRegion()`**：运行时会错误回退到 `Wuling`。
- **新增档位但没配阈值**：虽然流程可能继续执行，但购买阈值会退回 fallback，不一定符合预期。
- **文件名里带额外 `.`**：会影响商品名与 `Tier` 的解析。
