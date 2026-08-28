# IconRecognition 物品图标识别

`IconRecognition` 是 C++ Custom Recognition，用于在已知游戏界面和 ROI 内识别物品图标。它只负责定位和分类，不负责进入界面、点击物品或控制流程。

调用方提供 1280x720 基准下的 Maa ROI `[x,y,width,height]`。截图或识别前，建议先将鼠标移动到不会遮挡物品网格的位置（例如左上角），再等待目标区域画面稳定。

## Pipeline 调用

Pipeline 使用 `Custom` 识别，注册名固定为 `IconRecognition`。原生 `roi` 写在 `recognition.param.roi`，组件参数写在 `recognition.param.custom_recognition_param`；不要把 `roi` 放进组件参数。

### 按物品 ID 查找位置

`item_ids` 使用 [`assets/data/IconRecognition/recognition_items.json`](/assets/data/IconRecognition/recognition_items.json) 的顶层 key：

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
                    "item_filters": ["Normal:Ore"],
                    "item_recheck_filters": ["Normal:Ore"],
                    "deduplicate": true
                },
                "roi": [
                    154,
                    202,
                    983,
                    291
                ]
            }
        },
        "action": "DoNothing"
    }
}
```

默认返回该物品在每个命中格子中的位置。设置 `deduplicate: true` 后，同一个 `item_id` 只保留分数最高的格子；不同物品仍各自保留结果。

### 识别网格内所有物品

不传 `item_ids` 时，组件先在 ROI 内定位该界面的物品网格，再逐格识别。识别耗时会随候选模板数量增加，应优先使用 `item_filters` 缩小物品集。

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
                "roi": [
                    154,
                    202,
                    983,
                    291
                ]
            }
        },
        "action": "DoNothing"
    }
}
```

`transfer`（背包和仓库）与 `port_storager`（便捷存取站）支持完整双侧 ROI 和单侧 ROI。完整 ROI 会识别左右两侧；单侧 ROI 只识别对应一侧。实际流程通常按当前操作目标分别传入左右侧 ROI。

### 识别单个正方形 ROI

`single_roi` 跳过真实网格检测，直接把 ROI 构造成一个临时格子。ROI 宽高必须相等，边长可以是任意正整数：

```json
{
    "RecognizeCurrentTradeItem": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "IconRecognition",
                "custom_recognition_param": {
                    "grid_type": "single_roi",
                    "item_filters": [
                        "Normal:Product",
                        "Normal:Usable"
                    ]
                },
                "roi": [
                    1177,
                    450,
                    54,
                    54
                ]
            }
        },
        "action": "DoNothing"
    }
}
```

54x54 只是该界面的示例。组件会将内置图标缩放到请求 ROI 的边长，调用方不需要准备图标资源。

## 参数参考

Pipeline、Go Service 和 C++ API 使用同一套识别语义，只是字段承载位置不同：

| 内容 | Pipeline | Go Service | C++ API |
| -------- | -------------------------- | ----------------------------------------------- | ------------------------------------------------------- |
| 原生 ROI | `recognition.param.roi` | `CustomRecognitionParam.ROI` | `RecognitionRequest.roi` |
| 组件参数 | `custom_recognition_param` | `CustomRecognitionParam.CustomRecognitionParam` | `RecognitionRequest`；候选字段位于 `request.candidates` |
| 注册名 | `IconRecognition` | `IconRecognition` | 直接构造 `IconRecognizer`，无需注册名 |

原生 ROI 采用 1280x720 基准下的 `[x,y,width,height]` 语义，宽高必须为正且区域必须完全位于图片内。`single_roi` 还要求宽高相等。

### 支持的控制器

`IconRecognition` 当前复用以下两套 1280x720 控制器 profile：

| 运行时 `type` | 画面要求 | 网格 profile |
| --- | --- | --- |
| Win32、Linux、MacOS | 1280x720 | 标准 720p UI |
| Adb、PlayCover | 1280x720、240 dpi | ADB 放大 UI |

Custom 入口会从 `MaaContext` 读取运行时 `type` 并选择对应 profile；CloudADB 的运行时 `type` 是 `Adb`。除 Win32/Adb 外，上表中的兼容映射暂没有独立截图数据验证，后续可能根据实际画面调整。其他控制器、直接 C++ 调用或上下文不可用时从请求 ROI 的图像证据推断。证据不足时返回 `exception`，调用方不能指定比例。检测器只在内存中临时归一化画面以定位网格，返回前会把格子坐标映射回原图；物品模板仍按原图格子尺寸生成并在原图上匹配，因此 `cell_box`、`item_box` 始终是原图坐标。`single_roi` 不执行网格检测，也不会缩放输入图。

组件不会根据 controller profile 猜测、移动或扩大请求 ROI。调用方始终负责传入完全位于图片内、且完整覆盖目标格子的原生 ROI；图像回退只读取该 ROI 内的像素。奖励界面应继续传入覆盖整组奖励的大 ROI，控制器类型和物品数量都不应由调用方用于改写 ROI。

### custom_recognition_param

| 字段 | 类型 | 必选 | 默认值 | 说明 |
| -------------------- | ------------------- | ------------------------- | ------------------- | ------------------------------------------------------------------------------------------- |
| `grid_type` | string / `GridType` | Custom 是；C++ 应显式设置 | Custom 无 | 选择当前界面的网格定位策略，合法值见下表；C++ 结构体中的初始值只用于构造占位 |
| `item_ids` | string[] | 否 | `[]` | 只保留指定物品；多个 ID 取并集，重复值按一次处理 |
| `item_filters` | string[] | 否 | 由 `grid_type` 决定 | 按 `storageKind:categoryType` 选择候选；多个条件取并集，`*` 匹配该 `storageKind` 下全部分类 |
| `additional_item_filters` | string[] | 否 | `[]` | 在基础过滤器与 `item_ids` 取交集后，额外并入这些分类 |
| `excluded_item_ids` | string[] | 否 | `[]` | 从候选集合中排除指定物品 ID |
| `item_recheck_filters` | string[] | 否 | `[]` | 与 `item_ids` 均非空时生效；格式与 `item_filters` 相同；用于对已命中的候选格进行单格复核 |
| `recognize_region_unavailable` | boolean | 否 | `false` | 仅在背包/仓库（`transfer`）和储存站（`port_storager`）场景生效。开启后，格子的常规识别未命中时，会继续尝试识别因当前地区限制而无法使用的物品。帝江号不会出现这种状态，因此无需开启 |
| `threshold` | number | 否 | `0.85` | 物品命中的最低最终分数，所有网格类型统一按该值判断 |
| `subpixel_threshold` | number | 否 | `0.60` | 基础分达到该值但低于 `threshold` 时，在图标附近尝试更细的位置偏移 |
| `deduplicate` | boolean | 否 | `false` | 同一个 `item_id` 命中多个格子时，只保留分数最高的一项 |
| `debug` | boolean | 否 | `false` | 正常执行到结果汇总阶段时会收集网格和格子诊断；提前返回的 `invalid_image` 或 `exception` 可能不包含诊断。debug 控制性能计时和 Custom debug 文件写入 |

- **`threshold` 与 `subpixel_threshold`**：阈值必须满足 `0 <= subpixel_threshold < threshold <= 1`。基础分低于 `subpixel_threshold` 时，组件认为当前候选明显不可靠，不再尝试更细的位置偏移，也不会把它放入 `matches`。基础分位于两个阈值之间时，组件会继续细化位置；只有最终分达到 `threshold`，并且没有被低纹理检查拒绝，结果才会返回。送货界面的数量条和贵重品库的头像区域会从模板匹配遮罩中排除，但不会绕过统一阈值。调整阈值前应先检查 ROI、画面稳定性和候选分类。
- **候选集合顺序**：最终候选为 `(((item_filters 或界面默认值) ∩ item_ids) ∪ additional_item_filters) - excluded_item_ids`；未提供 `item_ids` 时跳过交集。`excluded_item_ids` 在候选集合计算的最后一步应用，因此其中的物品最终不会参与识别。各候选数组中的重复值均按一次处理；`item_ids` / `excluded_item_ids` 中的未知 ID、格式错误或无法匹配 catalog 的 filter，以及最终空候选都会返回 `invalid_argument`。
- **`item_recheck_filters`**：使用 `item_ids` 查找指定物品时，范围外但外观相似的物品可能被误识别为目标物品。`item_recheck_filters` 只复核来自原始 `item_ids` 的命中，不复核 `additional_item_filters` 追加的物品。相比不传 `item_ids`、仅使用 `item_filters` 识别整个网格，这种方式只复核已命中的格子，通常更快。建议同时设置 `deduplicate: true`，避免重复复核同一物品。

当前地区不可用物品的模板合成、mask 与后备匹配流程见[识别算法](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md#候选选择与图标匹配)。

### grid_type、默认候选和参考 ROI

| `grid_type` | C++ `GridType` | 界面 | 默认 `item_filters` | Win32 参考 ROI | ADB 参考 ROI |
| --------------- | ------------------------ | ---------------- | ---------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `trade` | `GridType::Trade` | 据点交易 | `Normal:Product`、`Normal:Usable` | `[170,165,935,385]` | `[32,46,1216,549]` |
| `transfer` | `GridType::Transfer` | 背包和仓库 | `Normal:*` | 完整 `[154,202,983,291]`；左侧 `[154,202,585,291]`；右侧 `[739,202,398,291]` | 完整 `[30,160,1220,370]`；左侧 `[30,160,710,370]`；右侧 `[780,160,470,370]` |
| `port_storager` | `GridType::PortStorager` | 便捷存取站 | `Normal:*` | 完整 `[190,250,880,350]`；左侧 `[190,250,318,350]`；右侧 `[570,250,500,350]` | 完整 `[78,228,1150,410]`；左侧 `[78,228,368,410]`；右侧 `[562,326,620,293]` |
| `valuables` | `GridType::Valuables` | 贵重品库 | `ValuableDepot:*` | `[24,76,950,570]` | `[100,85,790,540]` |
| `shipment` | `GridType::Shipment` | 送货界面 | `Normal:*` | `[34,132,386,474]` | `[43,169,480,408]` |
| `credit_trade` | `GridType::CreditTrade` | 信用交易所 | `ValuableDepot:SpecialItem`、`Isolate:*` | `[70,95,1140,415]` | `[10,120,1250,510]` |
| `rewards` | `GridType::Rewards` | 奖励界面 | `Isolate:*`、`ValuableDepot:*` | `[39,82,1205,511]` | `[178,140,935,440]` |
| `single_roi` | `GridType::SingleRoi` | 调用方指定的单格 | `Normal:*` | 任意位于图片内的正方形；示例 `[1177,450,54,54]` | 任意位于图片内的正方形；示例 `[1151,393,66,66]` |

参考 ROI 均为 1280x720 绝对坐标，不是相对于其它 ROI 的局部坐标。它们只适用于表中的对应界面；调用方应确保 ROI 完整覆盖要识别的格子。仓库类界面传入单侧 ROI 时，仍然使用画面绝对坐标。

`rewards` 按白色奖励卡片定位，并要求整组网格大致居中。单行按实际物品数计算宽度；多行从首行观测推导列数，后续行复用首行左边界，只有末行可以不足一行。多个游戏功能共用这种界面，不同功能展示的物品分类可能不同；未传入 `item_filters` 或传入空数组时使用默认候选集 `Isolate:*`、`ValuableDepot:*`，传入非空 `item_filters` 时会完整替换默认候选集。

### 物品 ID

物品 ID 是 [`recognition_items.json`](/assets/data/IconRecognition/recognition_items.json) 的顶层 key，例如：

```json
{
    "item_copper_ore": {
        "name": "铜矿",
        "category": "矿物",
        "storageKind": "Normal",
        "categoryType": "Ore",
        "rarity": 1,
        "iconId": "item_copper_ore"
    }
}
```

调用时使用 `item_copper_ore`，不要使用 `iconId`、多语言 key 或显示名称。`iconId` 只用于定位发布图标文件。catalog 的 `name` 是下载时写入的中文名称，仅方便阅读；运行时 `matches[].name` 仍由 `item_id` 推导 locale key。仅地区受限物品额外包含 `regionRestricted: true`，普通物品不导出该字段。

### `item_filters` 分类

过滤器不会改变网格定位方式，只会缩小参与匹配的内置图标集合。格式为 `storageKind:categoryType`，两部分区分大小写，并直接对应 `recognition_items.json` 中的同名字段。

`storageKind` 的合法值：

| `storageKind` | 含义 |
| --------------- | -------- |
| `Normal` | 普通物品 |
| `ValuableDepot` | 贵重品库 |
| `Isolate` | 独立资源 |

各存储类别下的 `categoryType`：

| `storageKind` | `categoryType` | 含义 |
| --------------- | ---------------- | -------- |
| `Normal` | `Ore` | 矿物 |
| `Normal` | `Plant` | 植物 |
| `Normal` | `Product` | 产物 |
| `Normal` | `Doodad` | 采集材料 |
| `Normal` | `Nurturance` | 培养素材 |
| `Normal` | `Usable` | 可用道具 |
| `Normal` | `Producer` | 生产工具 |
| `Normal` | `PortableDevice` | 随身装置 |
| `ValuableDepot` | `Weapon` | 武器 |
| `ValuableDepot` | `CommercialItem` | 珍贵物品 |
| `ValuableDepot` | `SpecialItem` | 培养素材 |
| `Isolate` | `Gold` | 折金票 |
| `Isolate` | `Diamond` | 嵌晶玉 |
| `Isolate` | `WeaponGold` | 武库配额 |

通配写法如 `Normal:*`、`ValuableDepot:*`、`Isolate:*`。通配符只用于 `categoryType`，不支持 `*:Ore`。

## 返回值与 Pipeline 命中框

`RecognitionResult` 与 Custom detail 使用相同结构：

| 字段 | 类型 | 说明 |
| ---------------- | ------- | ---------------------------------------------- |
| `detail_version` | integer | detail 契约版本，当前为 `3` |
| `matched` | boolean | 是否至少有一个物品达到阈值并通过界面规则检查 |
| `grid_type` | string | 本次请求的网格类型；参数解析前失败时可能不存在 |
| `roi` | integer[4] | 请求 ROI，格式为 `[x,y,width,height]` |
| `matches` | array | 实际返回给调用方的物品，按分数和位置排序 |
| `error` | object | 失败时出现，包含稳定的 `code` 和可读 `message` |

`matches[]` 字段：

| 字段 | 类型 | 说明 |
| -------------------------------- | ------- | ----------------------------------------------------- |
| `item_id` | string | catalog 顶层物品 ID |
| `name` | string | 多语言 key，如 `iconRecognition.name.item_copper_ore` |
| `category` | string | catalog 中文分类标签 |
| `storage_kind` / `category_type` | string | 可用于后续过滤和业务判断的分类字段 |
| `rarity` | integer | catalog 稀有度 |
| `cell_box` | integer[4] | 所属格子，格式为 `[x,y,width,height]`；`single_roi` 时等于请求 ROI |
| `item_box` | integer[4] | 最终模板命中位置，格式为 `[x,y,width,height]` |
| `score` | number | 最终匹配分数 |
| `region_unavailable` | boolean | 当前物品是否在当前地区不可用；仅为 `true` 时返回，普通命中省略 |
| `row` / `column` | integer | 真实网格中的行列；`single_roi` 不返回 |

结果先按 `score` 降序排列；同分时依次比较 `cell_box.y`、`cell_box.x` 和 `item_id`。`deduplicate=true` 时，每个 `item_id` 只保留排序后的第一项。

`matches` 非空时，Custom 返回 `MAA_TRUE`，Pipeline 使用的识别框 `out_box` 等于 `matches[0].cell_box`；`matches` 为空时返回 `MAA_FALSE`。MaaFramework 会把回调 detail 包装到外层 `all/filtered/best` 中：命中时完整组件结果位于 `best.detail`；未命中时 `best` 为 `null`，组件结果保留在 `all[0].detail`。

### `error.code`

| `code` | 触发条件 | 调用方处理 |
| --------------- | -------------------------------------- | ------------------------------------------------- |
| `invalid_image` | 输入图片为空 | 检查截图或直接调用输入；请求未进入正常识别 |
| `exception` | 参数校验、资源加载、网格检测或匹配异常 | 可记录 `message` 排查，但不要依赖其文本做业务分支 |
| `no_match` | 识别正常完成，但没有找到足够可靠的物品 | 按“本次未找到物品”处理，不表示组件异常 |

三种情况均返回 `MAA_FALSE`，但含义不同。`invalid_image` 和 `exception` 表示请求没有正常完成；`no_match` 表示截图、参数解析、网格定位和逐格匹配均未发生异常，但所有已定位格子的候选分数都不够高，或候选被低纹理检查拒绝。网格检测无法生成正式格子时返回 `exception`，不属于 `no_match`。`no_match` 时 `matched=false`、`matches=[]`，调用方通常应按“目标物品当前不存在”继续业务流程，而不是把它当成组件故障。

`no_match` 会保留已解析的 `grid_type` 和 `roi`。解析期间发生的 `exception` 可能没有完整请求字段。

## 参考界面

> [!NOTE]
> 以下截图和图中 ROI 均来自 Win32 控制器，不适用于 ADB 控制器；ADB 请使用上表中的参考 ROI。

<details>
<summary>据点交易</summary>

![settlement-trade](https://github.com/user-attachments/assets/7e8623ad-61be-4415-add8-c1c2abf95390)

</details>

<details>
<summary>背包和仓库</summary>

![inventory-transfer](https://github.com/user-attachments/assets/bc1bbea5-5aa4-4421-9ccb-b3de8314688e)

</details>

<details>
<summary>便捷存取站</summary>

![port-storager](https://github.com/user-attachments/assets/f0fab5de-186d-40df-b212-7b8ae6714103)

</details>

<details>
<summary>贵重品库</summary>

![valuables](https://github.com/user-attachments/assets/4121c648-94b8-4032-8afd-3436cf31f99b)

</details>

<details>
<summary>送货界面</summary>

![shipment](https://github.com/user-attachments/assets/61eb016e-13f6-4e5d-80f1-c1d1eecb57d7)

</details>

<details>
<summary>信用交易所</summary>

![credit-trade](https://github.com/user-attachments/assets/c4415a7c-56d0-4aa2-bf2e-230bae21211d)

</details>

<details>
<summary>single_roi 示例</summary>

![specific-roi-example](https://github.com/user-attachments/assets/76e7f9d0-ed4e-4feb-b1b4-afbc40ac6003)

</details>

## Go Service 调用

Go Service 通过 `ctx.RunRecognitionDirect` 调用注册名 `IconRecognition`。原生 ROI 设置在 `CustomRecognitionParam.ROI`，其余字段放入 `CustomRecognitionParam.CustomRecognitionParam`：

MaaEnd Go Service 调用方应直接复用 `agent/go-service/pkg/iconrecognition`，不要在业务包中重复声明 IconRecognition 的参数或 detail JSON 结构。`Params` 对应 `custom_recognition_param`，`Detail`/`Match` 对应组件 detail，`Match.CellBox` 是可用于后续操作的格子坐标：

```go
import "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"

detail, err := ctx.RunRecognitionDirect(
    maa.RecognitionTypeCustom,
    &maa.CustomRecognitionParam{
        ROI:                maa.NewTargetRect(maa.Rect{154, 202, 983, 291}),
        CustomRecognition: iconrecognition.CustomRecognitionName,
        CustomRecognitionParam: iconrecognition.NewParams(
            iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
            iconrecognition.WithItemIDs("item_copper_ore"),
            iconrecognition.WithItemFilters(iconrecognition.StorageFilter().Normal.Ore),
            iconrecognition.WithAdditionalItemFilters(iconrecognition.StorageFilter().Normal.Product),
            iconrecognition.WithExcludedItemIDs("item_carbon_mtl"),
            iconrecognition.WithRecognizeRegionUnavailable(true),
            iconrecognition.WithDeduplicate(true),
        ),
    },
    img,
)

parsed, _, err := iconrecognition.ParseRecognitionDetail(detail)
if err != nil {
    return
}
for _, match := range parsed.Matches {
    _ = match.CellBox
    _ = match.RegionUnavailable
}
```

`iconrecognition.ParseRecognitionDetail` 负责从 Maa 结果中选择 Custom detail：命中时读取 `Results.Best`，未命中时读取 `Results.All[0]`，调用方无需自行合并或去重结果桶。若已经取得 `CustomRecognitionResult.Detail` 字符串，也可以直接使用 `iconrecognition.ParseDetail`。

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
request.candidates.additional_item_filters = { "Normal:Product" };
request.candidates.excluded_item_ids = { "item_carbon_mtl" };
request.recognize_region_unavailable = true;
request.deduplicate = true;

// 并发识别前可选预热；不调用时会在识别过程中按需准备模板。
if (!recognizer.preload({ request })) {
    return;
}

const iconrecognition::RecognitionResult result = recognizer.recognize(image, request);
```

`request.candidates` 中的 ID 与过滤器字段对应 Pipeline 和 Go 的同名参数；`request.recognize_region_unavailable` 控制当前地区不可用物品后备。C++ 直接返回 `RecognitionResult`，不包含 MaaFramework 的 JSON 外层包装。

## Debug 输出

`debug=true` 时，Custom 入口把本次识别保存到 `exe_dir/../debug/vision/IconRecognition`：

- `raw/<stem>.png`：输入原图；
- `annotated/<stem>.png`：ROI、格子、候选框和分数标注；
- `detail/<stem>.json`：公开结果和内部诊断。

三个文件使用相同 stem，合称一组。组件会自动清理较旧的组。正常执行到结果汇总阶段时，核心 C++ 结果保留网格和格子诊断；提前返回的 `invalid_image` 或 `exception` 可能不包含诊断。`debug=true` 时还会附加性能诊断。Custom 回调的公开 detail 不包含这些内部字段，只有 debug 文件和人工测试输出会附加它们。

## 测试与内部实现

- [测试截图、运行命令和人工审核图](/agent/cpp-algo/source/IconRecognition/docs/testing.md)
- [内部架构与维护边界](/agent/cpp-algo/source/IconRecognition/docs/architecture.md)
- [识别算法](/agent/cpp-algo/source/IconRecognition/docs/algorithm.md)
- [网格配置维护](/agent/cpp-algo/source/IconRecognition/docs/grid-profiles.md)
- [资源下载与发布](/tools/icon_recognition/README.md)
