# IconRecognition 物品图标识别

`IconRecognition` 是 C++ Custom Recognition，用于在已知游戏界面和 ROI 内识别物品图标。它只负责定位和分类，不负责进入界面、点击物品或控制流程。

调用方提供 1280x720 基准下的 Maa ROI `[x,y,width,height]`。截图或识别前，建议先将鼠标移动到不会遮挡物品网格的位置（例如左上角），再等待目标区域画面稳定。

## 三种用法

三种用法共享注册名 `IconRecognition` 和同一套参数。

### 按物品 ID 查找位置

`item_ids` 使用 `assets/data/IconRecognition/recognition_items.json` 的顶层 key：

```json
{
    "IconRecognitionFindItem": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "transfer",
                    "item_ids": ["item_copper_ore"],
                    "deduplicate": true
                },
                "roi": [154, 202, 983, 291]
            }
        },
        "action": "DoNothing"
    }
}
```

默认返回该物品在每个命中 cell 中的位置。设置 `deduplicate: true` 后，同一个 `item_id` 只保留分数最高的 cell；不同物品仍各自保留结果。

### 识别网格内所有物品

不传 `item_ids`，组件先在 ROI 内定位该界面网格，再逐格识别：

对于 `transfer`（背包和仓库）与 `port_storager`（便捷存取站），ROI 可以覆盖左右两侧，也可以只覆盖其中一侧。整块 ROI 会识别两侧；单侧 ROI 只识别该侧，实际流程通常按当前操作目标分别传入左右侧 ROI。

**性能提示：识别耗时会随候选模板数量增加。调用方应优先使用 `item_filters` 按分类缩小物品集，已知目标物品时再用 `item_ids` 精确限定；不要在单个界面中使用不必要的全量候选集。**

```json
{
    "ScanTransferItems": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "transfer",
                    "item_filters": ["Normal:*"]
                },
                "roi": [154, 202, 983, 291]
            }
        },
        "action": "DoNothing"
    }
}
```

### 识别单个正方形 ROI

`single_roi` 跳过真实网格检测，直接把 ROI 构造成一个临时 cell。ROI 宽高必须相等，边长可以是任意正整数：

```json
{
    "RecognizeCurrentTradeItem": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "single_roi",
                    "item_filters": ["Normal:Product", "Normal:Usable"]
                },
                "roi": [1177, 450, 54, 54]
            }
        },
        "action": "DoNothing"
    }
}
```

54x54 只是该界面的示例。组件会将内置的 128 或 256 原图缩放到请求 ROI 边长，调用方不需要提供图标资源。

## 参数与返回值

原生 `roi` 写在 `recognition.param` 中，与 `custom_recognition` 同级，使用 1280x720 基准下的 Maa `[x,y,width,height]`；`custom_recognition_param` 必须包含 `grid_type`。

| 参数 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `grid_type` | 是 | 无 | 当前界面：`trade` 据点交易、`transfer` 背包和仓库、`port_storager` 便捷存取站、`valuables` 贵重品库、`shipment` 送货、`credit_trade` 信用交易所、`single_roi` 临时单格 |
| `item_ids` | 否 | 空 | 限定为 catalog 顶层物品 ID；不得重复；与 `item_filters` 同时提供时取交集 |
| `item_filters` | 否 | 由 `grid_type` 决定 | 使用“库:分类”，多个条件取并集，`*` 表示该库的全部分类 |
| `threshold` | 否 | `0.85` | 最终接受阈值；降低会增加误识别风险 |
| `subpixel_threshold` | 否 | `0.60` | 基础分达到该值但低于 `threshold` 时执行亚像素精排；低于该值直接拒识 |
| `deduplicate` | 否 | `false` | 是否只保留每个 `item_id` 分数最高的 cell |
| `debug` | 否 | `false` | 是否保存原图、标注图和 detail 到 `exe_dir/../debug/vision/IconRecognition`，只保留最近 20 组 |

`item_filters` 的完整库、分类取值、业务含义和各 `grid_type` 默认候选见[接口与数据契约中的分类表](/agent/cpp-algo/source/IconRecognition/docs/architecture.md#item_filters-分类)。

基础模板分使用带 mask 的 `TM_CCOEFF_NORMED`，最终 `score` 为模板分的 85% 与 Lab 颜色分的 15% 之和。阈值必须满足 `0 <= subpixel_threshold < threshold <= 1`。识别不稳定时应先确认 ROI、画面稳定性和候选分类是否正确，再考虑调整阈值。完整错误码和结果契约见[接口与数据契约](/agent/cpp-algo/source/IconRecognition/docs/architecture.md)。

命中时 `out_box` 等于最高优先级结果的 `cell_box`。组件 detail 包含：

- `detail_version`、`matched`、`grid_type`、`roi`；
- `matches[]` 中的 `item_id`、多语言 `name` key、分类、稀有度、`cell_box`、`item_box`、`score`；
- 真实网格结果另含 `row`、`column`；
- 失败时的 `error.code`、`error.message`。

## Go Service 调用

Go Service 通过 `ctx.RunRecognitionDirect` 调用注册名 `IconRecognition`，原生 ROI 设置在 `CustomRecognitionParam.ROI`。MaaFramework 的 `DetailJson` 是外层包装，组件结果位于 `best.detail`：

```go
import (
    "encoding/json"
    "image"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type iconMatch struct {
    ItemID  string  `json:"item_id"`
    Name    string  `json:"name"`
    Score   float64 `json:"score"`
    CellBox struct {
        X      int `json:"x"`
        Y      int `json:"y"`
        Width  int `json:"width"`
        Height int `json:"height"`
    } `json:"cell_box"`
}

type iconDetail struct {
    Matched bool        `json:"matched"`
    Matches []iconMatch `json:"matches"`
}

func recognizeIcons(ctx *maa.Context, img image.Image) (iconDetail, error) {
    detail, err := ctx.RunRecognitionDirect(
        maa.RecognitionTypeCustom,
        &maa.CustomRecognitionParam{
            ROI:                maa.NewTargetRect(maa.Rect{154, 202, 983, 291}),
            CustomRecognition: "IconRecognition",
            CustomRecognitionParam: map[string]any{
                "grid_type":  "transfer",
                "item_ids":   []string{"item_copper_ore"},
                "deduplicate": true,
            },
        },
        img,
    )
    if err != nil {
        return iconDetail{}, err
    }

    raw := json.RawMessage(detail.DetailJson)
    var wrapped struct {
        Best struct {
            Detail json.RawMessage `json:"detail"`
        } `json:"best"`
    }
    if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Best.Detail) > 0 {
        raw = wrapped.Best.Detail
    }

    var result iconDetail
    if err := json.Unmarshal(raw, &result); err != nil {
        return iconDetail{}, err
    }
    return result, nil
}
```

## 测试与实现

- [测试截图、运行命令和人工审核图](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [识别算法](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [内部网格类型与参考 ROI](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [资源下载与发布](/tools/icon_recognition/README.md)
