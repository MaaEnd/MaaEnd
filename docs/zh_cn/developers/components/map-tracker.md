# 开发手册 - MapTracker 参考文档

## 简介

此文档介绍了如何使用 **MapTracker** 相关的常用节点。

**MapTracker** 是一个完全基于计算机视觉的**小地图追踪系统**，能够根据游戏内的小地图来推断玩家所处的位置，并且能够操控玩家按照指定路径点移动。

### 重要概念

1. **地图名称**：每张大地图在游戏中都有唯一名称，例如 "map01_lv001"，其中 "map01" 表示地区是“四号谷地”，"lv001" 表示子区域是“枢纽区”。请查看 `/assets/resource/image/MapTracker/map` 以获取所有地图名称和图片（这些图片已被缩放处理，以适配 720P 分辨率的游戏中的小地图 UI）。`map_name` 必须与该目录下的文件名（去掉 `.png` 后缀）**完全一致**。
2. **坐标系统**：MapTracker 使用的坐标是上述大地图的图片像素坐标 $(x, y)$，以图片的左上角作为原点 $(0, 0)$。

> [!TIP]
>
> 要想了解更详细的技术细节，请阅读[这一份进阶性文档](./map-tracker%28advanced%29.md)。其中介绍了高级编程节点的使用方法和 MapTracker 的维护办法。

## 节点说明

下面将详细介绍 MapTracker 提供的**常用节点**的具体用法。这些节点都是 Custom 类型的节点，需要在 pipeline 中指定 `custom_action` 或 `custom_recognition` 来使用。

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

- `no_ensure_initial_movement_state`: 真假值，默认 `false`。是否在开始首次移动前跳过“冲刺”准备动作。开启后会直接进入寻路流程，不再主动重置为稳定的初始移动状态。

- `no_ensure_final_orientation`: 真假值，默认 `false`。是否禁用在抵达最后一个目标点时调整玩家朝向以确保相机面向路径的最后一个方向。

- `arrival_threshold`: 正实数，默认 `2.5`。判断到达下一个目标点的距离阈值，单位是像素距离。较大的值会更容易被判定为到达目标点，但可能导致寻路不完全；较小的值会要求更精确地到达目标点，但可能导致寻路难以完成。

- `arrival_timeout`: 正整数，默认 `60000`。判断无法到达下一个目标点的时间阈值，单位是毫秒。超过这个时间还未到达下一个目标点，则寻路立即失败。

- `rotation_lower_threshold`: 介于 $(0, 180]$ 的实数，默认 `7.5`。判断需要微调朝向的方向角偏离阈值，单位是度。

- `rotation_upper_threshold`: 介于 $(0, 180]$ 的实数，默认 `60.0`。判断需要大幅调整朝向的方向角偏离阈值，单位是度。此时玩家将会使用更慢的速度转向。

- `sprint_threshold`: 正实数，默认 `10.0`。执行冲刺操作的距离阈值，单位是像素距离。当玩家与下一个目标点的距离超过这个值并且朝向正确时，玩家将会执行冲刺。

- `stuck_threshold`: 正整数，默认 `2000`。判断卡住的最短持续时间，单位是毫秒。当玩家在这一段时间后仍未有实际移动，则会触发卡住缓解动作。

- `stuck_timeout`: 正整数，默认 `10000`。判断无法脱离卡住状态的时间阈值，单位是毫秒。超过这个时间还未脱离卡住状态，则寻路立即失败。

- `stuck_mitigators`: 字符串列表，默认 `["MoveOrDeleteDevice", "Jump"]`。当玩家被判定为卡住时，依次执行列表中的操作以尝试脱离卡住状态。不允许不做任何操作，如果该字段设为空列表，则效果与默认值相同。可用的操作包括：
    - `"Jump"`：执行跳跃操作；
    - `"MoveOrDeleteDevice"`：尝试删除或移动面前的设备。

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

### Action: MapTrackerGoal

🧭 基于 NavMesh 自动规划路径，并操控玩家移动到指定目标。

#### 工作原理

此节点会先识别玩家当前位置，再读取 NavMesh 路网数据，将当前位置和目标点临时连接到路网中，通过 Dijkstra 算法规划路径，最后交给 [MapTrackerMove](#action-maptrackermove) 执行移动。

若主动指定了滑索策略，还会在寻路前自动扫描大地图中的滑索点位，并将滑索纳入寻路考量中。

#### 节点参数

必填参数：

- `map_name`: 地图的唯一名称。例如 "map02_lv002"。

- `target` 或 `entity_id`: 二者至少提供一个。
    - `target`: 由 2 个实数组成的列表 `[x, y]`，表示目标坐标点。
    - `entity_id`: NavMesh 顶点关联的实体 ID，会从顶点的 `E` 字段查找目标点。

可选参数：

- `zipline_policy`: 字符串，默认 `"Never"`。控制使用滑索的积极程度。可选值：

    | 选项值         | 含义                       | 适用场景                     |
    | -------------- | -------------------------- | ---------------------------- |
    | `"Never"`      | 始终不使用滑索（默认）     | 大多数场景                   |
    | `"Lazy"`       | 仅在极端情况下使用滑索     | 需要跨越水域等不可通行区域时 |
    | `"Active"`     | 像人类玩家一样主动使用滑索 | 不可通行区域较多且路程较长时 |
    | `"Aggressive"` | 非常积极地使用滑索         | 一般不推荐                   |

- 其他参数：支持补充填写 [MapTrackerMove](#action-maptrackermove) 的各个参数，这会透传给最终的移动过程，例如 `fine_approach`、`arrival_timeout`、`stuck_mitigators` 等。

> [!TIP]
>
> 如果同时提供 `target` 和 `entity_id`，节点会优先使用 `target`，不会报错。

#### 示例用法

使用坐标作为目标：

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerGoal",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                670.0,
                350.8
            ]
        }
    }
}
```

使用实体 ID 作为目标：

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerGoal",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "entity_id": 22800173539
        }
    }
}
```

> [!TIP]
>
> 实体信息可在 [assets/data/ZmdMap/maaend_entities.json](/assets/data/ZmdMap/maaend_entities.json) 文件中查找，并使用 [ZmdMap 网站](https://zmdmap.com)进行对照。

> [!WARNING]
>
> 执行此节点期间，请确保玩家**始终处于**指定的地图中，并且目标点能够通过对应 NavMesh 路网抵达。

### Action: MapTrackerZipline

🎢 让滑索架上的玩家转向下一个指定的滑索架，对准后自动执行滑索移动。

#### 节点参数

必填参数：

- `map_name`: 地图的唯一名称。

- `target`: 下一个滑索架所处的地图坐标 `[x, y]`。

可选参数：

- `rotation_threshold`: 介于 $(0, 180)$ 的正实数，默认 `9.0`。判断已朝向目标滑索点的方向角偏离阈值，单位是度。

<details>
<summary>高级可选参数（展开）</summary>

- `timeout`: 正整数，默认 `15000`。转向目标滑索架，以及执行滑索移动操作的超时时间，单位是毫秒。

- `map_name_match_rule`: 含义同 [MapTrackerMove](#action-maptrackermove) 节点中的 `map_name_match_rule` 参数。

</details>

#### 示例用法

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerZipline",
        "custom_action_param": {
            "map_name": "map02_lv002",
            "target": [
                114.0,
                514.0
            ]
        }
    }
}
```

> [!TIP]
>
> 此节点全程**在滑索架上**完成。即，节点被调用时，要求玩家已经在滑索架上；节点执行完毕后，玩家不会自动下滑索架。

> [!WARNING]
>
> 若目标滑索架无法抵达（滑索架未通电、滑索架不存在、障碍物阻挡），此节点会立即返回失败。

### Recognition: MapTrackerAssertLocation

✅判断玩家当前所处的地图名称和位置坐标是否满足任一预期条件。

#### 节点参数

必填参数：

- `expected`: 由一个或多个条件组成的列表。每个条件对象需要包含以下字段：
    - `map_name`: 预期地图的唯一名称。
    - `target`: 由 4 个实数组成的列表 `[x, y, w, h]`，表示预期坐标所处的矩形区域。

<details>
<summary>高级可选参数（展开）</summary>

- `precision`: 含义同 [MapTrackerInfer](./map-tracker%28advanced%29.md#recognition-maptrackerinfer) 节点中的 `precision` 参数。

- `threshold`: 含义同 [MapTrackerInfer](./map-tracker%28advanced%29.md#recognition-maptrackerinfer) 节点中的 `threshold` 参数。

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

### Recognition: MapTrackerBigMapFindImage

🔍 在大地图界面中通过模板匹配查找指定图标的位置。

#### 节点参数

必填参数：

- `template`: 模板图片路径。注意这个路径是相对于 `assets/resource` 目录的，例如 `image/MapTracker/BigMapIcons/Pointer.png`（玩家指针图标）。

- `expected`: 布尔值、非负整数、或者条件对象。控制命中识别的条件，具体含义如下：
    - 如果是布尔值 `true`，则表示找到至少一个匹配结果即可命中识别；
    - 如果是布尔值 `false`，则表示找不到匹配结果才能命中识别；
    - 如果是非负整数 `n`，则表示匹配到恰好 `n` 个结果才能命中识别；
    - 如果是对象 `{"map_name": "...", "target": [x, y, w, h]}`，则表示指定的地图的矩形坐标区域内有至少一个匹配结果才能命中识别。

可选参数：

- `threshold`: 介于 $(0, 1]$ 的实数，默认 `0.5`。匹配置信度阈值，低于此值的匹配结果将被忽略。

- `green_mask`: 布尔值，默认 `false`。是否对模板图启用绿色遮罩。

- `with_rotation`: 布尔值，默认 `false`。是否开启任意角度匹配，适用于需要匹配旋转图标的情况（例如玩家指针）。

- `zoom_value`: 介于 $[0, 1]$ 的实数，默认 `0`。开始匹配之前先将大地图缩放滑块调整到该位置。若设为 `0`（默认），则表示不进行缩放滑块的调整。

- `map_name_regex`: 字符串，默认不填。限制大地图推断时的候选地图范围。仅在可能出现地图误判时设置，例如 `"^map02_lv002$"` 会锁定推断只在 "map02_lv002" 中进行。

<details>
<summary>高级可选参数（展开）</summary>

- `max_matches`: 整数，默认 `32`。控制最多匹配多少个结果。此参数一般无需调整。
- `must_see_points`: 由若干个实数坐标组成的点列表，默认不填。指定匹配过程中地图视口必须涵盖的地图坐标点。若填写了此参数，在匹配期间会自动拖拽地图视口直到所有指定的坐标点都曾出现在视口中。此参数适合需要在大范围区域内进行大规模匹配的情况，但会显著增加匹配耗时。

</details>

#### 示例用法

下面演示了如何判断“蓝色任务定位标”是否在地图的某个区域内：

```json
{
    "MyFindImageNode": {
        "recognition": "Custom",
        "custom_recognition": "MapTrackerBigMapFindImage",
        "custom_recognition_param": {
            "template": "image/SeizeDeliveryJobs/BlueTaskLocation.png",
            "expected": {
                "map_name": "map02_lv005",
                "target": [
                    114,
                    514,
                    19,
                    19
                ]
            },
            "green_mask": true,
            "zoom_value": 0.25
        },
        "action": "DoNothing"
    }
}
```

> [!TIP]
>
> 当然，此节点在 Go 侧调用可以获得更丰富的信息返回。返回结果的具体格式请参考本节点的 Go 代码。

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

- `zoom_value`: 控制在寻找目标点之前的自动缩放调整行为，详情参见 [MapTrackerBigMapZoom](#action-maptrackerbigmapzoom) 节点的 `zoom_value` 参数。不填时，默认值为 0.725。

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

### Action: MapTrackerBigMapZoom

🔍 在大地图界面中调整缩放滑条到指定位置。

#### 节点参数

必填参数：

- `zoom_value`: 介于 $[0, 1]$ 的实数。若设为 `0` 或不填，则表示禁用缩放调整（什么都不会发生）。其余非零值表示的是大地图缩放滑条的点击位置，越接近 `0` 就越接近最近视野（最大缩放），`1` 为最远视野（最小缩放）。

#### 示例用法

```json
{
    "MyNodeName": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapTrackerBigMapZoom",
        "custom_action_param": {
            "zoom_value": 0.7
        }
    }
}
```

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
