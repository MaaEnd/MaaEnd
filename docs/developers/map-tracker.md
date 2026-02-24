# 开发手册 - MapTracker 参考文档

## 简介

此文档介绍了如何使用 MapTracker 相关的节点。

**MapTracker** 是一个完全基于计算机视觉的**小地图追踪系统**，能够根据游戏内的小地图来推断玩家所处的位置，并且能够操控玩家按照指定路径点移动。

### 重要概念

1. **地图名称**：每张大地图在游戏中都有唯一名称，例如 "map001_lv001"，其中 "map001" 表示地区是“四号谷地”，"lv001" 表示子区域是“枢纽区”。请查看 `/assets/resource/image/MapTracker/map` 以获取所有地图名称和图片（这些图片已被缩放处理，以适配 720P 分辨率的游戏中的小地图 UI）。
2. **坐标系统**：MapTracker 使用的坐标是上述大地图的图片像素坐标 $(x, y)$，以图片的左上角作为原点 $(0, 0)$。

## 节点说明

下面将详细介绍 MapTracker 提供的节点的具体用法。这些节点都是 Custom 类型的节点，需要在 pipeline 中指定 `custom_action` 或 `custom_recognition` 来使用。

### Action: MapTrackerMove

🚶操控玩家在指定的路径点上移动。

> [!IMPORTANT]
>
> 在仓库内提供了一个 **GUI 工具**，可以非常方便地生成、导入和编辑路径点。请参阅[工具说明](#工具说明)以了解如何最大化地利用工具来提高效率。

#### 节点参数

必填参数：

- `map_name`: 地图的唯一名称。例如 "map001_lv001"。

- `path`: 由若干个坐标组成的路径点列表。玩家将会依次移动到这些坐标点。

可选参数：

- `no_print`: 真假值，默认 `false`。是否关闭寻路状态的 UI 消息打印。为提升用户体验，不建议关闭此节点的消息打印。

<details>
<summary>高级可选参数（展开）</summary>

- `path_trim`: 真假值，默认 `false`。开启后会在移动前先以当前位置为基准计算到路径中各点的最近距离，将最近的路径点作为新的起点，之前的路径点会被自动跳过；关闭时始终从首个路径点开始移动。

- `arrival_threshold`: 正实数，默认 `3.5`。判断到达下一个目标点的距离阈值，单位是像素距离。较大的值会更容易被判定为到达目标点，但可能导致寻路不完全；较小的值会要求更精确地到达目标点，但可能导致寻路难以完成。

- `arrival_timeout`: 正整数，默认 `60000`。判断无法到达下一个目标点的时间阈值，单位是毫秒。超过这个时间还未到达下一个目标点，则寻路立即失败。

- `rotation_lower_threshold`: 介于 $(0, 180]$ 的实数，默认 `8.0`。判断需要微调朝向的方向角偏离阈值，单位是度。

- `rotation_upper_threshold`: 介于 $(0, 180]$ 的实数，默认 `60.0`。判断需要大幅调整朝向的方向角偏离阈值，单位是度。此时玩家将会停下来逐步朝向再继续移动。

- `rotation_speed`: 正实数，默认 `2.0`。玩家调整朝向时的旋转速度乘子，单位是像素每度。较大的值能更快地调整朝向，但可能导致过度和反复调整；较小的值能平滑地调整朝向，但可能导致调整不及时。

- `rotation_timeout`: 正整数，默认 `30000`。判断无法调整朝向的时间阈值，单位是毫秒。超过这个时间还未调整好朝向，则寻路立即失败。

- `sprint_threshold`: 正实数，默认 `25.0`。执行冲刺操作的距离阈值，单位是像素距离。当玩家与下一个目标点的距离超过这个值并且朝向正确时，玩家将会执行冲刺。

- `stuck_threshold`: 正整数，默认 `1500`。判断卡住的最短持续时间，单位是毫秒。当玩家在这一段时间后仍未有实际移动，则会触发自动跳跃。

- `stuck_timeout`: 正整数，默认 `10000`。判断无法脱离卡住状态的时间阈值，单位是毫秒。超过这个时间还未脱离卡住状态，则寻路立即失败。

</details>

#### 示例用法

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerMove",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "path": [
                [
                    688,
                    350
                ],
                [
                    679,
                    358
                ],
                [
                    670,
                    350
                ]
            ]
        }
    }
}
```

> [!TIP]
> 执行此节点之前，推荐使用 [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) 节点来检查玩家的**初始位置**是否满足要求，以便抵达首个路径点。

> [!WARNING]
>
> 执行此节点期间，请确保玩家**始终处于**指定的地图中，并且相邻的路径点之间**可以直线抵达**。

### Recognition: MapTrackerInfer

📍获取玩家当前所处的地图名称、位置坐标和朝向。

#### 节点参数

必填参数：无

可选参数：

- `map_name_regex`: 用于筛选地图名称的[正则表达式](https://regexr.com/)。仅匹配该正则表达式的地图会参与识别。例如：

    - `^map\\d+_lv\\d+$`: 默认值。匹配所有常规地图。
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: 匹配所有常规地图和分层地图（Tier）。
    - `^map001_lv001$`: 仅匹配 "map001_lv001"（四号谷地-枢纽区）。
    - `^map001_lv\\d+$`: 匹配 "map001"（四号谷地）的所有子区域。

- `print`: 真假值，默认 `false`。是否开启识别结果的 UI 消息打印。

<details>
<summary>高级可选参数（展开）</summary>

- `precision`: 介于 $(0, 1]$ 的实数，默认 `0.4`。控制匹配的精确度。较大的值会更严格地匹配地图特征，但可能导致匹配速度缓慢；较小的值会极大提升匹配速度，但可能导致结果错误。在需要匹配的地图数量较少时（例如只匹配一张地图），推荐使用较大的值以获得更准确的结果。

- `threshold`: 介于 $(0, 1]$ 的实数，默认 `0.4`。控制匹配的置信度阈值。低于此值的匹配结果将不命中识别。

</details>

#### 示例用法

```json
{
    "MyNodeName": {
        "recognition": "Custom",
        "custom_recognition": "MapTrackerInfer",
        "custom_recognition_param": {
            "map_name_regex": "^map\\d+_lv\\d+$"
        },
        "action": "DoNothing"
    }
}
```

> [!TIP]
>
> MapTracker 使用一个介于 $[0, 360)$ 的整数来表示玩家的**朝向**，单位是度。0° 表示朝向正北方向，以顺时针旋转为递增方向。

> [!WARNING]
>
> 该节点不适合放在 pipeline 中进行低代码开发。如需判断玩家所处的位置是否符合条件，请使用 [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) 节点。

### Recognition: MapTrackerAssertLocation

✅判断玩家当前所处的地图名称和位置坐标是否满足任一预期条件。

#### 节点参数

必填参数：

- `expected`: 由一个或多个条件组成的列表。每个条件对象需要包含以下字段：
    - `map_name`: 预期地图的唯一名称。
    - `target`: 由 4 个整数组成的列表 `[x, y, w, h]`，表示预期坐标所处的矩形区域。

<details>
<summary>高级可选参数（展开）</summary>

- `precision`: 含义同 [MapTrackerInfer](#recognition-maptrackerinfer) 节点中的 `precision` 参数。

- `threshold`: 含义同 [MapTrackerInfer](#recognition-maptrackerinfer) 节点中的 `threshold` 参数。

- `fast_mode`: 真假值，默认 `false`。控制是否开启快速匹配模式，以额外提升识别速度。除非遇到性能瓶颈，否则不建议开启此模式。

</details>

#### 示例用法

```json
{
    "MyNodeName": {
        "recognition": "Custom",
        "custom_recognition": "MapTrackerAssertLocation",
        "custom_recognition_param": {
            "expected": [
                {
                    "map_name": "map002_lv002",
                    "target": [
                        670,
                        350,
                        20,
                        20
                    ]
                }
            ]
        },
        "action": "DoNothing"
    }
}
```

## 工具说明

我们提供一个 GUI 工具脚本，位于 `/tools/map_tracker/map_tracker_editor.py`。它支持以下基本功能：

1. **创建路径**：选择一张地图，随后用鼠标在地图上创建路径点，最后导出路径点列表或节点 JSON。
2. **编辑路径**：从现有的 pipeline JSON 文件中加载路径点，进行修改后重新保存或导出。

只需安装 Python 和 `opencv-python` 库，即可使用 Python 来运行上述工具脚本。运行后，在控制台中按照指引操作即可。

### 路径编辑的具体用法

**鼠标操作**：左键可以添加、移动或删除路径点；右键可以拖拽地图；滚轮可以用于缩放。

**常用按钮**：

- 保存（Save）按钮仅在编辑现有路径时可用，点击后会将修改保存回原 pipeline 所在的 JSON 文件。
- 完成（Finish）按钮会结束编辑，效果等同于关闭窗口。此时，控制台会要求输入一个导出模式。导出模式有多种选择，可以导出 JSON 节点或路径点列表等。
- 实时定位（Get Realtime Location）按钮会尝试连接定位服务并识别当前游戏内的坐标。定位服务如何开启请参见下方说明。

**定位服务**：

要使用实时定位功能，请使用 [Maa Pipeline Support](https://marketplace.visualstudio.com/items?itemName=nekosu.maa-support) 这个 VS Code 插件来“执行”位于 `/assets/resource/pipeline/MapTracker.json` 中的 `MapTrackerTestLoop` 节点。确保游戏窗口可以被 Maa 正确截图，并且该节点可正常运行。

随后即可使用实时定位按钮来获取游戏内玩家当前的坐标了。
