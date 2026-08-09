# IconRecognition 物品图标识别

`IconRecognition` 是 C++ Custom Recognition，用于在已知游戏界面和 ROI 内识别物品图标。它只负责定位和分类，不负责进入界面、点击物品或控制流程。

调用方提供 1280x720 基准下的 Maa ROI `[x,y,width,height]`。截图或识别前，建议先将鼠标移动到不会遮挡物品网格的位置（例如左上角），再等待目标区域画面稳定。

## Pipeline 调用

三种识别方式共享注册名 `IconRecognition` 和同一套参数。

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

Go 的 `CustomRecognitionParam` 与 Pipeline 使用相同的 `grid_type`、`item_ids`、`item_filters`、阈值和去重语义；只有原生 ROI 改由 `CustomRecognitionParam.ROI` 提供。

## C++ API 调用

C++ 可直接构造 `IconRecognizer`，不经过 Maa Custom Recognition 注册。`data_root` 指向 `assets/data/IconRecognition`，请求参数集中在 `RecognitionRequest`：

```cpp
iconrecognition::IconRecognizer recognizer("assets/data/IconRecognition");
if (!recognizer.initialize()) {
    return;
}

iconrecognition::RecognitionRequest request;
request.grid_type = iconrecognition::GridType::Transfer;
request.roi = cv::Rect(154, 202, 983, 291);
request.candidates.item_ids = { "item_copper_ore" };
request.deduplicate = true;

// 并发识别前可选预热；不调用时会在识别过程中按需准备模板。
if (!recognizer.preload({ request })) {
    return;
}

const iconrecognition::RecognitionResult result = recognizer.recognize(image, request);
```

C++ 的 `request.candidates.item_ids` / `item_filters` 对应 Pipeline 和 Go 的同名参数；`RecognitionResult` 与 Custom detail 使用同一结果字段语义，但以 C++ 结构体而不是 JSON 包装返回。

## 通用参数与返回值

三个调用入口共用同一识别语义。Pipeline 将原生 ROI 写在 `recognition.param.roi`，Go 使用 `CustomRecognitionParam.ROI`，C++ 使用 `RecognitionRequest.roi`；其余参数、默认值、`storageKind:categoryType` 分类、阈值约束、结果字段、错误码和 debug 输出统一见[参数与数据契约](/agent/cpp-algo/source/IconRecognition/docs/architecture.md)。

## 测试与实现

- [测试截图、运行命令和人工审核图](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [识别算法](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [内部网格类型与参考 ROI](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [资源下载与发布](/tools/icon_recognition/README.md)
