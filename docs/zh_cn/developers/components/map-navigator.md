# 开发手册 - MapNavigator 寻路系统

MapNavigator 是 MaaEnd 的寻路组件：给定目标位置，自动规划路线并控制角色抵达。

路径坐标通过配套工具 `/tools/MapNavigator` 在地图上点选获得，不建议手写。

- [快速上手](#快速上手)
- [配套工具](#配套工具)
- [目标寻路：`NAVMESH`](#目标寻路navmesh)
- [录制完整路径 `path`](#录制完整路径-path)
- [`path` 数据格式](#path-数据格式)
- [节点参数](#节点参数)
- [`NAVMESH` 寻路原理](#navmesh-寻路原理)
- [采集与挖掘 `COLLECT` / `DIG`](#采集与挖掘-collect--dig)
- [实践建议](#实践建议)

## 快速上手

让角色走到指定位置共五步：

1. **启动工具**：`cd tools\MapNavigator`，执行 `uv run main.py`。浏览器自动打开，默认进入 `A* 寻路` 模式。
2. **选择层级**：在左侧 `选择底图与层级` 中选中目标所在的底图 / tier。
3. **标记目标**：在地图上点击目标位置。
4. **复制**：点击 `复制 JSON 配置`。
5. **粘贴**：粘贴到 Pipeline 节点中。

```json
{
    "GotoTarget": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "path": [
                {
                    "action": "NAVMESH",
                    "target": [
                        720,
                        630
                    ]
                }
            ]
        }
    }
}
```

节点无需起点、中间点和 `zone_id`，运行时根据当前定位自动规划；目标位于分层底图时，工具会一并导出 `target_tier` 字段。

例外：路线中包含交互、过图、跳台等机关时，`NAVMESH` 无法推断这些语义，应改用[录制路径](#录制完整路径-path)。

---

## 配套工具

工具负责产出坐标，MapNavigator 负责按坐标执行移动。工具为本地 FastAPI 后端（仅监听 `127.0.0.1`）+ 浏览器前端，启动后自动打开页面，详见 [`tools/MapNavigator/README.md`](../../../../tools/MapNavigator/README.md)。

不使用 uv 时的启动方式：

```powershell
cd tools\MapNavigator
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python main.py
```

页面顶部有三个模式，各自导出一种可直接粘贴到 Pipeline 的配置：

| 模式       | 操作                                       | 导出内容                                         |
| ---------- | ------------------------------------------ | ------------------------------------------------ |
| `A* 寻路`  | 选择底图与层级，在图上标记目标点并预览路线 | `复制 JSON 配置` → `NAVMESH` 节点                |
| `断言模式` | 框选矩形区域                               | `复制断言 JSON` → `MapLocateAssertLocation` 节点 |
| `路径编辑` | 连接游戏录制路线，编辑路径点动作           | `复制路径` → 完整 `path`                         |

`A* 寻路` 与 `路径编辑` 对应 MapNavigator 的两种用法：给定终点由运行时规划路线，或给定完整路线按序执行。两者可混排在同一个 `path` 数组中——长距离移动用 `NAVMESH`，需要精确语义的局部段用坐标点。

`断言模式` 产出的是 [MapLocator](./map-locator.md) 的区域判定节点，用于在导航前确认角色位于预期位置。

---

## 目标寻路：`NAVMESH`

即[快速上手](#快速上手)中的节点，由 `A* 寻路` 模式的 `复制 JSON 配置` 导出。

只需一个 `target`：无需起点、中间点和 `zone_id`。运行时根据当前定位确定角色所在区域与位置，在地图三角面数据（BaseNav）上规划可行路线。

角色的位置与朝向全部来自 [MapLocator](./map-locator.md) 的逐帧识别，MapNavigator 只负责抵达目标。

工具预览与运行时读取同一份数据、使用同一套算法，预览中的路线即运行时实际执行的路线，目标是否可达可直接通过预览判断。该写法已用于自动采集、环境监测等多条生产路线。

### 分层底图目标：`target_tier`

不写 `target_tier` 时，`target` 按 **base（基础底图）坐标**解释，即上文的默认行为。

分层底图（tier）中每一层都是独立的坐标系：同样的 `[123, 456]`，在 base 与在某个 tier 上是两个不同的位置。此时为节点增加 `target_tier` 字段，声明 `target` 属于哪一层：

```json
{
    "action": "NAVMESH",
    "target": [
        81.77,
        108.72
    ],
    "target_tier": "ValleyIV_L1_171"
}
```

- `target`：在工具中切换到对应层级底图后直接点选的坐标，无需手动换算为 base。
- `target_tier`：该层的区域名，即工具层级选择中 `id:name` 冒号后的 name。
- 运行时使用烘焙进 `.nav` 的仿射变换将其投影回 base 坐标系，并按该层楼层高度做落点吸附。

前往 tier 目标仅需一个节点（`target` + `target_tier`），无需额外的 `ZONE`、中间点或坐标换算。字段也接受驼峰写法 `targetTier`；层名不存在时记录一条告警并按 base 坐标处理。

### 适用边界

`NAVMESH` 只负责移动，不推断业务语义。路线中包含交互、过图、跳台、外力传送时，需要在 `path` 中显式标注，此时应使用下节的录制方式。

---

## 录制完整路径 `path`

### 录制准备

1. 项目开发环境已配置完成，尤其是 `install/agent/cpp-algo.exe` 与 `install/maafw` 可正常使用。
2. Python 依赖已安装（见 `requirements.txt`，或直接使用 `uv run`）。
3. **Windows** 需**以管理员身份运行**，否则游戏（管理员进程）在前台时 `G` / `X` 热键无法接收。`main.py` 启动时会自动检测并弹出 UAC。
4. **macOS** 首次运行需在 **系统设置 → 隐私与安全性 → 输入监控** 中授权当前终端或 Python 解释器，否则全局热键不生效。
5. 使用 `Win32` 连接时，游戏已启动且窗口**未最小化**；使用 `ADB` 连接时，`adb` 可用且设备已出现在列表中（`检测并刷新设备`）。
6. 角色已位于待录路线的起点附近。

### 录制

在 `路径编辑` 模式选择连接方式，点击 `开始录制`，切回游戏，按期望的自动执行方式走一遍。

录制过程中有两个热键：

| 热键 | 作用                                                           |
| ---- | -------------------------------------------------------------- |
| `G`  | 将当前坐标以 `[x, y]` 复制到剪贴板，不影响录制数据，可随时按下 |
| `X`  | 在当前位置强制插入一个**严格到达点**                           |

`X` 用于标记关键位置（交互点、跳台落点等），确保该坐标被记录并标记为严格到达。

`FIGHT`、`TRANSFER`、`HEADING` 等业务语义较强的点**不会在录制时自动判定**，需在停止录制后在页面中手动修改。

点击 `停止录制` 后，工具会整理原始轨迹：统一为 canonical 格式，将跨区域边界两侧的点标记为 `PORTAL`，并按区域分段显示。

### 编辑路径点

- 视角：滚轮缩放，`视角平移 (Alt)` 拖动画面。
- 路径点：`加路点/选择 (1)` 用于添加、选中、拖拽点，`框选工具 (2)` 用于框选多个点。
- 属性：选中点后设置动作与严格标记，点击 `应用属性` 生效。
- 跨区域路线按区域分段显示，便于检查过图前后的点是否合理。

通常需要修改的只有三处：

1. 将关键交互点改为 `INTERACT` 并标记严格（`X` 录入的点默认已是严格）。
2. 将需要跳跃、冲刺、等待传送、过图的点改为对应动作。
3. 检查跨区域前后两个点的位置是否合理。

### 导出

点击 `复制路径` 复制到剪贴板的是 **`path` 本体**而非完整节点 JSON，可直接粘贴到 `custom_action_param.path`：

```json
"custom_action_param": {
    "path": [
        ...
    ]
}
```

导出内容已是 MapNavigator 可直接使用的 canonical 格式，建议在页面中完成全部修改后再复制。

### 导入已有路径

`导入 JSON` 可载入已有的 JSON / JSONC 继续编辑，适用于迁移旧路线、复用他人路线或修改历史路线。

工具会递归扫描文件中可识别的 `path`，自动载入点数最多的一条。源数据缺少 zone 信息时，需先在页面中为各段分配区域。导入时严格校验动作语义，**未知动作直接拒绝**，不做静默降级。导入 `MapTrackerMove` / `MapTrackerAssertLocation` 时按兼容表转换到对应的 base 坐标系。

---

## `path` 数据格式

该格式通常由工具导出，无需手写；需要手动调整时参考本节。

`path` 为数组，按顺序依次执行。元素有以下几种写法。

**仅坐标**，到达后继续下一个点：

```json
[
    688,
    350
]
```

**坐标加动作**，到达后执行一次该动作：

```json
[
    720,
    350,
    "SPRINT"
]
```

可用动作：

| 动作       | 到达后行为                                                                        |
| ---------- | --------------------------------------------------------------------------------- |
| `RUN`      | 无额外动作，继续前往下一个点；省略动作时的默认值                                  |
| `SPRINT`   | 冲刺一次                                                                          |
| `JUMP`     | 跳跃一次                                                                          |
| `FIGHT`    | 攻击一次                                                                          |
| `INTERACT` | 交互一次                                                                          |
| `COLLECT`  | 采集：停止移动，触发 OCR 识别并点击采集，见[采集与挖掘](#采集与挖掘-collect--dig) |
| `DIG`      | 挖掘：停止移动，触发挖掘子任务，见[采集与挖掘](#采集与挖掘-collect--dig)          |
| `TRANSFER` | 原地等待外力（剧情、传送等）将角色送至下一段，再从后续点继续                      |
| `PORTAL`   | 过图点，触发后盲走一小段并等待区域切换                                            |
| `HEADING`  | 将镜头转到指定朝向，再按一次 `W` 使朝向生效                                       |

**末尾加 `true`**，表示该点必须严格到达：

```json
[
    700,
    350,
    "INTERACT",
    true
]
```

默认到点判定保留一定半径；标记严格后判定半径更小、到点更慢，但落点更精确。交互、跳台、传送、过图等关键点建议标记——底层对这些动作本身即按更严格的到点语义处理。

**声明区域**，指定后续点所在的地图区域：

```json
{
    "action": "ZONE",
    "zone_id": "Wuling_Base"
}
```

该节点不产生移动，仅为后续路径点提供区域**校验**上下文。录制导出的 `path` 一般已包含。

**单独调整朝向**，同样不产生移动：

```json
{
    "action": "HEADING",
    "angle": 90
}
```

也可以给定坐标，按“当前位置 → 该坐标”的方向转向：

```json
{
    "action": "HEADING",
    "target": [
        688,
        350
    ]
}
```

> [!NOTE]
>
> 页面的点编辑面向带坐标的路径点（`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`）以及由区域信息派生的 `ZONE` 声明。`HEADING` 是无坐标控制节点，不属于该编辑模型，建议在导出 `path` 后手动补充维护；`NAVMESH` 在 `A* 寻路` 模式中直接复制。

---

## 节点参数

接口为基于 MAA `Custom` 的 Action：`MapNavigateAction`。

`path` 为唯一必填参数，其余可选参数写在 `custom_action_param` 中：

| 参数               | 默认值  | 说明                                                                 |
| ------------------ | ------- | -------------------------------------------------------------------- |
| `map_name`         | 空      | 初始区域上下文。`path` 中已有 `ZONE` 声明时通常无需填写              |
| `arrival_timeout`  | `60000` | 单个点允许的最长到达时间，超时判定失败，单位毫秒                     |
| `sprint_threshold` | `25.0`  | 自动冲刺的判定阈值，依据**前方连续可跑段的长度**而非当前点的直线距离 |

顶层未知字段会被静默忽略，不报错。

### 执行结果

`MapNavigateAction` 是 Action 节点，没有 Recognition 那样的结构化输出，结果只有两种：

- 走完整条路线 → 成功。
- 中途持续无进度，或持续偏离路线超时 → 失败。

在 Pipeline 中应将其视为原子动作编排：**要么走完整条路线，要么节点失败**。

### 完整示例

将录制工具导出的 `path` 粘贴进去即为完整节点：

```json
{
    "DebugNavi": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "MapNavigateAction",
        "custom_action_param": {
            "arrival_timeout": 45000,
            "path": [
                {
                    "action": "ZONE",
                    "zone_id": "Wuling_Base"
                },
                [
                    405,
                    1592
                ],
                [
                    400,
                    1583
                ],
                [
                    380,
                    1567,
                    "SPRINT"
                ],
                [
                    331,
                    1578,
                    "INTERACT",
                    true
                ]
            ]
        }
    }
}
```

> [!TIP]
>
> `MapNavigateAction` 应放在已确认入口状态的节点之后：先确认角色处于预期的场景、区域与朝向附近，再开始整段导航，可明显提高成功率。`断言模式` 导出的 `MapLocateAssertLocation` 节点适合作为该入口判定。

> [!WARNING]
>
> 相邻路径点之间应当可以连贯走到。导航器无法穿模、绕开复杂障碍或理解业务机关；过图、跳板、下落、上升机关等路段需显式拆分为 `PORTAL` / `TRANSFER` / 业务节点处理。

---

## `NAVMESH` 寻路原理

本节面向需要了解内部实现的读者，日常使用可跳过。

1. 优先加载 `assets/resource/model/map/navmesh/base.nav.gz`，不存在时回退 `base.nav`。
2. 根据当前定位区域推断对应的 BaseNav zone。
3. 按当前楼层高度做落点吸附，在 `.nav` 三角图上执行 A\*，仅使用 BaseNav 自身的连边。
4. 将规划结果展开为普通 `RUN` 路径点，交给移动执行链路。

其中楼层吸附需要特别说明：多层地图在同一平面坐标上叠加了多层的三角面，不区分高度时，起点或终点可能被吸附到错误楼层，表现为角色穿墙或路径不可达。BaseNav 在数据包中为每个 zone 烘焙了楼层高度，规划时按高度带筛选候选面，因此多层区域的落点是确定的。

> [!NOTE]
>
> 工具的 A\* 预览与运行时寻路读取同一份 `base.nav.gz`、使用同一套规划逻辑，两者结果一致。注意一致的是**规划结果**；实际执行仍受定位、地形与游戏内状况影响，外层 Pipeline 仍需做好失败兜底。

---

## 采集与挖掘 `COLLECT` / `DIG`

`COLLECT` 与 `DIG` 是 MapNavigator 内置的采集 / 挖掘语义点。在 `path` 中将采集坐标的第三位写为对应动作，导航器精确到点后自动停止、同步触发对应的 Pipeline 子任务，完成后继续下一段，**全程不退出 NaviController**：

```json
"path": [
    { "action": "ZONE", "zone_id": "Wuling_Base" },
    [707, 838],
    [720, 832],
    [741, 802, "COLLECT"],
    [744, 800, "COLLECT"],
    [739, 792, "COLLECT"]
]
```

- `[x, y, "COLLECT"]`：到点后触发 OCR 识别 + 自动点击采集（`AutoCollectClickStart`）。
- `[x, y, "DIG"]`：到点后触发无条件点击挖掘（`AutoCollectDigStart`）。
- 同一节点中 `COLLECT` 与 `DIG` 可混合使用，数量不限。
- **不需要**在节点上写 `anchor`，也不需要把 `next` 指向 `AutoCollectClickStart`。

工具中的操作方式：录制时正常走到采集物旁，停止录制后将该点动作改为 `COLLECT` 或 `DIG`。

相比旧的 `anchor` 拆链写法，该方式无需在每次采集时重新建连、重新 Bootstrap、重置疾跑起步宽限；临近采集点的整段路自动禁用疾跑，避免冲过头；多个采集点合并在单个 Pipeline 节点中，无需拆分为多个 `GotoFindN`。

### 相关文件

| 文件                                                          | 职责                                                            | 何时需要修改                 |
| ------------------------------------------------------------- | --------------------------------------------------------------- | ---------------------------- |
| `assets/resource/pipeline/AutoCollect/AutoCollectRoute*.json` | 路线定义，包含 `MapNavigateAction` 节点和采集坐标               | 新增路线、调坐标、增减采集点 |
| `assets/resource/pipeline/AutoCollect/AutoCollectClick.json`  | `COLLECT` 触发的 OCR 与点击子任务，入口 `AutoCollectClickStart` | 增删 OCR 识别的采集物名称    |
| `assets/resource/pipeline/AutoCollect/AutoCollectDig.json`    | `DIG` 触发的挖掘子任务，入口 `AutoCollectDigStart`              | 挖掘交互逻辑变化             |
| `assets/resource/pipeline/AutoCollect.json`                   | 路线遍历、失败收集、任务前后存放背包                            | 新增路线入口或调整总流程     |

**绝大多数情况下只需修改 `AutoCollectRoute*.json`。**

整体流程：`AutoCollectLoop` 依次调用各路线的包装节点，包装节点通过通用的 `FailureCollectorRunTask` 执行已启用的路线；路线内任意节点失败时，包装 Action 记录该路线的 `{Route}Failed` 节点并返回成功，使 Pipeline 继续下一条。所有路线与后置背包整理结束后，`AutoCollectFinish` 按失败顺序调用这些节点，输出 `$option.*.label` 本地化文案，并使自动采集任务返回失败。

### 无需修改的部分

以下文件由 cpp-algo 维护者负责，路线作者无需修改：

- `agent/cpp-algo/source/MapNavigator/navi_domain_types.h`：`ActionType` 枚举，`COLLECT` / `DIG` 在此声明。
- `agent/cpp-algo/source/MapNavigator/navi_config.h`：子任务入口名、`pipeline_override`、采集后等待时间等常量。
- `agent/cpp-algo/source/MapNavigator/semantic_nodes.cpp`：到达采集点后的执行逻辑。

### 注意事项

**旧写法已废弃。** 旧的 `anchor: { "AutoCollectClickAfter": "..." }` + `next: ["AutoCollectClickStart"]` 拆链写法不应再出现在新路线中。

**`AutoCollectClickEnd` 的 `next` 不可修改。** 它指向 `[Anchor]AutoCollectClickAfter` 是为了兼容旧的 anchor 链调用。从 `MaaContextRunTask` 子任务调用时，cpp-algo 会通过 `pipeline_override` 将该 `next` 临时置空，使子任务干净退出。修改后会影响仍在使用旧写法的其他路线。

**疾跑由运行时控制。** 所有 `COLLECT`、`DIG` 与严格到达点之前的整段路，自动疾跑由 cpp-algo 在 `NavigationStateMachine` 层面强制禁用，路线作者无法也无需控制。

### 新增采集路线

1. 在 `assets/resource/pipeline/AutoCollect/` 下新建 `AutoCollectRouteN.json`，参照现有路线编写 `Start` → `AssertLocation` → `Goto` → `End` 四节点骨架。
2. 使用工具录制路径，将采集目标点动作改为 `COLLECT` 或 `DIG`，复制 `path` 并粘贴到 `Goto` 节点的 `custom_action_param.path`。
3. 在 `interface.json` / 任务入口 JSON 中注册新路线入口。
4. 无需修改 `AutoCollectClick.json`、`AutoCollectDig.json` 及任何 cpp-algo 源文件。

---

## 实践建议

1. **首选 `NAVMESH`，需要语义再录制。** 纯移动路线在 `A* 寻路` 模式预览确认可达后即可复制节点；预览走不通的目标当场调整，不必等到运行时失败。只有含交互、过图等语义的路线才需要录制。
2. **录制优于手写。** 实际走一遍通常比凭感觉填写坐标更准确；若录制时打点精度不足，可放慢移动速度。
3. **保证起点状态稳定。** 录制前先调整好站位与视角，可显著减少后续的修点工作。
4. **特殊动作点少而精。** `INTERACT`、`TRANSFER`、`PORTAL`、`HEADING` 只放在确实需要触发的位置。
5. **跨区域路线务必检查过图点。** 自动补充的 `PORTAL` 仅是语义标注，不代表每个跨区域边界都天然合理。
