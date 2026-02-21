# 开发手册 - MapTracker 参考文档

## 简介

此文档介绍了如何使用 MapTracker 相关的节点。

**MapTracker** 是一个基于计算机视觉的**小地图追踪系统**，能够根据游戏内的小地图来推断玩家所处的位置，并且能够操控玩家按照指定路径点移动。

### 重要概念

1. **地图名称**：每张大地图在游戏中都有唯一名称，例如 "map001_lv001"，其中 "map001" 表示地区是“四号谷地”，"lv001" 表示子区域是“枢纽区”。请查看 `/assets/resource/image/MapTracker/map` 以获取完整的地图名称列表。
2. **坐标系统**：MapTracker 使用的坐标是小地图的图片像素坐标 $(x, y)$，以图片的左上角作为原点 $(0, 0)$。

### 工具支持

在仓库内提供了一个 GUI 工具，可以非常方便地**生成路径点列表**，并且支持从现有的 pipeline 文件中加载和编辑路径点。请安装 Python 和 `opencv-python`，然后运行 `/tools/map_tracker_tool.py`。

## 节点说明

下面将详细介绍 MapTracker 提供的节点的具体用法。这些节点都是 Custom 类型的节点，需要在 pipeline 中指定 `custom_action` 或 `custom_recognition` 来使用。

### Action: MapTrackerMove

🚶操控玩家在指定的路径点上移动。

#### 节点参数

必填参数：

- `map_name`: 地图的唯一名称。例如 "map001_lv001"。

- `path`: 由若干个坐标组成的路径点列表。玩家将会依次移动到这些坐标点。

<details>
<summary>高级可选参数：</summary>

- `arrival_threshold`: 正实数，默认 `4.5`。判断到达下一个目标点的距离阈值，单位是像素距离。较大的值会更容易被判定为到达目标点，但可能导致寻路不完全；较小的值会要求更精确地到达目标点，但可能导致寻路难以完成。

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

#### 注意事项

使用这个节点时，务必确保玩家初始所处的位置**能够直线抵达** `path` 中的第一个坐标点，并且玩家始终处于指定的地图中。推荐使用 [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) 节点来进行前置检查。

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

<details>
<summary>高级可选参数：</summary>

- `precision`: 介于 $(0, 1]$ 的实数，默认 `0.4`。控制匹配的精确度。较大的值会更严格地匹配地图特征，但可能导致匹配速度缓慢；较小的值会极大提升匹配速度，但可能导致结果错误。在需要匹配的地图数量较少时（例如只匹配一张地图），推荐使用较大的值以获得更准确的结果。

- `threshold`: 介于 $(0, 1]$ 的实数，默认 `0.5`。控制匹配的置信度阈值。低于此值的匹配结果将不命中识别。

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

#### 注意事项

MapTracker 使用一个介于 $[0, 360)$ 的整数来表示玩家的**朝向**，单位是度。0° 表示朝向正北方向，以顺时针旋转为递增方向。

### Recognition: MapTrackerAssertLocation

✅判断玩家当前所处的地图名称和位置坐标是否满足任一预期条件。

#### 节点参数

必填参数：

- `expected`: 由一个或多个条件组成的列表。每个条件对象需要包含以下字段：
    - `map_name`: 预期地图的唯一名称。
    - `target`: 由 4 个整数组成的列表 `[x, y, w, h]`，表示预期坐标所处的矩形区域。

<details>
<summary>高级可选参数：</summary>

- `precision`: 含义同 [MapTrackerInfer](#recognition-maptrackerinfer) 节点中的 `precision` 参数。

- `threshold`: 含义同 [MapTrackerInfer](#recognition-maptrackerinfer) 节点中的 `threshold` 参数。

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
