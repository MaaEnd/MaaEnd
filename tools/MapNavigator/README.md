# MapNavigator Tool

MapNavigator 是用于 C++ MapNavigator 模块使用的地图路径录制与编辑工具，采用 Web 架构：本地 FastAPI 后端（仅监听 `127.0.0.1`）+ 浏览器前端（原生 JS + WebGL，无构建步骤）。入口为 `main.py`，运行后自动打开浏览器页面。

本工具与 MapNavigator 组件配套使用：工具负责产出坐标，组件负责执行移动。**导出的配置如何接入 Pipeline，见[开发手册 - MapNavigator 寻路系统](../../docs/zh_cn/developers/components/map-navigator.md)。**

当前支持：

- 通过统一的录制连接层在 `Win32`、`ADB` 与 `Linux-Gamescope` 之间切换。
- 录制地图路径并按区域切换浏览。
- 导入已有 JSON/JSONC，递归搜索可识别的 `path` 数据并显示。
- 在跨区域边界自动将前一区域的最后一个点和后一区域的第一个点标记为 `PORTAL`。
- GUI 动作编辑主要面向坐标点动作：`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`。
- `COLLECT / DIG` 是采集/挖掘语义点：精确抵达后由 `MapNavigator` 同步触发 `AutoCollectClickStart` / `AutoCollectDigStart` pipeline 子任务，期间不退出 NaviController，避免每次采集都重建定位/重新 Bootstrap/吃掉起步宽限。
- 支持为单个点标记 `strict`，用于要求该点必须精确抵达。
- 支持为单个点标记 `required`。未启用滑索时按作者顺序执行全部节点；启用滑索后，中间的移动和兜底点可被全局规划跳过，`required` 用于标记必须抵达并执行的点。`ZONE`、`HEADING`、`COLLECT`、`DIG` 始终保留。
- 支持为单个坐标点声明 `target_tier`；它只指定该点的坐标来源层级，不会改变 `ZONE` 或触发区域切换。
- 默认复制 `MapNavigator` 可直接粘贴的 canonical `path`：有 zone 时写 `ZONE` 无坐标声明节点，没有 zone 时保留纯坐标点数组。
- 支持独立的 `Assert 模式`：手动选择底图并框选矩形区域，导出 `MapLocateAssertLocation` 节点。
- 支持 `A* 模式`：加载 BaseNav `.nav` / `.nav.gz` 后选择起点和终点，在 GUI 上显示计算路线；在 X / Y 任一坐标框粘贴 `[x, y]` 数组可自动回填两项，再点击 `标点` 显示目标预览点。
- 支持只读的 `日志分析` 模式：浏览器本地导入一个或多个 `cpp-algo/debug/maafw*.log`，也可直接选择 MaaEnd 的 `-partNN.zip` 独立分包；工具逐条解压 cpp-algo 日志并读取同组 `record/Ziplines.json`，在对应底图上对照路线决策、实测轨迹、本次选择的滑索架和快照中的候选滑索架。
- 在单次定位和录制过程中都会显示 MapLocator 返回的实时坐标、区域和角色朝向；单次定位参考点还会在底图上绘制朝向箭头。

页面分为两个顶层工作区：`路线制作` 内含 A* 寻路、断言模式和路径编辑，并统一提供连接、实时位置与实机试跑；`日志分析` 是纯离线只读工作区，进入后隐藏这些实机能力和路线制作页签，同时暂停连接状态探测。切回路线制作时会恢复离开前使用的页签和编辑状态。

三个路线制作页签都可通过 `选择项目节点` 扫描当前项目 `assets`：路径编辑载入 `MapNavigateAction` 继续编辑；A* 将同一底图中的坐标按原顺序作为目标点，导入一个或多个目标后都需要先在地图上单击补充手动起点，随后自动规划“手动起点 → 导入点 1 → 导入点 2…”；断言模式默认选择 `MapLocateAssertLocation`，也可切换到 `参考路线` 查看导航路径。列表支持按资源路径、节点名或区域搜索；每个页签都并列提供 `读取剪贴板` 按钮，可自动识别完整 Pipeline、单个导航或断言节点、`{"path": [...]}` 与纯 `path` 数组。导入时会严格校验动作语义，未知动作会直接拒绝，不会静默降级。

当前需要注意：

- `HEADING` 是无坐标控制节点，不属于 GUI 常规点编辑与导出模型，建议在导出 `path` 后手工补回或维护。
- 运行时 `sprint_threshold` 的语义是“前方连续可跑段长度阈值”，不是只看当前点距离。

## Linux-Gamescope 连接

- 基于 maafw `LinuxController`（PipeWire 截屏 + libei 输入），从当前发现的 gamescope 实例里选一个即可录制，不再需要手填 socket 路径。
- 需要游戏跑在 gamescope 上；下拉列表展示 `Toolkit.find_gamescope_instances()` 发现的实例（display 编号 + PipeWire 节点 + EIS socket）。
- 仅录制截图，不投递输入；`screencap_method`/`input_method` 与 `assets/interface.json` 的 `Linux-Gamescope` 控制器条目保持一致。
- 依赖 `maafw==5.13.0b4`（含 `LinuxController` 绑定；`5.12.x` 没有该控制器类）。

## 位置与朝向

`A* 寻路` 和 `断言模式` 的“标出游戏内当前位置”按钮会临时连接游戏，连续取得 3 个有效定位帧后显示第 3 帧的坐标与朝向。底部罗盘按 MapLocator 约定显示精确角度和八方位文字：`0°` 为北、`90°` 为东，角度顺时针增加；地图参考点上的青色箭头表示同一朝向。

在 `路径编辑` 模式开始录制后，底部读数会以固定频率刷新位置与朝向。定位短暂丢失时保留最后一次真实观测，不使用轨迹预测补出虚拟位置。

## 复制格式

复制到剪贴板的内容是 `path` 本体，可直接粘贴到 `MapNavigator` 的 `custom_action_param.path`。其结构与加载格式保持一致：

```json
[
    {
        "action": "ZONE",
        "zone_id": "map01_lv002"
    },
    [
        688,
        350
    ],
    [
        700,
        350,
        true
    ],
    [
        720,
        350,
        "SPRINT"
    ],
    {
        "action": "RUN",
        "target": [
            243.49,
            177.53
        ],
        "target_tier": "Wuling_L4_328"
    },
    [
        760,
        352,
        "PORTAL"
    ],
    {
        "action": "ZONE",
        "zone_id": "map01_lv003"
    },
    [
        45,
        120,
        "PORTAL"
    ],
    [
        933,
        650,
        "COLLECT"
    ],
    [
        940,
        655,
        "DIG"
    ],
    {
        "action": "NAVMESH",
        "target": [
            1242.04,
            773.41
        ]
    }
]
```

- `ZONE` 是可选的无坐标声明节点，用于给后续点提供区域校验信息。
- 普通坐标点继续使用 `[x, y]` / `[x, y, "ACTION"]`。
- 普通点的坐标来自 tier 底图时，在路点属性中填写 `坐标层级`，导出为 `{ "action": "ACTION", "target": [x, y], "target_tier": "..." }`；留空仍使用旧数组格式。
- `target_tier` 只解释当前点的坐标系，与负责区域校验上下文的 `ZONE` 相互独立。
- 严格点会导出为 `[x, y, true]` 或 `[x, y, "ACTION", true]`。
- 必经点会改用对象格式并导出 `"required": true`；该字段与只控制到点精度的 `strict` 相互独立。
- `NAVMESH` 点会导出为 `{ "action": "NAVMESH", "target": [x, y] }`，由运行时从当前位置自动寻路到目标。
- 当前 GUI 导出的 canonical `path` 覆盖坐标点（`NAVMESH` 按对象格式导出）与 `ZONE` 声明，不会直接生成 `HEADING` 这类无坐标控制节点。
- 复制出来的内容可以直接粘贴到 pipeline 的 `custom_action_param.path`。

## Assert 模式

除了录制 `path` 以外，工具现在还支持导出 `MapLocateAssertLocation` 节点。

适用场景：

- 进入某段导航前，先判断人物是否已经站在预期区域内。
- 需要对某个 zone 的局部矩形范围做纯判定。
- 只想复用 `MapLocator` 当前的定位结果，不额外跑一趟导航。

### 使用方式

1. 打开工具，切换到 `断言模式` 页签。
2. 点击 `选择断言底图与层级`，选择目标 `zone`。
3. 在底图上按住左键拖拽，框出一个矩形区域。
4. 选择复制完整断言节点或仅复制环境监测 `routes.json` 使用的 `NavAssert` 坐标，再点击复制按钮。

### 导出格式

复制到剪贴板的是完整节点 JSON，可直接粘贴进 pipeline：

```json
{
    "NodeName": {
        "recognition": "Custom",
        "custom_recognition": "MapLocateAssertLocation",
        "custom_recognition_param": {
            "zone_id": "Wuling_Base",
            "target": [
                605,
                878,
                60,
                20
            ]
        },
        "action": "DoNothing"
    }
}
```

- `zone_id`: 需要命中的区域名。
- `target`: `[x, y, w, h]`，表示矩形判定区域。
- 该节点是纯判定 recognition，不负责移动。

## A\* 模式

该模式用于直接查看 BaseNav `.nav` 路线结果，不会修改当前录制路径。

### 使用方式

1. 打开工具，默认进入 `A* 寻路` 页签；BaseNav 在后台自动加载，优先默认 `base.nav.gz`，缺失时回退 `base.nav`。
2. 点击 `选择底图与层级`，选择用于显示的底图和 BaseNav zone。
3. 在底图或红色三角面区域上左键点击起点，再点击终点（`单段 A*` 为两点直连，`多段 A*` 可连续追加途经点）。
4. 查看绿色连线与标点结果。

`Delete` 或 `清除预览` 会清空当前 A\* 预览。

BaseNav 用于直接从 GLB 三角面生成寻路数据。它不是展示图，而是可直接做 A\* 的三角拓扑图，内部 magic 为 `BNAV`。

默认读取：

```text
assets/resource/model/map/navmesh/base.nav.gz
assets/resource/model/map/navmesh/base.nav      # optional local fallback
```

寻路要求包内带预烘格图段（`BGRD`，见 BaseNav 版本 4）；没有这一段的包仍可显示网格与底图，但点不出线路。

可选 zone：

```text
map01base
map02base
base01
dung01
```

四个 zone 都会直接落到对应底图：`ValleyIV/Base.png`、`Wuling/Base.png`、`OMVBase/OMVBase01.png`、`Dung/Dung01Base.png`。

在 A\* 模式点击目标点后，可以点击 `复制路径` 复制目标式 `MapNavigateAction` 参数。该参数使用语义动作 `NAVMESH`，运行时会从当前定位位置自动寻路到 `target`，不需要手工维护 `path`：

```json
{
    "action": "NAVMESH",
    "target": [
        720,
        630
    ]
}
```

`NAVMESH` 的 `.nav` 区域由运行时根据当前定位自动推断；复制结果不需要填写 `zone_id` / `navmesh_zone`。复制结果可直接作为环境监测 `routes.json` 中 `NavPath` 的 NAVMESH 动作。

`.nav` 只连接 GLB 自身共享/重叠边，以及同高度的小距离 component bridge；不会为了跨 level 自动补 portal 或 drop link。游戏本身分离的 level 暂保持不可达。

## 日志分析

该模式用于解释运行时为什么选择步行或滑索，不修改路线，也不连接游戏。

1. 切到顶层的 `日志分析` 工作区；该工作区不会显示连接与实机试跑设置。
2. 直接选择同一 MaaEnd 日志包的全部 `-partNN.zip`，或导入解压后的一个或多个 `cpp-algo/debug/maafw*.log`。ZIP 分包会按去掉 `partNN` 的前缀合并，并检查分包编号是否连续。
3. 按时间、节点名或文件名筛选并选择一次 `MapNavigateAction`。
4. 页面会自动切到日志中的 BaseNav 区域并缩放到相关路径；侧栏逐段列出步行成本、滑索成本、节省量和运行时原始 `why=` 原因。
5. 默认单击地图上的作者路径点、规划点、实测点或滑索架后，地图右上方才会出现点位详情浮动卡，列出来源、序号、坐标与相应运行信息；拖动地图仍是平移。
6. 需要测量滑索架间距时，先点击地图右上角的测距工具；测距浮动卡会随即出现，再依次选择 A、B 两座滑索架。地图会画出测距虚线，浮动卡按运行时同款占地算法列出可能中心跨度范围、原始锚点距离、水平距离范围、高差、底图像素距离和几何连接判定。再次单击 A 或点击「清除」可取消选择；退出测距工具会隐藏浮动卡但保留已有结果，重新打开即可继续查看。

地图标注含义：

- 白色虚线：请求参数中的作者路径提示。
- 粉色实线：运行时实际采用的步行规划。
- 绿色实线：`PositionProvider::Capture` 中 MapLocator 成功且非保持的实测地面轨迹；滑索飞行和定位空窗会切断折线，不会用推测位置补齐。
- 琥珀色虚线：规划器计算过、但因选择滑索而未采用的步行基线。
- 青色箭头：日志已记录发射、并由 `ride landed` 确认落地的滑索跳。
- `索1、索2……` 圆点：运行日志中可定位的本次选择滑索架。青色实心表示实际已经过，琥珀空心表示日志未确认经过；点中后出现的浮动卡同时列出 `[x, y, height]`，高度会优先从同组 ZIP 快照的同位置标记补齐。
- 紫色菱形：同组 ZIP 的 `record/Ziplines.json` 经仓库标定投影后的候选滑索架。深色描边和白色中心点用于在浅色底图上保持清晰；这里只按地图与滑索模板类型筛选，可能仍包含未供电、楼层不符、不可达或被成本规则排除的架子。
- 点位十字外圈与标题：默认查看工具当前选中的点位；它与滑索架测距是两套独立状态。
- A / B 外圈与两点间虚线：开启右上角测距工具后手动选择的测距对象。森空岛记录的是随朝向变化的角格锚点，因此主结果按两端 `footprint` 显示可能中心的世界三维跨度范围（米）；高度差原样参与，不应用占地偏移。与运行时一致，同类型、同 `level_id` 且范围下界不超过该类型 `max_span` 时只标为“几何上可连接”。这不代表供电、可达性、禁用边与成本规划均已通过，实际能否采用仍以运行时决策为准。
- 青色虚线：接近上索点与离开末端滑索的端点连接估计。当前日志没有记录这两段的完整折线，因此它们不是角色逐帧轨迹。

运行日志不会为分析功能额外输出完整滑索快照：本次选择只使用既有的首尾架、实际发射落点和落地确认，未执行链的中间架可能只能通过候选背景点与架数判断。ZIP 只解压 `cpp-algo/debug/maafw*.log` 和 `record/Ziplines.json`，逐条解析后释放日志文本；其他附件会被跳过。所有日志和滑索快照均由浏览器本地读取，不会上传到后端。日志模式只提供只读点位查看、测距、缩放和平移，也不会向实机试跑装载路线。

## 实机试跑

`路线制作` 工作区中的“实机试跑”面板对三个制作页签可用，跑的永远是**当前页签里那一份**：路径编辑跑编辑器路点，A\* 跑「复制路径」会给出的那串 `NAVMESH` 目标（同一处口径，两者不会漂移），断言模式跑的是画出来的那个框——原样交给 `MapLocateAssertLocation` 认一次，人在框里就提示通过、不在就提示不通过，不加额外判定。改完自动重新装载，不必手动同步。切到 `日志分析` 工作区后，连接与试跑面板会隐藏且连接状态探测暂停。

- **开始试跑 (F3)**：没有会话时连接游戏（Windows 非管理员会先请求提权），连上立即开跑；会话还在时直接重跑。
- **终止试跑 (F4)**：正在跑就停下这一轮，人立即松手；已经停着则结束会话、放开游戏。

会话在两轮之间保持连接，所以改完线按 F3 就能再跑一遍，不需要重连。F3/F4 由后端全局监听，游戏窗口在前台时同样有效；提权失败退回本进程时，面板提示会变成警告，此时只能用按钮终止。

A\* 的起点只参与预览规划，不会进 `NAVMESH` 节点；实机是从角色当前位置直接走向第一个目标点，与复制出来的配置一致。

## 运行方式

依赖声明在 `main.py` / `web/serve.py` 的 PEP 723 头里，uv 会自动准备对应环境：

```powershell
cd tools/MapNavigator
uv run main.py
```

启动后服务监听 `http://127.0.0.1:8770`（仅本机，不暴露局域网）并自动打开浏览器。**端口被占用时会自动顺延到下一个可用端口**（最多试 20 个，仍全占用则由系统分配），控制台会打印 `[Backend] 服务地址: ...`，浏览器也会打开实际地址。环境变量：`MAPNAV_PORT` 指定首选端口（被占用时同样顺延）；`MAPNAV_NO_BROWSER=1` 只起服务不开浏览器。也可以直接 `uv run web/serve.py`（完全等价）。

## 连接方式

左侧提供独立的“连接方式”面板，录制前可先选择本次会话使用哪种控制器：

- `Windows 窗口句柄`：通过窗口标题匹配当前 PC 版游戏窗口，默认标题为 `Endfield`。
- `Android ADB 桥接`：通过 `adb devices -l` 枚举模拟器或真机，再连接指定序列号/地址。
- `PlayCover (macOS)`：通过 PlayTools 服务地址 + 应用 UUID 连接 macOS 上运行的 PlayCover 实例。

### ADB 使用建议

1. 确保 `adb` 已安装，或在工具里手动指定 `adb` 可执行文件路径。
2. 点击 `检测并刷新设备` 拉取设备列表。
3. 从设备下拉框中选择目标，或手动输入序列号 / `127.0.0.1:5555` 这类地址。
4. 再点击 `开始录制`。

工具会把最近使用的连接配置保存到用户目录下的本地设置文件，不会污染仓库工作区。

## 模块结构

- `main.py`: 入口，拉起 web 后端并自动打开浏览器。
- `web/serve.py`: FastAPI 后端（静态站点托管、寻路/导入导出/设置 API、录制 WebSocket 桥），仅监听 `127.0.0.1`。
- `web/static/`: 浏览器前端（原生 JS + JSDoc + WebGL，ESM 模块，零构建）。
- `connection_models.py`: 录制会话、Win32/ADB/PlayCover 配置与设备模型。
- `connectors.py`: 录制连接器抽象，以及各 controller 建连实现。
- `settings_store.py`: 本地用户连接偏好持久化。
- `agent_session.py`: 一次 cpp Agent + Tasker 的生命周期，录制/试跑/单次定位/navmesh 查询共用同一套起法。
- `recording_service.py`: Maa Agent 录制线程与数据采集，不直接耦合具体 controller 类型。
- `navtest_service.py`: 实机试跑会话，把当前页签里的路线交给 `MapNavigateAction` 走一遍、断言框交给 `MapLocateAssertLocation` 认一次，F3 重跑 / F4 终止。
- `session_worker.py`: 提权会话子进程。Windows 非管理员时录制与试跑跑在这里，与后端用回连 socket 通信（服务自身不重启、不提权）。
- `session_modes.py`: 录制与试跑的差异表（服务怎么建、哪些消息算终结），进程内会话与子进程共用。
- `clipboard.py`: 系统剪贴板写入（G 热键复制坐标）。
- `navmesh_backend.py`: navmesh 查询后端，把 cpp-algo agent 当作常驻查询进程；几何解码、吸附、路线都在 agent 里算。
- `json_import.py`: JSON/JSONC 导入解析与动作语义校验。
- `key_listener.py`: 录制期间的全局按键监听与系统权限检查。
- `model.py`: 路径数据结构、动作类型与路径规范化工具。
- `runtime.py`: 项目路径定位与 maafw 运行时加载。
