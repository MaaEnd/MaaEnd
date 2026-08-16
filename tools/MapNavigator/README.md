# MapNavigator Tool

MapNavigator 是用于 C++ MapNavigator 模块使用的地图路径录制与编辑工具，采用 Web 架构：本地 FastAPI 后端（仅监听 `127.0.0.1`）+ 浏览器前端（原生 JS + WebGL，无构建步骤）。入口为 `main.py`，运行后自动打开浏览器页面。

本工具与 MapNavigator 组件配套使用：工具负责产出坐标，组件负责执行移动。**导出的配置如何接入 Pipeline，见[开发手册 - MapNavigator 寻路系统](../../docs/zh_cn/developers/components/map-navigator.md)。**

当前支持：

- 通过统一的录制连接层在 `Win32`、`ADB` 与 `WlRoots`（Linux Wayland）之间切换。
- 录制地图路径并按区域切换浏览。
- 导入已有 JSON/JSONC，递归搜索可识别的 `path` 数据并显示。
- 导入 `MapTrackerMove` / `MapTrackerAssertLocation` 时自动按兼容表转换到 `MapNavigator` / `MapLocateAssertLocation` 的 Base 坐标系。
- 导入时严格校验动作语义；未知动作会被拒绝，而不是静默降级。
- 在跨区域边界自动将前一区域的最后一个点和后一区域的第一个点标记为 `PORTAL`。
- GUI 动作编辑主要面向坐标点动作：`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`。
- `COLLECT / DIG` 是采集/挖掘语义点：精确抵达后由 `MapNavigator` 同步触发 `AutoCollectClickStart` / `AutoCollectDigStart` pipeline 子任务，期间不退出 NaviController，避免每次采集都重建定位/重新 Bootstrap/吃掉起步宽限。
- 支持为单个点标记 `strict`，用于要求该点必须精确抵达。
- 支持为单个坐标点声明 `target_tier`；它只指定该点的坐标来源层级，不会改变 `ZONE` 或触发区域切换。
- 默认复制 `MapNavigator` 可直接粘贴的 canonical `path`：有 zone 时写 `ZONE` 无坐标声明节点，没有 zone 时保留纯坐标点数组。
- 支持独立的 `Assert 模式`：手动选择底图并框选矩形区域，导出 `MapLocateAssertLocation` 节点。
- 支持 `A* 模式`：加载 BaseNav `.nav` / `.nav.gz` 后选择起点和终点，在 GUI 上显示计算路线。
- 单次定位与录制期间都会显示 MapLocator 返回的实时坐标、区域和角色朝向；单次定位参考点还会在底图上绘制朝向箭头。

当前需要注意：

- `HEADING` 是无坐标控制节点，不属于 GUI 常规点编辑与导出模型，建议在导出 `path` 后手工补回或维护。
- 运行时 `sprint_threshold` 的语义是“前方连续可跑段长度阈值”，不是只看当前点距离。

## WlRoots 连接（Linux Wayland）

- 游戏合成器需支持 `wlr-screencopy-unstable-v1` 协议；建议游戏跑在嵌套合成器（如 gamescope）上，**不要连接当前桌面会话的 socket**（连错会截到桌面而不是游戏）。
- socket 路径是完整路径（如 `/run/user/1000/wayland-0`），不一定是 `$WAYLAND_DISPLAY` 指向的那个；下拉候选来自 `$XDG_RUNTIME_DIR` 下的实际 socket 枚举。

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
- `NAVMESH` 点会导出为 `{ "action": "NAVMESH", "target": [x, y] }`，由运行时从当前位置自动寻路到目标。
- 当前 GUI 导出的 canonical `path` 覆盖坐标点（`NAVMESH` 按对象格式导出）与 `ZONE` 声明，不会直接生成 `HEADING` 这类无坐标控制节点。
- 复制出来的内容可以直接粘贴到 pipeline 的 `custom_action_param.path`。

## Assert 模式

除了录制 `path` 以外，工具现在还支持导出 `MapLocateAssertLocation` 节点。

适用场景：

- 进入某段导航前，先判断人物是否已经站在预期区域内。
- 需要对某个 zone 的局部矩形范围做纯判定。
- 不希望引入 `MapTracker`，只想复用 `MapLocator` 当前的定位结果。

### 使用方式

1. 打开工具，切换到 `断言模式` 页签。
2. 点击 `选择断言底图与层级`，选择目标 `zone`。
3. 在底图上按住左键拖拽，框出一个矩形区域。
4. 选择复制完整断言节点或仅复制环境监测 `routes.json` 使用的 `MapAssert` 坐标，再点击复制按钮。

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

在 A\* 模式点击目标点后，可以点击 `复制 JSON 配置` 复制目标式 `MapNavigateAction` 参数。该参数使用语义动作 `NAVMESH`，运行时会从当前定位位置自动寻路到 `target`，不需要手工维护 `path`：

```json
{
    "action": "NAVMESH",
    "target": [
        720,
        630
    ]
}
```

`NAVMESH` 的 `.nav` 区域由运行时根据当前定位自动推断；复制结果不需要填写 `zone_id` / `navmesh_zone`。

复制内容可切换为 `仅坐标（MapTarget）`，此时只复制最后一个目标点的 `[x, y]`，可直接粘贴到环境监测 `routes.json` 的 `MapTarget`。分层底图的状态栏会同时提示应填写的 `MapTargetTier`。

`.nav` 只连接 GLB 自身共享/重叠边，以及同高度的小距离 component bridge；不会为了跨 level 自动补 portal 或 drop link。游戏本身分离的 level 暂保持不可达。

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
- `recording_service.py`: Maa Agent 录制线程与数据采集，不直接耦合具体 controller 类型。
- `record_worker.py`: 提权录制子进程。Windows 非管理员时录制跑在这里，与后端用回连 socket 通信（服务自身不重启、不提权）。
- `clipboard.py`: 系统剪贴板写入（G 热键复制坐标）。
- `basenav_preview.py`: BaseNav `.nav` 加载与 A\* 路线预览计算。
- `json_import.py`: JSON/JSONC 导入解析与动作语义校验。
- `maptracker_compat.py`: `MapTracker*` 节点到 Base 坐标系的兼容转换表。
- `key_listener.py`: 录制期间的全局按键监听与系统权限检查。
- `model.py`: 路径数据结构、动作类型与路径规范化工具。
- `runtime.py`: 项目路径定位与 maafw 运行时加载。
