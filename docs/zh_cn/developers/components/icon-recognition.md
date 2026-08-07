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
                "roi": [155, 205, 970, 280]
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
                "roi": [155, 205, 970, 280]
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

原生 `roi` 写在 `recognition.param` 中，与 `custom_recognition` 同级；`custom_recognition_param` 必须包含 `grid_type`。其中可选参数包括 `item_ids`、`item_filters`、`threshold`、`subpixel_threshold`、`deduplicate` 和 `debug`。

`item_filters` 的全部分类、各 `grid_type` 默认候选、阈值约束和 item ID 获取方法见[接口与数据契约](/agent/cpp-algo/source/IconRecognition/docs/architecture.md)。

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
            ROI:                maa.NewTargetRect(maa.Rect{155, 205, 970, 280}),
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
