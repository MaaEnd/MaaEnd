# 开发手册 - AutoStockpile 维护文档

本文说明 `AutoStockpile` 如何在版本更新时进行维护  
该文档更新于 2026 年 11 月 14 日。

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

| 中文名 | Region ID |
| -------- | ---------- |
| 四号谷地 | `ValleyIV` |
| 武陵 | `Wuling` |

| 档位 | 价格波动幅度 |
| ------- | ------------ |
| `Tier1` | 适中 |
| `Tier2` | 较大 |
| `Tier3` | 极大 |

价格阈值只由 `strategy.go` 的「地区基准 + 档位基准 + 星期调整」公式产出，已无 JSON 配置入口；需要调整阈值时只能修改该公式。

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

在弹性需求物资的商品列表里截取该商品的图片，保存到对应目录：

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

新增 `AutoStockpileDecision{NewRegion}` 节点，在 `action.param.custom_action_param` 中写入 `"Region": "{NewRegion}"`；若新地区详情页的现金图标与现有的不同，还要把新模板加入 `AutoStockpileEnsureCashAnchor` 的 `template`。

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

| 字段键 | 用途 |
| ----------------------------------------------------------------- | -------------- |
| `global.region.{NewRegion}` | 地区全局显示名 |
| `task.AutoStockpile.option.AutoStockpileElastic{NewRegion}.label` | 任务选项标签 |

需补充的语言文件：`zh_cn.json`、`en_us.json`、`ja_jp.json`、`ko_kr.json`、`zh_tw.json`。

### 7. 更新价格记录 Schema

文件：`docs/zh_cn/protocol/autostockpile-daily-storage/daily_storage.schema.json`（以及对应 `en_us` 版本）

在 `region` 字段的 `enum` 列表中加入新地区标识（如 `"NewRegion"`），确保第三方工具能通过 Schema 校验新增地区的数据。

## 货架逐屏扫描

识别阶段会对弹性货架做**逐屏扫描**，滑到货架不再变化为止，不依赖具体屏数：

1. 对首屏截图执行 OCR + 模板匹配。
2. 调用 `AutoStockpileSwipeShelfDown` 下滑一段，再截图与上一屏比较；画面没有变化说明已经到底（或当前控制器本来就不需要滑动，如 Win32），结束扫描。
3. 画面有变化则扫描该屏，随后重复第 2 步。单次下滑距离远小于识别区高度，任意一行商品都会在至少一屏里完整出现，不会跳行。
4. 按商品 **ID 去重合并**（同 ID 保留首屏结果；仅第 N 屏（0-based）才出现的 ID 记入 `SwipeCountToReveal`，值为需要下滑的次数）。
5. 复原到扫描起始位置：按实际下滑次数调用 `AutoStockpileSwipeShelfUp` 反向拖动；上下滑使用等距的慢速拖动（`duration`），惯性可忽略，次数抵消即可回到原位。

逐屏扫描最多下滑 10 次（`shelfMaxScrollSteps`），仅作为防死循环的兜底；正常情况由「画面不再变化」终止。货架继续变长时不需要改这段逻辑，只需确认 ADB 侧 `AutoStockpileSwipeShelfDown` 的单次滚动量仍明显小于识别区高度。

复原只要求「差不多的位置」：点击前还会按 `SwipeCountToReveal` 补滑，且点击识别区域覆盖整个货架，几行以内的偏差不影响命中；因此不使用 OCR 锚点做像素级校准。

下滑、截图或某一屏扫描失败时降级为已扫到的结果，不中断任务；复原失败只记录警告，仍使用合并结果。

选中商品后：

- 若商品在 `SwipeCountToReveal` 中（首屏看不到），点击前会按记录的次数重复执行 `AutoStockpileSwipeShelfDown`，再交给 `AutoStockpileSelectedGoodsClick` 做模板点击。
- 若首屏（page0）已有该商品，则不额外滑动。

滑动节点：

- Win32 默认：`assets/resource/pipeline/AutoStockpile/Helper.json` 中 `AutoStockpileSwipeShelfDown` / `AutoStockpileSwipeShelfUp` 均为 `DoNothing`（一屏可看全），逐屏扫描第一次比较就会判定「没有滚动」并结束。
- ADB / PlayCover：`assets/resource_adb/pipeline/AutoStockpile/Helper.json` 覆盖为真实 `Swipe`：上下滑为等距的慢速拖动（200px、`duration: 700`、`end_hold: 200`），尽量减少惯性带来的位移误差。坐标基于 720p，实机若漏扫或复原偏差偏大，优先微调 ADB 侧 `begin` / `end` / `duration` 与 `post_wait_freezes.target`。
