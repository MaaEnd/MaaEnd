# 开发手册 - AutoStockpile 维护文档

本文说明 `AutoStockpile` 如何在版本更新时进行维护

## 概念说明

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
2. `BaseName`：英文文件名主体。可在[EndFieldTranslationReferrer](https://susieglitter.github.io/EndFieldTranslationReferrer/)中找到对应名称
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

| 中文名   | Region ID  |
| -------- | ---------- |
| 四号谷地 | `ValleyIV` |
| 武陵     | `Wuling`   |

| 档位    | 价格波动幅度 |
| ------- | ------------ |
| `Tier1` | 适中         |
| `Tier2` | 较大         |
| `Tier3` | 极大         |

## 添加商品

添加新商品时，需要维护**商品映射**和**模板图片**两部分。

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

### 2. 添加模板图片

将商品详情页截图保存到对应目录：

```text
assets/resource/image/AutoStockpile/Goods/{Region}/{BaseName}.Tier{N}.png
```

> [!important]
> 模板图片需基于1280×720分辨率

## 添加地区

新增地区需要同步修改以下文件：

### 1. 准备资源

- 建立 `assets/resource/image/AutoStockpile/Goods/{NewRegion}/` 目录并放入模板图片。
- 在 `agent/go-service/autostockpile/item_map.json` 中加入新地区的商品名称到商品 ID 的映射。

### 2. 配置任务选项

文件：`assets/tasks/AutoStockpile.json`

在 `option` 列表中新增 `AutoStockpileElastic{NewRegion}` 开关项，通过 `pipeline_override.enabled` 控制 `Main.json` 中对应地区节点是否启用。模板如下：

```json
"AutoStockpileElastic{NewRegion}": {
    "type": "switch",
    "label": "$task.AutoStockpile.option.AutoStockpileElastic{NewRegion}.label",
    "default_case": "Yes",
    "cases": [
        {
            "name": "Yes",
            "pipeline_override": {
                "AutoStockpileElastic{NewRegion}": {
                    "enabled": true
                }
            }
        },
        {
            "name": "No"
        }
    ]
}
```

同时在 `task[0].option` 数组中加入 `"AutoStockpileElastic{NewRegion}"`。

### 3. 确认场景导航入口存在

通常被命名为 `SceneEnterMenuRegionalDevelopment{Region}StockRedistribution`。

如果不存在，可等待其他维护者提供相应节点，或者自行补充，此处不再赘述。

### 4. Pipeline 节点

#### Main.json

文件：`assets/resource/pipeline/AutoStockpile/Main.json`

1. 在 `AutoStockpileStart` 的 `next` 列表中加入 `"[JumpBack]AutoStockpileElastic{NewRegion}"`。
2. 新增 `AutoStockpileElastic{NewRegion}` 节点，参照 `AutoStockpileElasticValleyIV` 的结构：
    - `action.param.custom_action_param.sub` 中填入对应的场景导航子任务名称。
    - `anchor` 设置 `"AutoStockpileDecision": "AutoStockpileDecision{NewRegion}"`。
    - `focus` 中填入地区中文名。

#### DecisionLoop.json

文件：`assets/resource/pipeline/AutoStockpile/DecisionLoop.json`

新增 `AutoStockpileDecision{NewRegion}` 节点，在 `action.param.custom_action_param` 中写入 `"Region": "{NewRegion}"`。

### 5. Go 逻辑

文件：`agent/go-service/autostockpile/strategy.go`

在 `regionBases` 中补充新地区及其基础值：

```go
var regionBases = map[string]int{
    "ValleyIV":   regionBaseValleyIV,
    "Wuling":     regionBaseWuling,
    "{NewRegion}": regionBase{NewRegion},  // 新增
}
```

确认共享的 `tierBases` 已覆盖该地区需要支持的档位。

### 6. 国际化

在所有语言的 `assets/locales/interface/*.json` 中补充以下字段：

| 字段键                                                            | 用途           |
| ----------------------------------------------------------------- | -------------- |
| `global.region.{NewRegion}`                                       | 地区全局显示名 |
| `task.AutoStockpile.option.AutoStockpileElastic{NewRegion}.label` | 任务选项标签   |

需补充的语言文件：`zh_cn.json`、`en_us.json`、`ja_jp.json`、`ko_kr.json`、`zh_tw.json`。

### 7. 更新价格记录 Schema

文件：`docs/zh_cn/protocol/autostockpile-daily-storage/daily_storage.schema.json`（以及对应 `en_us` 版本）

在 `region` 字段的 `enum` 列表中加入新地区标识（如 `"NewRegion"`），确保第三方工具能通过 Schema 校验新增地区的数据。
