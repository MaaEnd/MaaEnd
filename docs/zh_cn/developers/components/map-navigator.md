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
- [异步交互 `INTERACT`](#异步交互-interact)
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

启动方式：

```powershell
cd tools\MapNavigator
uv run main.py
```

页面顶部有三个模式，各自导出一种可直接粘贴到 Pipeline 的配置：

| 模式 | 操作 | 导出内容 |
| ---------- | ------------------------------------------ | ------------------------------------------------ |
| `A* 寻路` | 选择底图与层级，在图上标记目标点并预览路线 | 复制 `NAVMESH` 节点，可直接放入环境监测 `NavPath` |
| `断言模式` | 框选矩形区域 | 复制 `MapLocateAssertLocation` 节点，或仅复制环境监测 `NavAssert` 坐标 |
| `路径编辑` | 连接游戏录制路线，编辑路径点动作 | `复制路径` → 完整 `path` |

`A* 寻路` 与 `路径编辑` 对应 MapNavigator 的两种用法：给定终点由运行时规划路线，或给定完整路线按序执行。两者可混排在同一个 `path` 数组中——长距离移动用 `NAVMESH`，需要精确语义的局部段用坐标点。

`断言模式` 产出的是 [MapLocator](./map-locator.md) 的区域判定节点，用于在导航前确认角色位于预期位置。

`A* 寻路` 与 `断言模式` 都可以直接连接游戏标出当前位置。定位成功后，页面底部同时显示坐标、区域和 MapLocator 实测朝向（`0°` 为北、`90°` 为东），底图参考点上的箭头也会按该朝向绘制；开始录制后这组读数会持续刷新。

---

## 目标寻路：`NAVMESH`

即[快速上手](#快速上手)中的节点，由 `A* 寻路` 模式的 `复制 JSON 配置` 导出。

只需一个 `target`：无需起点、中间点和 `zone_id`。运行时根据当前定位确定角色所在区域与位置，在地图三角面数据（BaseNav）上规划可行路线。

角色的位置与朝向全部来自 [MapLocator](./map-locator.md) 的逐帧识别，MapNavigator 只负责抵达目标。

工具预览与运行时读取同一份数据、使用同一套算法，常规场景下预览路线与运行时规划结果一致，目标是否可达可直接通过预览判断（分叉情形见 [`NAVMESH` 寻路原理](#navmesh-寻路原理)）。该写法已用于自动采集、环境监测等多条生产路线。

### 分层底图坐标：`target_tier`

`NAVMESH` 不写 `target_tier` 时，`target` 按 **base（基础底图）坐标**解释，即上文的默认行为。普通坐标点不写时则完全保留历史解释，不做额外坐标转换。

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

普通坐标动作也可以使用相同的对象格式，直接声明该点的坐标系：

```json
{
    "action": "RUN",
    "target": [
        243.49,
        177.53
    ],
    "target_tier": "Wuling_L4_328"
}
```

该写法适用于 `RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`，以及使用 `target` 的 `HEADING`。`target_tier` **只解释当前节点的坐标**，不会切换区域、不会改变后续节点的上下文，也不能代替真正过图时需要的 `ZONE` / `PORTAL`。

字段也接受驼峰写法 `targetTier`。`NAVMESH` 的未知层名保持兼容行为：记录告警并把目标当作 base 坐标；普通坐标点显式声明了不存在的层名时会直接失败，避免静默走向错误位置。

### 重叠可走面目标：`target_deck_y`

游戏是三维的，底图是二维的：走廊、天桥、屋顶可以压在同一个 `target` 上。不写 `target_deck_y` 时，寻路把该格的所有可走面一并当作终点，**先够到哪张停哪张** —— 上下两张面往往属于同一连通块、不会报错，二维到达判定也照样通过，所以走错面是静默的。

`target_deck_y` 声明这个 `target` 落在哪张可走面上，取值是**该面的世界高度**：

```json
{
    "action": "NAVMESH",
    "target": [
        358.8,
        238.8
    ],
    "target_deck_y": 265.37
}
```

- 数值从 MapNavigator 工具读：点中目标后侧栏列出该点的重叠面，选一项可在画布上高亮那一层，确认后点「选择」写进字段。不要手估。
- 匹配按高度最近且相差不超过 2px（实测相邻面间距 8~9px）。带内没有可走面时报「目标面不可达」，既不退回按格搜索、也不被盲走兜底吸收 —— 响亮失败优于静默走到另一张面。
- 只有一张面的点不需要填；工具在不存在重叠时不会显示这个列表。
- 字段也接受驼峰写法 `targetDeckY`，与 `target_tier` 可同时使用。

声明只作用在它自己那个航点上：**钉的是这一段停在哪张面，起点站在哪张面由寻路按起点自己的高度判断**。所以一段可以从屋顶下到走廊，也可以反过来，不需要给起点补声明；重规划的起点是实时定位、本来也没有面可言。

### 适用边界

`NAVMESH` 只负责移动，不推断业务语义。路线中包含交互、过图、跳台、外力传送时，需要在 `path` 中显式标注，此时应使用下节的录制方式。

---

## 录制完整路径 `path`

### 录制准备

1. 项目开发环境已配置完成，尤其是 `install/agent/cpp-algo.exe` 与 `install/maafw` 可正常使用。
2. 已安装 uv；运行 `uv run main.py` 时会根据 PEP 723 声明自动准备 Python 与依赖。
3. **Windows** 需**以管理员身份运行**，否则游戏（管理员进程）在前台时 `G` / `X` 热键无法接收。`main.py` 启动时会自动检测并弹出 UAC。
4. **macOS** 首次运行需在 **系统设置 → 隐私与安全性 → 输入监控** 中授权当前终端或 uv 管理的 Python 解释器，否则全局热键不生效。
5. 使用 `Win32` 连接时，游戏已启动且窗口**未最小化**；使用 `ADB` 连接时，`adb` 可用且设备已出现在列表中（`检测并刷新设备`）。
6. 角色已位于待录路线的起点附近。

### 录制

在 `路径编辑` 模式选择连接方式，点击 `开始录制`，切回游戏，按期望的自动执行方式走一遍。

录制过程中有两个热键：

| 热键 | 作用 |
| ---- | -------------------------------------------------------------- |
| `G` | 将当前坐标以 `[x, y]` 复制到剪贴板，不影响录制数据，可随时按下 |
| `X` | 在当前位置强制插入一个**严格到达点** |

`X` 用于标记关键位置（交互点、跳台落点等），确保该坐标被记录并标记为严格到达。

`FIGHT`、`TRANSFER`、`HEADING` 等业务语义较强的点**不会在录制时自动判定**，需在停止录制后在页面中手动修改。

点击 `停止录制` 后，工具会整理原始轨迹：统一为 canonical 格式，将跨区域边界两侧的点标记为 `PORTAL`，并按区域分段显示。

### 编辑路径点

- 视角：滚轮缩放，`视角平移 (Alt)` 拖动画面。
- 路径点：`加路点/选择 (1)` 用于添加、选中、拖拽点，`框选工具 (2)` 用于框选多个点。
- 属性：选中点后设置动作与严格标记，点击 `应用属性` 生效。
- 坐标层级：点坐标来自 tier 底图时，在 `坐标层级` 中填写对应区域名；留空即保留旧坐标语义。
- 跨区域路线按区域分段显示，便于检查过图前后的点是否合理。

通常需要修改的只有三处：

1. 将关键交互点改为 `INTERACT` 并标记严格（`X` 录入的点默认已是严格）。需要先确认提示文字再按键时，导出后为该点补写 `interact_text`，见[异步交互](#异步交互-interact)。
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

**显式声明坐标层级**时改用对象格式；未声明的旧数组不受影响：

```json
{
    "action": "RUN",
    "target": [
        243.49,
        177.53
    ],
    "target_tier": "Wuling_L4_328"
}
```

`target_tier` 与 `ZONE` 含义不同：前者只说明当前 `target` 是在哪张底图上点出的，后者声明路线执行与区域校验上下文。给普通点填写 `target_tier` 不会触发层级切换。

**`INTERACT` 点声明交互提示文字**时同样改用对象格式，`interact_text` 即该点要识别的提示文字：

```json
{
    "action": "INTERACT",
    "target": [
        331,
        1578
    ],
    "interact_text": "登记"
}
```

数组写法 `[x, y, "INTERACT"]` 没有放这些字段的位置，需要异步交互就必须写成对象形式；不写 `interact_text` 的 `INTERACT` 保留原语义（到点后直接按一次交互键）。详见[异步交互](#异步交互-interact)。

这几个字段只有 `INTERACT` 点接得住，写在别的动作上会被忽略并在日志里留一行 `carries interact fields without an INTERACT action`。

`NAVMESH` 是例外：它是语义寻路节点，必须使用带 `target` 的对象，不能写成 `[x, y, "NAVMESH"]`：

```json
{
    "action": "NAVMESH",
    "target": [
        1242.04,
        773.41
    ]
}
```

可用动作：

| 动作 | 到达后行为 |
| ---------- | --------------------------------------------------------------------------------- |
| `RUN` | 无额外动作，继续前往下一个点；省略动作时的默认值 |
| `SPRINT` | 冲刺一次 |
| `JUMP` | 跳跃一次 |
| `FIGHT` | 攻击一次 |
| `INTERACT` | 交互一次；写了 `interact_text` 时升级为异步交互，见[异步交互](#异步交互-interact) |
| `COLLECT` | 采集：停止移动，触发 OCR 识别并点击采集，见[采集与挖掘](#采集与挖掘-collect--dig) |
| `DIG` | 挖掘：停止移动，触发挖掘子任务，见[采集与挖掘](#采集与挖掘-collect--dig) |
| `NAVMESH` | 从当前定位自动规划路线并移动到 `target`；必须使用上方对象格式 |
| `TRANSFER` | 原地等待外力（剧情、传送等）将角色送至下一段，再从后续点继续 |
| `PORTAL` | 过图点，触发后盲走一小段并等待区域切换 |
| `HEADING` | 将镜头转到指定朝向，再按一次 `W` 使朝向生效 |

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
> 页面的点编辑面向带坐标的路径点（`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG / NAVMESH`），可为单点编辑 `target_tier`，并由区域信息派生 `ZONE` 声明。`HEADING` 是无坐标控制节点，不属于该编辑模型，建议在导出 `path` 后手动补充维护；`NAVMESH` 也可以在 `A* 寻路` 模式中直接复制。

---

## 节点参数

接口为基于 MAA `Custom` 的 Action：`MapNavigateAction`。

`path` 为唯一必填参数，其余可选参数写在 `custom_action_param` 中：

| 参数 | 默认值 | 说明 |
| ------------------ | ------- | -------------------------------------------------------------------- |
| `map_name` | 空 | 初始区域上下文。`path` 中已有 `ZONE` 声明时通常无需填写 |
| `arrival_timeout` | `60000` | 单个点允许的最长到达时间，超时判定失败，单位毫秒 |
| `sprint_threshold` | `25.0` | 自动冲刺的判定阈值，依据**前方连续可跑段的长度**而非当前点的直线距离 |
| `interact_text` | 空 | 整条路线的交互提示文字默认值，见[异步交互](#异步交互-interact) |
| `interact_scan` | 空 | 整条路线的行进预筛节点默认值，见[换掉图标预筛](#换掉图标预筛) |
| `interact_rec` | `false` | 整条路线的 `INTERACT` 点是否只认提示不按键，见[只认提示不按键](#只认提示不按键rec-模式) |

顶层未知字段会被静默忽略，不报错。

`interact_text` 接受一个字符串或字符串数组，也可以写成 `{ "node": "某个 OCR 节点" }` 从那个节点取文字表（见[跨路线共用一张文字表](#跨路线共用一张文字表)），驼峰写法 `interactText` 一样接受；`interact_scan` 接受一个字符串，驼峰写法 `interactScan`。这两个都是写在点上的优先，路线级只补给那些自己没写的 `INTERACT` 点。

`interact_rec` 是布尔，驼峰写法 `interactRec`，它不按「点上优先」那套走：路线级写 `true` 是把这条路线的 `INTERACT` 点整批打开，点上再写 `false` 也关不掉——布尔分不出「写了 `false`」和「没写」，所以这个字段只能开不能关。要留几个点照常按键，就别写在路线级，逐点写。

一条路线的交互点通常属于同一业务，此时写在路线级比逐点重复更省事：

```json
"custom_action_param": {
    "interact_text": ["登记", "接取委托"],
    "path": [
        ...
    ]
}
```

`interact_text` 的空字符串与空数组会被直接拒绝（整个节点参数解析失败），不会当作没写：空文本在识别侧等同于「什么都匹配」，见提示就按键。`interact_scan` 写空字符串等同于没写，回落到出厂的那一份。

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
3. 按当前楼层高度做落点吸附，把窗口内的三角面栅格化为带高度区间的格子，在格子图上执行 A\*，再对结果做净空约束下的拉直。
4. 将规划结果展开为普通 `RUN` 路径点，交给移动执行链路。目标不可达时另有盲走兜底，走出的路线不来自规划器。

其中楼层吸附需要特别说明：多层地图在同一平面坐标上叠加了多层的三角面，不区分高度时，起点或终点可能被吸附到错误楼层，表现为角色穿墙或路径不可达。BaseNav 在数据包中为每个 zone 烘焙了楼层高度，规划时按高度带筛选候选面，因此多层区域的落点是确定的。

> [!NOTE]
>
> 工具的 A\* 预览与运行时寻路读取同一份 `base.nav.gz`，规划逻辑是两份互为镜像的实现，同层、无临时障碍的常规场景下结果一致。运行时独有的盲走兜底、跨层双端楼层与展开期的路径点压缩不在预览内，这些情况下两者会分叉。预览一致的只是**规划结果**；实际执行仍受定位、地形与游戏内状况影响，外层 Pipeline 仍需做好失败兜底。

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

| 文件 | 职责 | 何时需要修改 |
| ------------------------------------------------------------- | --------------------------------------------------------------- | ---------------------------- |
| `assets/resource/pipeline/AutoCollect/AutoCollectRoute*.json` | 路线定义，包含 `MapNavigateAction` 节点和采集坐标 | 新增路线、调坐标、增减采集点 |
| `assets/resource/pipeline/AutoCollect/AutoCollectClick.json` | `COLLECT` 触发的 OCR 与点击子任务，入口 `AutoCollectClickStart` | 增删 OCR 识别的采集物名称 |
| `assets/resource/pipeline/AutoCollect/AutoCollectDig.json` | `DIG` 触发的挖掘子任务，入口 `AutoCollectDigStart` | 挖掘交互逻辑变化 |
| `assets/resource/pipeline/AutoCollect.json` | 路线遍历、失败收集、任务前后存放背包 | 新增路线入口或调整总流程 |

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

## 异步交互 `INTERACT`

### 概念

`INTERACT` 有两种语义，由路径点上有没有 `interact_text` 决定：

- **不写**：到点后直接按一次交互键。这是历史行为，已有路线不受影响。
- **写了**：升级为**异步交互**。走向该点的最后一段时后台持续预筛交互提示图标，出现提示就停车，交给 Pipeline 子任务 OCR 确认提示文字，只有命中 `interact_text` 才按交互键；提示既然弹了就说明游戏认为已在范围内，因此这一下按完该点即算走完，不再往前挪剩下那点距离。

之所以要路线自己给文本：可交互物的种类太多，交互提示文字逐业务不同，一张通用名字表穷举不完。采集的名字表是通用的，所以 `COLLECT` 的文本留在 Pipeline 节点里；交互的文本必须由用它的那条路线注入。

路线能定制的是这一停一认两次识别，外加认出来之后按不按键：

| 字段 | 换掉的是 | 不写时 |
| --------------- | --------------------------------- | ------------------------------ |
| `interact_scan` | 行进中决定**何时停车**的图标预筛 | 用出厂那份，找默认提示图标 |
| `interact_text` | 停车后决定**是不是它**的 OCR 文字 | 这个点不算异步交互，到点直接交互 |
| `interact_rec` | 认出来之后**按不按键** | 按 |

按的是交互键，这一下按什么不可换（Windows 按 F、macOS 走按键号、触屏端点击识别到的提示框）；不想让导航按，写 `interact_rec` 让它只认不按，见[只认提示不按键](#只认提示不按键rec-模式)。交互打开界面之后的一切归外层 Pipeline，导航跑完这个点就返回。详见[换掉图标预筛](#换掉图标预筛)。

### 写法

在 `path` 中把交互点写成对象形式，加 `interact_text`：

```json
"path": [
    { "action": "ZONE", "zone_id": "Wuling_Base" },
    [405, 1592],
    { "action": "INTERACT", "target": [331, 1578], "interact_text": "登记" }
]
```

- `interact_text` 收字符串或字符串数组；数组内任一条命中即按键。
- 多条路线认同一批文字时，`interact_text` 改写成 `{ "node": "..." }` 点名一个 OCR 节点，见[跨路线共用一张文字表](#跨路线共用一张文字表)。
- 文本按 OCR 结果做**正则匹配**，与 Pipeline 的 `expected` 语义一致。中文提示直接照抄游戏里的字即可，含正则元字符时需自行转义。
- 一整条路线共用同一业务时，把 `interact_text` 写在 `custom_action_param` 顶层作为默认值，见[节点参数](#节点参数)。
- 只要导航走到位、把提示认出来，按键交回外层 Pipeline 决定时，加 `interact_rec`，见[只认提示不按键](#只认提示不按键rec-模式)。
- 同一条路线可以混排多个业务的交互点、混排 `COLLECT` 与 `DIG`，数量不限。
- **不需要**再补末尾的 `true`：`INTERACT` 本身即按严格到达语义处理。

工具中的操作方式：录制时正常走到交互物旁，停止录制后将该点动作改为 `INTERACT`，导出 `path` 后手动为该点补写 `interact_text`（编辑器目前不产出这个字段）。

### 文本怎么填才抗得住 OCR

OCR 不总是可靠，所以这个字段本来就是**一组**正则，而不是一句原文。按习惯写法列全变体：

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": [
        "^登记$", // 锚定短动词，避免提示区里别的字蹭上
        "^登記$", // 繁体
        "(?i)^Register$", // (?i) 忽略大小写
        "(?i)^Sign\\s*In$" // \\s* 吃掉 OCR 多切出来的空格
    ]
}
```

几条经验：

- **锚定优先。** 匹配用的是「包含即命中」而非整串相等，所以提示是「登记」这类短动词时写 `^登记$`；不锚定的 `登记` 会被同 ROI 里任何含这两个字的文本命中。
- **宁窄勿宽。** 漏识别只是这个点白跑一次（导航不失败，见下节），误识别却会对着别的东西按键。
- **变体逐条列，别指望一条模式吃下所有语言。** 简繁、英文、日文各写一条；语法是 Perl 风格，`(?i)` 这类内联标志可用。JSON 里反斜杠要写两个。
- **首尾空格不用管**，识别结果在匹配前已经去过首尾空白；`\\s*` 只用于吃掉文本中间被切开的空格。
- **JSONC 注释可用**，建议给每条写清它对应哪种语言或哪个界面，禁用某条时把原因也留下。
- **正则写坏了不会静默失效。** 注入前会做一次合法性校验，不合法则整个子任务不予派发，日志里连着出现 `regex invalid`、`failed to override_pipeline` 与 `Prompt subtask failed to dispatch`；这一点表现为不按键，但不会让导航失败。
- 现成范本见 `assets/resource/pipeline/RealTimeTask/AutoPick.json` 的 `AutoPickInteractive`：它把物名与 `^采集$`、`^打开$` 一类动词混在一张表里，并注明了哪几条为什么不启用。

复用的边界：同一条路线内共用一张表，写在 `custom_action_param` 顶层即可（见[节点参数](#节点参数)）；跨路线共用则把表放进一个 OCR 节点，见下节。

### 跨路线共用一张文字表

多条路线认同一批提示文字时（同一业务铺到多个区域最常见），把 `interact_text` 写成对象、点名一个 OCR 节点，表就只留那一份：

```json
"path": [
    { "action": "INTERACT", "target": [331, 1578], "interact_text": { "node": "MyBusinessInteractText" } }
]
```

整条路线共用时同样可以写在顶层：

```json
"custom_action_param": {
    "interact_text": { "node": "MyBusinessInteractText" },
    "path": [
        ...
    ]
}
```

被点名的就是一个普通 OCR 节点，文字写在它的 `expected` 里；这个节点常常本来就存在（停车后要点的那个按钮自己的识别节点），此时连新节点都不用加：

```json
"MyBusinessInteractText": {
    "recognition": {
        "type": "OCR",
        "param": {
            "roi": "MyBusinessInteractButton",
            "expected": ["^登记$", "^登記$", "(?i)^Register$"]
        }
    }
}
```

几条边界：

- **只读 `expected`，那个节点本身从不被派发。** 它的 `roi` 等字段属于真正跑它的人，导航不看也不改。
- **必须是 OCR 节点，`expected` 必须是非空、不含空串的数组。** 不满足时日志里留一行 `Interact text node must recognize by OCR` 或 `has no usable expected list`，该点退回原语义（到点直接按一次交互键），**整条路线照跑**。
- **对象形状写错是硬错误。** 键名拼错、`node` 不是字符串或是空串，都会让整个节点参数解析失败，与 `interact_text` 写空串是同一种失败。
- **点上写的盖过顶层的**，与直接写文字同一条规则；文字与节点在同一个点上不会并存，因为它们本来就是同一个字段。
- **读取发生在开跑前**，成功时日志里留一行 `Interact text resolved from node`。

### 换掉图标预筛

走向交互点的最后一段里，后台每隔固定节拍在屏幕上找一次提示图标，找到才停车。这一步只决定「值不值得停车」，停下后还要由 `interact_text` 再确认一次才按键，所以它可以很松。

出厂的那一份就是一个普通 Pipeline 节点 `MapNavigatorInteractScan`，`roi` / `template` / `threshold` 都写在里面。提示图标长得不一样、或者不出现在默认区域时，照抄一份改成自己的，再用 `interact_scan` 点名：

```json
"MyBusinessInteractScan": {
    "recognition": {
        "type": "TemplateMatch",
        "param": {
            "roi": [755, 330, 297, 312],
            "template": "MyBusiness/InteractHint.png",
            "threshold": 0.75
        }
    }
}
```

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": "登记",
    "interact_scan": "MyBusinessInteractScan"
}
```

- 这个节点**不会被派发执行**，MapNavigator 只读它这三个参数。所以它不需要 `action`、不需要 `next`，也不必接在任何链上。
- `recognition.type` 必须是 `TemplateMatch`，`template` 只能给一张图，`threshold` 只能给一个数（不填按 `0.75`）。`roi` 必须是 1280×720 基准帧里的绝对坐标，不支持相对上一节点的偏移写法——预筛每帧独立跑，没有「上一个节点」。
- `template` 按 `image/` 下的相对路径解析，与 Pipeline 里的写法一致；平台 overlay 里同名图会照常胜出，所以触屏端要换图时在 `resource_adb` 下补一份即可。
- 以上任一条不满足，日志里会出现一行 `Prompt scan node ...`，**点名它的那些点退化为「精确到点后再识别」**，不会连累整条路线，也不会静默按错。
- 一条路线最多同时武装 4 份预筛（每份一个后台线程）。同一个节点名被多个点点名只算一份。超出的会报 `Prompt scan node dropped at the cap`，那些点同样退化为到点识别。
- **预筛得配着 `interact_text` 用。** 只写 `interact_scan` 的点会退回原语义（到点直接按一次交互键），预筛不生效，日志里留一行 `names a prompt scan node without any interact text`。文本可以写在点上，也可以写在路线根上由这些点继承。
- 阈值调松不会把导航拖死：预筛命中一次就把该点算走完，所以一份见啥都报的预筛只会把自己的点一个个花掉，路线仍然往前排空。但那些点也就没真交互上，阈值该按图标本身定。

### 只认提示不按键：rec 模式

写 `"interact_rec": true` 的 `INTERACT` 点，前面那一整套照旧——行进中预筛、见提示停车、停下后 OCR 确认——**只有最后那一下交互键不按了**。判定圈仍按异步交互的紧半径收，提示命中仍然算这个点走完。

用得着它的场合：提示弹出来之后按哪一个得由业务决定，而交互键只会选中默认那一项。典型是一个可交互物上挂着多行选项（领取 / 放弃之类），按交互键永远是第一行，业务侧那个「按哪一行」的开关于是根本轮不到生效。让导航只负责走到位、把提示认出来，按哪一行交回外层 Pipeline，那个开关才重新说了算。

```json
{
    "action": "INTERACT",
    "target": [331, 1578],
    "interact_text": "领取",
    "interact_rec": true
}
```

- **得配着 `interact_text` 用**，理由与 `interact_scan` 相同：文本没解析出来的点进不了异步那条路，会退回原语义（到点直接按一次交互键），而那正是 rec 模式要避开的。为防这种退化真把键按下去，原语义那条路上也认这个字段，跳过按键并留一行 `INTERACT in rec mode, skipping the key press`。
- **停车后那次识别照跑**，只是识别节点自己的按键动作被换成 `DoNothing`。所以 `interact_text` 该怎么填还怎么填，命中与否照常出现在日志里。
- **导航不再产生任何点击**。交互到底做没做成完全由外层 Pipeline 验收——这本来就是异步交互的语义，rec 模式只是把它贯彻到底。
- 整条路线的交互点归同一业务时，`interact_rec` 可以写在 `custom_action_param` 顶层，见[节点参数](#节点参数)；注意它只能开不能关。

### 到点判定与移动

异步交互点与采集点使用同一组判定值，全部由运行时控制，路线作者无法也无需干预：

- 到点判定圈按更小的半径收紧，长时间够不着时才退让到常规半径。
- 临近该点的整段路禁用自动疾跑，避免冲过头。
- 更近的一段自动从跑动切换为走路，减少冲过提示触发范围。

预筛只在**正走向该点、且已进入最后一段**时按固定节拍尝试，后台没报提示时这一拍零成本；因此一条不含异步交互点的路线不受任何影响。远处路过别的可交互物不会被误认成这个点的提示。

### 兜底与失败语义

- 途中没等到提示时，**精确到点后仍会执行一次权威识别**，不会因为预筛漏了就整点跳过。因此每个异步交互点必定恰好跑一次权威识别：途中命中即走完，没命中则到点补跑。
- **异步交互不会让导航失败。** 按键只发生在 OCR 命中注入文本时，rec 模式下则一次都不按；导航本身不校验交互是否真的发生，也不因为没识别到提示而报错。交互到底成不成，需要外层 Pipeline 自己验收（例如交互后应出现的界面）。

交互会打开界面（登记台、委托板一类）时，**建议把该点作为路线的最后一个点**：界面一开角色就不再移动、小地图也被盖住，后面的点走不了。这与不写 `interact_text` 的 `INTERACT` 是同一条作者纪律，异步与否都一样。

`MapNavigatorInteract` 节点里预置了一条永远匹配不上的占位文本，路线没注入 `interact_text` 时该节点识别不到任何东西，因此不会误按。

### 相关文件

| 文件 | 职责 | 何时需要修改 |
| ------------------------------------------------ | ------------------------------------------------------------------------ | ---------------------------------- |
| 业务自己的路线 JSON | `MapNavigateAction` 节点、交互坐标与 `interact_text` | 新增交互点、改文案 |
| `assets/resource/pipeline/MapNavigator/Interact.json` | 异步交互子任务，入口 `MapNavigatorInteractStart`，含提示 ROI 与按键动作；出厂预筛 `MapNavigatorInteractScan` 也在这里 | 提示区域、交互按键或出厂预筛的默认值变化 |
| `assets/tasks/setting/Keymap.json` | 交互键改绑，`KeymapInteract` 会同时作用于该节点 | 增删受改绑影响的节点 |
| `assets/resource_macos/pipeline/MacOSKeyMap.json` | macOS 下的按键号覆盖，与其他交互键节点同列 | 增删会按交互键的节点 |
| `assets/resource_adb/pipeline/MapNavigator/Interact.json` | ADB / 云游戏 / PlayCover 下改为点击识别到的提示框（触屏没有 F 键） | 触屏端的动作方式变化 |

**绝大多数情况下只需修改业务自己的路线 JSON。**

### 无需修改的部分

以下文件由 cpp-algo 维护者负责，路线作者无需修改：

- `agent/cpp-algo/source/MapNavigator/async_prompt_action.h` / `.cpp`：提示驱动动作的公共实现，`COLLECT` 与异步 `INTERACT` 共用一套。
- `agent/cpp-algo/source/MapNavigator/prompt_scan_profile.h` / `.cpp`：从 Pipeline 节点读预筛参数与交互文字表，以及那一串校验。
- `agent/cpp-algo/source/MapNavigator/navi_config.h`：子任务节点名、预筛节拍与兜底常量、判定值等。
- `agent/cpp-algo/source/MapNavigator/navi_param_parser.cpp`：`interact_text` / `interact_scan` / `interact_rec` 的解析与路线级默认值下发，以及开跑前把点名的 OCR 节点读成文字表。
- `agent/cpp-algo/source/MapNavigator/semantic_nodes.cpp`：到点后的兜底执行逻辑。

---

## 实践建议

1. **首选 `NAVMESH`，需要语义再录制。** 纯移动路线在 `A* 寻路` 模式预览确认可达后即可复制节点；预览走不通的目标当场调整，不必等到运行时失败。只有含交互、过图等语义的路线才需要录制。
2. **录制优于手写。** 实际走一遍通常比凭感觉填写坐标更准确；若录制时打点精度不足，可放慢移动速度。
3. **保证起点状态稳定。** 录制前先调整好站位与视角，可显著减少后续的修点工作。
4. **特殊动作点少而精。** `INTERACT`、`TRANSFER`、`PORTAL`、`HEADING` 只放在确实需要触发的位置。
5. **跨区域路线务必检查过图点。** 自动补充的 `PORTAL` 仅是语义标注，不代表每个跨区域边界都天然合理。
