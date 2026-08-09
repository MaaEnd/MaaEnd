# 接口与数据契约

## 统一 C++ API

`IconRecognition` 只公开一个识别方法。`grid_type` 决定如何取得待识别格子，`item_ids` 决定是否限制到指定物品。

```cpp
iconrecognition::IconRecognizer recognizer("assets/data/IconRecognition");
if (!recognizer.initialize()) {
    return;
}

iconrecognition::RecognitionRequest request;
request.grid_type = iconrecognition::GridType::Transfer;
request.roi = cv::Rect(154, 202, 983, 291); // Maa [x,y,width,height]
request.candidates.item_ids = { "item_copper_ore" };

const iconrecognition::RecognitionResult result = recognizer.recognize(image, request);
```

同一个 `recognize(image, request)` 支持三种用法：

| 用法 | 请求差异 | 返回 |
| --- | --- | --- |
| 按物品 ID 找位置 | 使用真实界面的 `grid_type`，设置 `item_ids` | 指定物品在网格中的所有接受结果 |
| 识别 ROI 内所有物品 | 使用真实界面的 `grid_type`，不设置 `item_ids` | 网格内所有接受结果 |
| 识别单个正方形 ROI | `grid_type=SingleRoi` | 跳过网格检测，把 ROI 构造成一个临时 cell，最多返回一项 |

`single_roi` 与其它类型共用候选选择、内置图标缩放、匹配、阈值和结果排序逻辑，仅省略真实网格检测及界面专用门控。

## Custom Recognition 参数

Maa 注册名为 `IconRecognition`。调用节点的原生 `roi` 写在 `recognition.param` 中，与 `custom_recognition` 同级；`custom_recognition_param` 必须是 JSON 对象并包含 `grid_type`。

| 字段 | 类型 | 必选 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `grid_type` | string | 是 | 无 | `trade`、`transfer`、`port_storager`、`valuables`、`shipment`、`credit_trade` 或 `single_roi` |
| `item_ids` | string[] | 否 | `[]` | 只保留指定物品候选；多个 ID 取并集，不能重复 |
| `item_filters` | string[] | 否 | 由 `grid_type` 决定 | `storageKind:categoryType`；多个条件取并集，`*` 匹配该 storage 下全部分类 |
| `threshold` | number | 否 | `0.85` | 最终接受阈值 |
| `subpixel_threshold` | number | 否 | `0.60` | 基础分达到该值但低于 `threshold` 时进行亚像素精排 |
| `deduplicate` | boolean | 否 | `false` | 同一个 `item_id` 在多个 cell 命中时只保留分数最高的一项；不同物品分别保留 |
| `debug` | boolean | 否 | `false` | Custom 入口保存原图、标注图和内部诊断 JSON |

原生 `roi` 使用 Maa `[x,y,width,height]`，基准分辨率为 1280x720，宽高必须为正；`single_roi` 还要求 ROI 宽高相等且完全位于图片内。阈值必须满足 `0 <= subpixel_threshold < threshold <= 1`。`item_ids` 与 `item_filters` 同时提供时取交集；ID 不存在或被过滤器排除会返回明确错误。

完整 Pipeline 参数示例：

```jsonc
{
    "recognition": {
        "type": "Custom",
        "param": {
            "roi": [154, 202, 983, 291],
            "custom_recognition": "IconRecognition",
            "custom_recognition_param": {
                "grid_type": "transfer",
                "item_ids": ["item_copper_ore"],
                "item_filters": ["Normal:Ore"],
                "threshold": 0.85,
                "subpixel_threshold": 0.6,
                "deduplicate": false,
                "debug": false
            }
        }
    }
}
```

### item ID 从哪里获取

物品 ID 是 [`assets/data/IconRecognition/recognition_items.json`](/assets/data/IconRecognition/recognition_items.json) 的顶层 key，例如：

```json
{
    "item_copper_ore": {
        "category": "矿物",
        "storageKind": "Normal",
        "categoryType": "Ore",
        "rarity": 1,
        "iconId": "item_copper_ore"
    }
}
```

调用时使用 `item_copper_ore`，不要使用 `iconId`、多语言 key 或显示名称。`iconId` 只用于定位发布图标文件。

### item_filters 分类

过滤器用于缩小内置图标候选集合，能减少计算量和相似图标误判；它不会改变网格位置或匹配算法。格式为 `storageKind:categoryType`。

#### 贵重品库（`ValuableDepot`）

| `categoryType` | 含义 |
| --- | --- |
| `Weapon` | 武器 |
| `CommercialItem` | 珍贵物品 |
| `SpecialItem` | 培养素材 |

#### 普通物品（`Normal`）

| `categoryType` | 含义 |
| --- | --- |
| `Ore` | 矿物 |
| `Plant` | 植物 |
| `Product` | 产物 |
| `Doodad` | 采集材料 |
| `Nurturance` | 培养素材 |
| `Usable` | 可用道具 |
| `Producer` | 生产工具 |
| `PortableDevice` | 随身装置 |

#### 货币（`Isolate`）

| `categoryType` | 含义 |
| --- | --- |
| `Gold` | 折金票 |
| `Diamond` | 嵌晶玉 |
| `WeaponGold` | 武库配额 |

通配写法如 `Normal:*`、`ValuableDepot:*`、`Isolate:*`。默认候选范围如下：

| `grid_type` | 默认过滤器 |
| --- | --- |
| `trade` | `Normal:Product`、`Normal:Usable` |
| `transfer`、`port_storager`、`shipment` | `Normal:*` |
| `valuables` | `ValuableDepot:*` |
| `credit_trade` | `ValuableDepot:SpecialItem`、`Isolate:*` |
| `single_roi` | `Normal:*` |

## 返回值

`RecognitionResult` 与 Custom detail 使用相同数据结构：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `detail_version` | integer | detail 契约版本，当前为 `1` |
| `matched` | boolean | 是否至少存在一个接受结果 |
| `grid_type` | string | 本次请求的类型，包括 `single_roi` |
| `roi` | object | 请求 ROI，字段为 `x/y/width/height` |
| `matches` | array | 按分数和位置排序的接受结果 |
| `error` | object | 失败时出现，包含稳定的 `code` 和可读 `message` |

`matches[]` 字段：

| 字段 | 说明 |
| --- | --- |
| `item_id` | catalog 顶层物品 ID |
| `name` | 多语言 key，如 `iconRecognition.name.item_copper_ore` |
| `category` | catalog 中文分类标签 |
| `storage_kind` / `category_type` | 可用于后续过滤和业务判断的分类字段 |
| `rarity` | catalog 稀有度 |
| `cell_box` | 所属网格 cell；`single_roi` 时等于请求 ROI |
| `item_box` | 最终模板命中位置 |
| `score` | 最终匹配分数 |
| `row` / `column` | 真实网格结果的行列；`single_roi` 不返回 |

结果按 `score` 降序，再按 `cell_box.y`、`cell_box.x`、`item_id` 排序。`deduplicate=true` 时按该顺序为每个 `item_id` 保留第一项，因此留下的是各物品分数最高的 cell。

Custom 命中时返回 `MAA_TRUE`，`out_box` 等于 `matches[0].cell_box`；没有接受结果时返回 `MAA_FALSE`。MaaFramework 会把回调 detail 包装到外层 `all/filtered/best` 中，命中时完整结果位于 `best.detail`。

### error.code

| `code` | 触发条件 | 说明 |
| --- | --- | --- |
| `invalid_image` | Custom 入口收到空图片 | 请求未进入参数解析和识别流程 |
| `exception` | 参数校验、资源加载、网格检测或匹配抛出异常 | `message` 保留具体失败原因，调用方不应依赖其文本做分支 |
| `no_match` | 识别正常完成，但没有物品达到阈值 | 属于正常拒识结果，不表示组件异常 |

三种错误均返回 `MAA_FALSE`。其中 `no_match` 仍会返回已解析的 `grid_type`、`roi` 和空 `matches`；`exception` 是否包含 `grid_type` 取决于异常发生前是否已成功解析该字段。

## Debug 输出

`debug=true` 时，Custom 入口把本次识别保存到 `exe_dir/../debug/vision/IconRecognition`：

- `raw/<stem>.png`：输入原图；
- `annotated/<stem>.png`：ROI、cell、候选框和分数标注；
- `detail/<stem>.json`：公开结果加内部 `diagnostics`。

三个文件使用相同 stem，合称一组。组件按 `raw` 文件的修改时间只保留最近 20 组，并同步删除其它两个目录中的同组文件。Custom 回调的公开 detail 不包含内部 `diagnostics`；只有 debug 文件和人工测试 detail 会附加该字段。
