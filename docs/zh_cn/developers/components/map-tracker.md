# 开发手册 - MapTracker 参考文档

## 简介

此文档介绍了如何使用 MapTracker 相关的节点。

**MapTracker** 是一个完全基于计算机视觉的**小地图追踪系统**，能够根据游戏内的小地图来推断玩家所处的位置，并且能够操控玩家按照指定路径点移动。

### 重要概念

1. **地图名称**：每张大地图在游戏中都有唯一名称，例如 "map01_lv001"，其中 "map01" 表示地区是“四号谷地”，"lv001" 表示子区域是“枢纽区”。请查看 `/assets/resource/image/MapTracker/map` 以获取所有地图名称和图片（这些图片已被缩放处理，以适配 720P 分辨率的游戏中的小地图 UI）。`map_name` 必须与该目录下的文件名（去掉 `.png` 后缀）**完全一致**。
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

- `map_name`: 地图的唯一名称。例如 "map01_lv001"。

- `path`: 由若干个实数坐标组成的路径点列表。玩家将会依次移动到这些坐标点。

可选参数：

- `no_print`: 真假值，默认 `false`。是否关闭寻路状态的 UI 消息打印。为提升用户体验，不建议关闭此节点的消息打印。

- `path_trim`: 真假值，默认 `false`。是否在寻路启动时选择距离角色最近的路径点作为实际起点（该点之前的路径点会被自动跳过）；关闭此功能则会始终从首个路径点开始移动。

- `fine_approach`: 字符串，默认 `"FinalTarget"`。控制何时启用精细进近（极精确地到达目标点），可选值：

    | 选项值          | 含义                                   | 适用场景                                       |
    | --------------- | -------------------------------------- | ---------------------------------------------- |
    | `"FinalTarget"` | 仅在最后一个目标点启用精细进近（默认） | 大多数场景                                     |
    | `"AllTargets"`  | 在每一个目标点都启用精细进近           | 对途径点的精度要求极高时（例如经过狭窄桥梁时） |
    | `"Never"`       | 不启用精细进近                         | /                                              |

<details>
<summary>高级可选参数（展开）</summary>

- `no_ensure_final_orientation`: 真假值，默认 `false`。是否禁用在抵达最后一个目标点时调整玩家朝向以确保相机面向路径的最后一个方向。

- `arrival_threshold`: 正实数，默认 `2.5`。判断到达下一个目标点的距离阈值，单位是像素距离。较大的值会更容易被判定为到达目标点，但可能导致寻路不完全；较小的值会要求更精确地到达目标点，但可能导致寻路难以完成。

- `arrival_timeout`: 正整数，默认 `60000`。判断无法到达下一个目标点的时间阈值，单位是毫秒。超过这个时间还未到达下一个目标点，则寻路立即失败。

- `rotation_lower_threshold`: 介于 $(0, 180]$ 的实数，默认 `7.5`。判断需要微调朝向的方向角偏离阈值，单位是度。

- `rotation_upper_threshold`: 介于 $(0, 180]$ 的实数，默认 `60.0`。判断需要大幅调整朝向的方向角偏离阈值，单位是度。此时玩家将会使用更慢的速度转向。

- `sprint_threshold`: 正实数，默认 `10.0`。执行冲刺操作的距离阈值，单位是像素距离。当玩家与下一个目标点的距离超过这个值并且朝向正确时，玩家将会执行冲刺。

- `stuck_threshold`: 正整数，默认 `2500`。判断卡住的最短持续时间，单位是毫秒。当玩家在这一段时间后仍未有实际移动，则会触发自动跳跃。

- `stuck_timeout`: 正整数，默认 `10000`。判断无法脱离卡住状态的时间阈值，单位是毫秒。超过这个时间还未脱离卡住状态，则寻路立即失败。

- `map_name_match_rule`: 字符串，默认 `"^%s(_tier_\\w+)?$"`。允许满足该表达式的地图用于寻路。其中 `%s` 会被替换为 `map_name` 参数（并自动做正则转义）。典型值：
    - `^%s(_tier_\\w+)?$`（默认）：允许该地图和它的所有分层地图参与寻路；
    - `^%s$`：仅允许该地图参与寻路。

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
                    688.0,
                    350.0
                ],
                [
                    679.5,
                    358.2
                ],
                [
                    670.0,
                    350.8
                ]
            ]
        }
    }
}
```

> [!TIP]
>
> 执行此节点之前，推荐使用 [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) 节点来检查玩家的**初始位置**是否满足要求，以便抵达首个路径点。

> [!WARNING]
>
> 执行此节点期间，请确保玩家**始终处于**指定的地图中，并且相邻的路径点之间**可以直线抵达**。

### Action: MapTrackerBigMapPick

🫳 在大地图界面中拖动视野直到指定的点出现，随后可以进行点击操作。

#### 节点参数

必填参数：

- `map_name`: 地图的唯一名称。例如 "map01_lv001"。

- `target`: 由 2 个实数组成的列表 `[x, y]`，表示目标坐标点。

可选参数：

- `on_find`: 找到目标点后执行的操作。默认 `"Click"`。可选值为：

    - `"Click"`：点击目标点（默认）；
    - `"Teleport"`：执行传送操作（要求目标点是传送锚点）；
    - `"DoNothing"`：不执行任何操作。

- `auto_open_map_scene`: 真假值，默认 `false`。是否预先自动打开对应的大地图界面。此功能依赖于 SceneManager 系列节点。未启用此功能的情况下，请确认玩家当前已经处于对应的大地图界面。

- `no_zoom`: 真假值，默认 `false`。是否禁用自动缩放调整功能（自动调整大地图的缩放到合适的范围）。禁用此功能可能会降低本节点的成功率。

#### 示例用法

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerBigMapPick",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                585.8,
                825.5
            ],
            "on_find": "Teleport"
        }
    }
}
```

### Recognition: MapTrackerAssertLocation

✅判断玩家当前所处的地图名称和位置坐标是否满足任一预期条件。

#### 节点参数

必填参数：

- `expected`: 由一个或多个条件组成的列表。每个条件对象需要包含以下字段：
    - `map_name`: 预期地图的唯一名称。
    - `target`: 由 4 个实数组成的列表 `[x, y, w, h]`，表示预期坐标所处的矩形区域。

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
                    "map_name": "map02_lv002",
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

### Recognition: MapTrackerInfer

📍获取玩家当前所处的地图名称、位置坐标和朝向。

#### 节点参数

必填参数：无

可选参数：

- `map_name_regex`: 用于筛选地图名称的[正则表达式](https://regexr.com/)。仅匹配该正则表达式的地图会参与识别。例如：

    - `^map\\d+_lv\\d+$`: 默认值。匹配所有常规地图。
    - `^map\\d+_lv\\d+(_tier_\\d+)?$`: 匹配所有常规地图和分层地图（Tier）。
    - `^map01_lv001$`: 仅匹配 "map01_lv001"（四号谷地-枢纽区）。
    - `^map01_lv\\d+$`: 匹配 "map01"（四号谷地）的所有子区域。

- `print`: 真假值，默认 `false`。是否开启识别结果的 UI 消息打印。

<details>
<summary>高级可选参数（展开）</summary>

- `precision`: 介于 $(0, 1]$ 的实数，默认 `0.5`。控制匹配的精确度。较大的值会更严格地匹配地图特征，但可能导致匹配速度缓慢；较小的值会极大提升匹配速度，但可能导致结果错误。在需要匹配的地图数量较少时（例如只匹配一张地图），推荐使用较大的值以获得更准确的结果。

- `threshold`: 介于 $(0, 1]$ 的实数，默认 `0.4`。控制匹配的置信度阈值。低于此值的匹配结果将不命中识别。

</details>

<br>

> [!TIP]
>
> MapTracker 使用一个介于 $[0, 360)$ 的整数来表示玩家的**朝向**，单位是度。0° 表示朝向正北方向，以顺时针旋转为递增方向。

> [!WARNING]
>
> 该节点是为高级编程而设计的，因此不适合放在 pipeline 中进行低代码开发。如需判断玩家所处的位置是否符合条件，请使用 [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) 节点。

### Recognition: MapTrackerBigMapInfer

🗺️ 在大地图界面中推断当前视野区域在地图中的坐标与地图缩放。

> [!WARNING]
>
> 该节点是为高级编程而设计的，因此不适合放在 pipeline 中进行低代码开发。“当前视野区域”的裁切规则请参见具体代码中的定义。

#### 节点参数

请参见具体代码中 `MapTrackerBigMapInferParam` 的类型定义。

## 工具说明

我们提供一个 GUI 工具脚本，位于 `/tools/map_tracker/map_tracker_editor.py`。它支持以下基本功能：

- **创建路径（Create Move Node）**：在地图上可视化地绘制 [MapTrackerMove](#action-maptrackermove) 路径点。
- **创建位置判断节点（Create AssertLocation Node）**：在地图上框选一个用于 [MapTrackerAssertLocation](#recognition-maptrackerassertlocation) 的矩形区域。
- **编辑已有节点（Import from Pipeline JSON）**：从现有的 pipeline JSON 文件中加载上述两种节点，修改后可以直接保存到文件！

### 环境配置和打开办法

准备好 **Python 运行环境**，并通过下面的命令**安装依赖库**：

```bash
pip install opencv-python maafw
```

随后使用 Python 运行程序即可（工作目录需要是项目根目录）：

```bash
python tools/map_tracker/map_tracker_editor.py
```

### 使用方式介绍

🖱**鼠标操作**：左键可以添加、移动或选中路径点；右键可以拖拽地图；滚轮可以用于缩放。

📷**路径录制**：在路径编辑页面中，提供两种录制路径的模式，分别是 **Loop（持续录制）和 Once（单次打点）模式**。在 Loop 模式下，按下录制按钮就会持续录制玩家的路径点；在 Once 模式下，每次按下录制按钮只会录制一个路径点。

> [!NOTE]
>
> 要想使用路径录制功能，您需要确保您已按照本项目的快速开始指南成功搭建了整个环境。
>
> 路径录制功能支持 Win32 和 ADB 两种控制器（优先采用 Win32）。程序会自动检测当前可用的游戏窗口并自动进行连接，无需手动选择。

↕️**层级切换**：部分地图具有层级功能，您可以在左侧的 Tiers List 面板中查看不同层级的地图。

👀**点位属性查看**：单击一个路径点，可以查看它的坐标信息，并且可以进行删除和复制坐标的操作。

✅**完成编辑**：在任意编辑页面的侧边栏中，点击 Finish 按钮可以选择导出方式。

> [!TIP]
> 
> 如果您是在“编辑已有节点”模式下进行编辑的，那么您也可以直接点击 Save 按钮来将更改一键保存到文件中！
