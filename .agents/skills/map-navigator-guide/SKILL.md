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

| 需求 | 节点 |
| ----------------------------------------- | ------------------------------------- |
| 让角色走到某个已知坐标 | `MapNavigateAction` 的 `NAVMESH` 节点 |
| 让角色走一条有交互、过图、机关的复杂路线 | `MapNavigateAction` 的录制 `path` |
| 判断角色当前是否已经站在某个区域内 | `MapLocateAssertLocation` |
| 只想读出当前坐标 / 朝向，自己决定后续逻辑 | `MapLocateRecognition` |
| 走到某点后采集 / 挖掘 | `path` 里的 `COLLECT` / `DIG` 语义点 |
| 目标只有识别框、没有坐标（名字牌、物件标签） | `path` 里的 `FIND` 语义点 |
| 走着走着某个画面出现，或走到目标坐标就算到 | `path` 末尾的 `TRIGGER` 点 + `trigger_node` |

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

导航器始终按照作者给出的 `path` 顺序执行。普通点和动作全部保留，`NAVMESH` 逐点展开，显式 `target_tier` 仍按原规则投影。

滑索是对作者路线的局部替换，路线不写 `zip` 也会考虑，写 `zip: false` 才关。展开时按边界把路线切段，每段先照作者路线展开当步行基线，滑索比基线省下 `min_gain` 以上才把这段的走路整段换掉；不划算时这段与不开滑索逐点一致。

`ZONE`、除 `RUN` / `NAVMESH` 外的所有动作点、写了 `required: true` 的 `RUN` / `NAVMESH` 点都是段边界，滑索不会跨过，动作照常执行。所以动作点不需要 `required`；只有必须经过的 `RUN` / `NAVMESH` 点才写。

某个 `NAVMESH` 点纯步行不可达时，仍会尝试用一条连续滑索链桥接起终两侧可走面；桥接成功优先于盲走兜底。纯步行可达时才按区间成本与收益门槛比较，不能简单按滑索跳数给固定优先级。

声明只钉这一段停在哪张面，起点站在哪张面由寻路按起点自己的高度判断，所以跨面的一段不需要给起点补声明。

**交互点的提示文字别在多条路线里各抄一份。** `INTERACT` 点写了 `interact_text` 才升级为异步交互（行进中看到提示就停车，OCR 确认命中才按键），不写则保持到点直接按键的原语义。这份文字表除了直接写字符串 / 字符串数组，还可以写成 `{ "node": "某个 OCR 节点" }` 点名一个现成的 OCR 节点：导航只读它的 `expected`，从不派发它，于是同一业务铺到多个区域时共用一张表。写在点上的盖过写在 `custom_action_param` 顶层的；节点读不出来时该点退回原语义，整条路线照跑。

**目标只有识别框、没有坐标时（NPC 名字牌、物件标签等）使用 `FIND` 语义点**，不应在 Pipeline 中使用 `NeuralNetworkDetect` + `CharacterMoveToTargetAction` 自行实现。每个 `FIND` 点需配置两组参数：目标来源（`find_target` 或 `find_text`，二选一）与完成条件（`find_stop` 或 `find_arrive`，至少一项）。旋转视角搜索、按识别框前进、越过目标后的后退均由导航器处理；配置 `find_arrive` 时，该点每拍额外读取一次定位用于距离判定（不参与转向），其余时刻不读取小地图。带 `target` 时先移动至锚点再开始搜索；不带 `target` 时抵达后立即开始搜索。搜索阶段的视角旋转为开环控制，节点结束后角色可能不在路网内且朝向任意，导航器会在退出时重置操舵与走廊记账；若其后仍有移动需求，建议追加一个普通移动节点以供重新定位；`FIND` 作为路线终点亦可行。

**「某个画面出现就算到了」用路线末尾的 `TRIGGER` 点**，别拿 `FIND` 或自己在外层轮询凑。导航一边走一边识别 `trigger_node`，命中就算整条导航成功，跟走到哪无关；一直没命中就走到 `target`，到点即成功。`TRIGGER` 自己直线走向 `target`，要寻路就在前面放一个同坐标的 `NAVMESH` 点。它必须是路线最后一个点；识别节点要选只在目的地出现的画面，路上命中会提前收工。

**提示弹出来之后按哪一行由业务决定时，给 `INTERACT` 点加 `interact_rec`。** 交互键只会选中默认那一行，所以一个可交互物上挂着「领取 / 放弃」这类多行选项时，导航一按就把业务侧那个「按哪一行」的开关架空了，而且完全无声——键按下去了、界面也开了，看不出哪里不对。写了 `interact_rec` 的点照旧预筛、停车、OCR 确认，只是最后那一下不按，按哪一行交回外层 Pipeline。它得配着 `interact_text` 用（文本没解析出来的点进不了异步那条路，会退回到点直接按键）；写在 `custom_action_param` 顶层是整条路线一起打开，且只能开不能关。

## 组件概览

### 核心代码

**C++** 代码位于 `agent/cpp-algo/source` 目录下，主要包含以下子目录：

- `MapLocator` 目录：小地图定位实现；
    - YOLO 前置鉴别、局部对比度归一后的 ZNCC 模板匹配、MotionTracker 运动预测；
    - 对外暴露 `MapLocateRecognition` 与 `MapLocateAssertLocation` 两个节点。
- `MapNavigator` 目录：导航状态机与路径执行；
    - `navi_param_parser.cpp`：`custom_action_param` 解析，含 `target_tier` 等字段；
    - `navi_domain_types.h`：`ActionType` 枚举，所有路径点语义动作在此声明；
- `navi_config.h`：子任务入口名、`pipeline_override`、等待时间等常量；
- `semantic_nodes.cpp`：各语义点（`COLLECT`/`DIG`/`INTERACT` 等）到达后的执行逻辑；
- `find_action.cpp`：`FIND` 的搜索与接近（按帧跑识别节点取框、转视角、前进脉冲），以及退出时的记账作废；
- `trigger_action.cpp`：`TRIGGER` 的行进中识别与到点完成；
    - `state_machine/`（`Navigator` 与 `RunNavigator`）：到点判定、疾跑控制、失败与恢复。
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

`.nav` 数据包通过安装根目录相对路径定位（`common::InstallPath` + `resource/` 前缀，优先 `.gz`），与 YOLO 模型的加载方式一致。安装根目录即 agent 启动时的工作目录，由宿主保证；开发时从仓库根目录直接跑二进制会让它指向错误的位置——验证资源加载改动时，应以 `install/` 为工作目录启动。

### 日志改动要同步日志分析工具

`tools/MapNavigator` 的 `日志分析` 按固定子串匹配 cpp-algo 日志行（行头函数名、消息文本、`[key=value]` 字段）来还原实测轨迹、规划决策和滑索跳，任一处变化都会让它静默失效：拆分 `PositionProvider::Capture` 就曾让实测地面轨迹整段消失。

改动 MapNavigator / MapLocator 的日志输出（增删改日志行、调整 `VAR(...)` 字段、把日志挪进别的函数或改名）时，即使用户没有提出要求，也主动提醒用户同步：

- `tools/MapNavigator/web/static/js/log_analysis.js` 的解析逻辑；
- `docs/zh_cn/developers/components/map-navigator.md`、`docs/en_us/` 对应文档和 `tools/MapNavigator/README.md` 中的日志分析描述。

### C++ 编码规范

本目录下的代码遵循 `docs/zh_cn/developers/coding-standards.md`，另可参考 `cpp-algo-style` 技能。通用的 RAII 辅助设施（`ScopedImageBuffer` / `ScopedStringBuffer` / `to_mat`）统一放在 `source/utils.h`，路径基准统一用 `Common/paths.h` 的 `common::InstallPath` / `common::UserDataPath`，**直接复用，不要在各自的 TU 里重新声明一份**。MapNavigator 的代码组织方式见下一节。

## MapNavigator 的代码写法

`agent/cpp-algo/source/MapNavigator/` 全部写成数据结构加自由函数，状态从参数显式传入。在这个目录里新增或修改代码，都照本节写。

### 判断标准：显式

一个函数只做它名字说的事。它读什么、改什么，在签名和调用处都看得见：数据从哪来、被谁改、到哪去，看签名就知道，不熟 C++ 的人也能从上往下读懂。

看着做 A、实际顺带做了 B 的函数，要么把 B 写进名字，要么把 B 挪到调用方显式调用。写新代码和审查时，先问一句：这个函数有没有偷偷干别的。

### 数据和函数

- 结构体只放数据：字段带默认值，没有成员函数、构造函数和私有字段。字段名 snake_case，不加尾下划线。
- 函数都是自由函数，状态从参数显式传入：
    - 只读的判断写成 `bool Xxx(const T&)`。名字像查询的（`Has`、`Is`、`Get`）不改任何状态。
    - 修改写成 `void Xxx(T*)`，调用处写 `&x`，一个函数只改一件事。要改的状态一律用指针传，不用非 const 引用参数。
    - 结果用返回值，可能没有结果时用 `std::optional`。
    - 要调用方去做的事用 `enum class` 返回。调用方 `switch`，不写 `default`，`switch` 后补一行返回。
- 参数顺序：被改的状态（指针）在前，只读依赖（`const&`）其次，再是值，时间点放最后。
- 一个参数用来切换两种行为时，拆成两个函数，不传 `bool` 模式参数。
- 有副作用的调用单独写一句，不塞进复合条件。

`planning/navigation_session.h`（节选）：

```cpp
struct Session
{
    std::vector<Waypoint> current_path;
    size_t current_node_idx = 0;
    NaviPhase phase = NaviPhase::Bootstrap;
    bool success = false;
    // ...
};

bool HasCurrentWaypoint(const Session& session);
bool FinalSuccessReady(const Session& session, const NaviPosition& position);
void CommitFinalSuccess(Session* session, const NaviPosition& position, const char* reason);
void LeaveCurrentWaypoint(Session* session, ActionType expected_action, const char* reason);
```

模块只给出结论，怎么处理由编排层决定（`state_machine/navigation_runtime_state.h`、`state_machine/recovery.cpp`，省略了日志和失败参数）：

```cpp
enum class WedgeAction
{
    None,
    Replan,
    Fail,
};

WedgeAction StepWedge(OffRouteWedgeState* wedge, double progress, TimePoint now);
```

```cpp
switch (StepWedge(&nav->runtime_state.offroute, route.progress_distance, tick.now)) {
case WedgeAction::None:
    return TickStep::Next;
case WedgeAction::Fail:
    return EndTickWith(FailNavigation(nav, ...));
case WedgeAction::Replan:
    HandleDynamicReplanRequest(nav, "offroute_wedge");
    return TickStep::EndTick;
}
return TickStep::Next;
```

### 文件

- 一个模块一对文件。头文件只放结构体、`enum class` 和对外函数声明，看头文件就知道模块能干什么。
- 对外函数 PascalCase，写进头文件；内部函数 snake_case，放 `.cpp` 的匿名命名空间。
- `.cpp` 里内部小函数在前，对外函数在后，主函数放最后，读起来是一串步骤。

### 分层

- 编排层：`state_machine/`、`semantic/`、`zipline/zipline_action.cpp` 和导航入口 `navi_controller.cpp`。可以收 `Navigator*`，按顺序调用各模块。
- 模块层：其余全部。只收自己的结构体、显式传入的依赖（例如运动控制收 `InputBackend*`）和值，不认识 `Navigator`，结构体里也不存别的模块的指针。
- 把 `Navigator*` 传进模块层，等于把 `this` 换了个名字，审查时专门盯这一条。要动角色、会话的流程放在编排层。

### 共享状态

- 被两个以上文件写的字段只留一个写入函数，其他地方都调它，搜函数名就能找到全部写入点。例如阶段只经 `UpdatePhase` 改，传送等待只经 `BeginTransferWait` 开始。新加这种字段时，同时加它的写入函数。
- 跨导航保留的状态只在入口留一个实例（`MapNavigator.cpp` 的 `g_process_state`），用指针一路传下去。加载后只读的资源可以放进函数内 `static` 缓存并配一把锁，如 `planning/navmesh_cache.cpp`。

### 拆分

- 一个函数依次做几件不相干的事时才拆。一整段算法（循环、几何计算、拼 JSON）留在一起。
- 拆之前过三道刹车，任一条成立就不拆：拆出的函数要超过 4 个参数；要靠出参把局部变量传回；名字不比函数体好懂。只用一次、本身就清楚的一行赋值留在主函数。
- 主函数读成一串具名步骤。每步返回 `enum class`，第一个不是“继续”的结果就是整个函数的结果：

```cpp
TickStep run_navigate_steps(Navigator* nav, NavigateTick* tick)
{
    TickStep step = run_semantic_flow(nav);
    if (step != TickStep::Next) {
        return step;
    }
    step = CaptureNavigateFix(nav, tick);
    if (step != TickStep::Next) {
        return step;
    }
    // ...
    tick->heading_error = SteerAlongCorridor(nav, *tick);
    return finish_navigate_tick(nav, *tick);
}
```

### 保留的写法

- 守卫结构体：只用来在退出时收尾，相当于 Rust 的 `Drop`。字段是要收尾的对象的指针，不设默认值；拿到要收尾的东西之后立刻创建，创建时一次写全所有字段，不先默认构造再赋值。析构函数只有一行，名字写明退出时做什么：

    ```cpp
    struct LeaveNavigationOnExit
    {
        NavigatingTaskers* navigating;
        MaaTasker* tasker;

        ~LeaveNavigationOnExit() { LeaveNavigation(navigating, tasker); }
    };

    if (!TryEnterNavigation(&process->navigating, tasker)) {
        return false;
    }
    const LeaveNavigationOnExit leave_on_exit { &process->navigating, tasker };
    ```

- `sensing/roi_template_scanner` 自己持有工作线程，析构时停线程，保留类的写法。
- `std::any_of`、`std::erase_if` 这类标准算法配一行只读判断的 lambda 可以用。当局部小函数用的 lambda、会改外面变量的 lambda，改成具名函数。
- 不传存起来的回调：查停止传 `MaaContext*` 调 `TaskStopping`；原来靠回调带进去的东西，改成结构体字段或参数，例如取位用的 `CaptureParts`。
- 外部模块的接口照原样用：MapLocator、Navmesh、OpenCV、meojson、MaaFramework。meojson 输入结构体里的 `check_json`、`from_json` 是库要求的写法，保留。
- 标准库的锁、线程、原子量、容器照常用。

### 审查清单

- [ ] 结构体只放数据，函数是自由函数；要改的状态用指针传，只读的用 `const&`，看签名就知道改不改
- [ ] 函数只做名字说的事，没有顺带改别的状态
- [ ] 模块层没有收 `Navigator*`，结构体里没有存别的模块的指针
- [ ] 两个以上文件写的字段都经它的写入函数修改
- [ ] 没有 `bool` 模式参数、当局部函数用的 lambda、存起来的回调、新的可变全局变量
