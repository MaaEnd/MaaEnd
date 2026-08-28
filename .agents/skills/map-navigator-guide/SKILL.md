---
name: map-navigator-guide
description: MaaEnd 自动寻路、地图定位、角色移动与路径导航开发指南，覆盖 MapLocator、MapNavigator、NAVMESH 三角图寻路和路径录制工具。编写涉及坐标定位、位置判断、目标点移动、自动寻路、路线规划或导航执行的 Pipeline 节点，维护 agent/cpp-algo/source 下对应 C++ 实现，或了解 navmesh 与路径规划工作原理时使用。
---

# MaaEnd MapNavigator / MapLocator 组件编写指南

**MapLocator** 与 **MapNavigator** 是 MaaEnd 中使用 C++ 实现的一对地图组件：

- **MapLocator**（Recognition 层）：识别角色当前所处区域、全局像素坐标与朝向。
- **MapNavigator**（Action 层）：基于 MapLocator 的持续定位，驱动角色移动到目标位置。

## 参考资料

### 重要文档

当你判断确实正在进行 MapNavigator / MapLocator 相关工作时，*务必无条件地先读取下列文档*以快速了解详细内容：

- `docs/zh_cn/developers/components/map-navigator.md` 列出了 pipeline JSON 调用方视角下 MapNavigator 的使用方式，包含 `NAVMESH` 语义寻路与路径录制两种工作流；
- `docs/zh_cn/developers/components/map-locator.md` 列出了 MapLocator 的节点参数、返回结构与调参方式。

## 什么时候用哪个节点

这是编写 Pipeline 时最常见的判断，先按下表选择节点：

| 需求                                      | 节点                                  |
| ----------------------------------------- | ------------------------------------- |
| 让角色走到某个已知坐标                    | `MapNavigateAction` 的 `NAVMESH` 节点 |
| 让角色走一条有交互、过图、机关的复杂路线  | `MapNavigateAction` 的录制 `path`     |
| 判断角色当前是否已经站在某个区域内        | `MapLocateAssertLocation`             |
| 只想读出当前坐标 / 朝向，自己决定后续逻辑 | `MapLocateRecognition`                |
| 走到某点后采集 / 挖掘                     | `path` 里的 `COLLECT` / `DIG` 语义点  |

**优先考虑 `NAVMESH`。** 只要目标点在不发生交互、过图或特殊机关的情况下本来就可达，填一个 `target` 坐标即可，运行时会基于三角图自动规划出可执行路径，不需要预先录制整段路线：

```json
{
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
```

只有当路线本身包含导航器无法自行推断的语义（交互、过图、跳台、外力传送）时，才需要退回到录制完整 `path` 的写法。

**普通坐标点来自 tier 底图时，用 `target_tier` 给当前点消歧。** 使用对象格式 `{ "action": "RUN", "target": [x, y], "target_tier": "Wuling_L4_328" }`；同样适用于其他带位置的普通动作和使用 `target` 的 `HEADING`。`target_tier` 只声明当前坐标的来源层级，绝不代表区域切换，也不改变后续点的上下文；真正的区域校验 / 过图仍由 `ZONE` / `PORTAL` 表达。不要把所有坐标统一强制换算成 base，也不要用 `ZONE` 猜测坐标系：未声明的旧点必须保持原语义。

**终点落在重叠可走面上时必须标 `target_deck_y`。** 游戏是三维的、底图是二维的，走廊 / 天桥 / 屋顶可能压在同一个 `target` 上；不声明时寻路把该格全部可走面当终点，先够到哪张停哪张，而且上下两张常属同一连通块、二维到达判定也通过，所以走错面完全无声。数值是那张面的世界高度，用 MapNavigator 点中目标后从重叠面列表读出（可预览确认是哪一层），不要手估。

未启用滑索时，严格按照作者给出的 `path` 顺序执行。普通点和动作全部保留，`NAVMESH` 逐点展开，显式 `target_tier` 仍按原规则投影。

启用滑索后，才会对作者路径做全局规划。每个区间先尝试从起点直达末端；成功时跳过中间的移动和兜底点，失败时仍按作者路径逐点执行。可跳过的点包括 `NAVMESH`、`RUN`、`SPRINT`、`JUMP`、`FIGHT`、`TRANSFER`、`PORTAL` 和 `INTERACT`。

`ZONE` 是结构边界，`HEADING`、`COLLECT`、`DIG` 是固有动作边界，始终保留。其他节点必须抵达并执行时写 `required: true`；该节点会结束当前规划区间，并成为后续规划的新起点。

直达的纯步行路线不可达时，开启滑索的请求仍会尝试用一条连续滑索链桥接起终两侧可走面；桥接成功优先于盲走和作者路径回退。纯步行可达时才按全程成本与收益门槛比较，不能简单按滑索跳数给固定优先级。

声明只钉这一段停在哪张面，起点站在哪张面由寻路按起点自己的高度判断，所以跨面的一段不需要给起点补声明。

**交互点的提示文字别在多条路线里各抄一份。** `INTERACT` 点写了 `interact_text` 才升级为异步交互（行进中看到提示就停车，OCR 确认命中才按键），不写则保持到点直接按键的原语义。这份文字表除了直接写字符串 / 字符串数组，还可以写成 `{ "node": "某个 OCR 节点" }` 点名一个现成的 OCR 节点：导航只读它的 `expected`，从不派发它，于是同一业务铺到多个区域时共用一张表。写在点上的盖过写在 `custom_action_param` 顶层的；节点读不出来时该点退回原语义，整条路线照跑。

**提示弹出来之后按哪一行由业务决定时，给 `INTERACT` 点加 `interact_rec`。** 交互键只会选中默认那一行，所以一个可交互物上挂着「领取 / 放弃」这类多行选项时，导航一按就把业务侧那个「按哪一行」的开关架空了，而且完全无声——键按下去了、界面也开了，看不出哪里不对。写了 `interact_rec` 的点照旧预筛、停车、OCR 确认，只是最后那一下不按，按哪一行交回外层 Pipeline。它得配着 `interact_text` 用（文本没解析出来的点进不了异步那条路，会退回到点直接按键）；写在 `custom_action_param` 顶层是整条路线一起打开，且只能开不能关。

## 组件概览

### 核心代码

**C++** 代码位于 `agent/cpp-algo/source` 目录下，主要包含以下子目录：

- `MapLocator` 目录：小地图定位实现；
    - YOLO 前置鉴别、梯度域 ZNCC 模板匹配、MotionTracker 运动预测；
    - 对外暴露 `MapLocateRecognition` 与 `MapLocateAssertLocation` 两个节点。
- `MapNavigator` 目录：导航状态机与路径执行；
    - `navi_param_parser.cpp`：`custom_action_param` 解析，含 `target_tier` 等字段；
    - `navi_domain_types.h`：`ActionType` 枚举，所有路径点语义动作在此声明；
    - `navi_config.h`：子任务入口名、`pipeline_override`、等待时间等常量；
    - `semantic_nodes.cpp`：各语义点（`COLLECT`/`DIG`/`INTERACT` 等）到达后的执行逻辑；
    - `NavigationStateMachine`：到点判定、疾跑控制、失败与恢复。
- `Navmesh` 目录：BaseNav 三角图寻路核心；
    - `BaseNavReader.cpp`：`.nav` / `.nav.gz` 二进制包解析（magic 为 `BNAV`）；
    - `BaseNavPack.cpp`：zone 索引与楼层高度查询（`floorYForZoneName`）；
    - `BaseNavPlanner.cpp`：A\* 规划、楼层感知落点吸附、路径后处理。

### 工具代码

`tools/MapNavigator` 目录下提供了配套的路径录制与预览工具，采用 Web 架构（本地 FastAPI 后端 + 浏览器前端，仅监听 `127.0.0.1`）。入口为 `main.py`，推荐 `uv run main.py` 启动。

- `web/serve.py`：FastAPI 后端，托管静态站点并提供寻路 / 导入导出 / 录制 WebSocket 接口；
- `web/static/`：浏览器前端（原生 JS + JSDoc + WebGL，ESM 模块，零构建）；
- `navmesh_backend.py`：navmesh 查询后端，把 cpp-algo agent 当作常驻查询进程，几何解码 / 吸附 / 路线都在 agent 里算；
- `connectors.py` / `connection_models.py`：Win32 / ADB / PlayCover 录制连接层；
- `agent_session.py`：cpp Agent + Tasker 的生命周期，录制 / 试跑 / 单次定位 / navmesh 查询共用；
- `recording_service.py`：Maa Agent 录制线程与轨迹采集；
- `navtest_service.py`：实机试跑会话，当前页签里的路线直接交给 `MapNavigateAction` 走一遍、断言框交给 `MapLocateAssertLocation` 认一次（F3 重跑 / F4 终止）；
- `json_import.py`：JSON/JSONC 导入解析与动作语义校验；
- `model.py`：路径数据结构、动作类型与规范化工具。

Web 路径编辑器必须完整保留单点 `required` 与 `target_tier`：导入、规范化、撤销重做和导出均不能丢字段；两者均未声明的普通点继续导出旧数组格式，声明任一字段的点改用对象格式。

工具的 A\* 预览与运行时寻路读取同一份 `base.nav.gz`、走同一套规划逻辑，因此 GUI 上看到的路线与实际执行的路线保持一致。

## 开发时的注意事项

### 数据与运行时的边界

`base.nav.gz` 只承载原始三角面、连边与投影信息。路径的细化处理（抽稀、居中、视线拉直）发生在**运行时**，不在数据包里。这意味着调整路线形态的改动通常不需要重新烘焙数据包。

### 定位是唯一的位置真相

MapNavigator 的位置输入完全来自 MapLocator 的逐帧识别结果。**不要**基于运动预测去补一个“虚拟位置”来填补定位丢失的间隙——这会让导航器在无法观测的情况下继续移动，错误会累积且无法自我修正。定位丢失时的正确做法是依靠既有的保持与恢复机制。

### 导航过程中不要为了转向而停下

调整朝向必须在移动中完成。角色停止前进时镜头无法转动，进而无法产生新的小地图观测，会直接导致定位停滞。任何“先停下、再转向、再出发”的改法都会造成死锁。

### 资源路径

`.nav` 数据包通过可执行文件相对路径定位（`get_exe_dir()` + `resource/` 前缀，优先 `.gz`），与 YOLO 模型的加载方式一致。开发时若从项目根目录运行，路径问题会被工作目录掩盖——验证资源加载改动时，应从其他工作目录、以可执行文件相对布局进行测试。

### C++ 编码规范

本目录下的代码遵循 `docs/zh_cn/developers/coding-standards.md`，另可参考 `cpp-algo-style` 技能。通用的 RAII 辅助设施（`ScopedImageBuffer` / `ScopedStringBuffer` / `to_mat` / `get_exe_dir`）统一放在 `source/utils.h`，**直接复用，不要在各自的 TU 里重新声明一份**。
