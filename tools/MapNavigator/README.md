# MapNavigator Tool

MapNavigator 是用于 C++ MapNavigator 模块使用的地图路径录制与编辑工具，采用 Web 架构：本地 FastAPI 后端（仅监听 `127.0.0.1`）+ 浏览器前端（原生 JS + WebGL，无构建步骤）。入口为 `main.py`，运行后自动打开浏览器页面。

本工具与 MapNavigator 组件配套使用：工具负责产出坐标，组件负责执行移动。**导出的配置如何接入 Pipeline，见[开发手册 - MapNavigator 寻路系统](../../docs/zh_cn/developers/components/map-navigator.md)。**

当前支持：

- 通过统一的录制连接层在 `Win32` 与 `ADB` 之间切换。
- 录制地图路径并按区域切换浏览。
- 导入已有 JSON/JSONC，递归搜索可识别的 `path` 数据并显示。
- 导入 `MapTrackerMove` / `MapTrackerAssertLocation` 时自动按兼容表转换到 `MapNavigator` / `MapLocateAssertLocation` 的 Base 坐标系。
- 导入时严格校验动作语义；未知动作会被拒绝，而不是静默降级。
- 在跨区域边界自动将前一区域的最后一个点和后一区域的第一个点标记为 `PORTAL`。
- GUI 动作编辑主要面向坐标点动作：`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER / COLLECT / DIG`。
- `COLLECT / DIG` 是采集/挖掘语义点：精确抵达后由 `MapNavigator` 同步触发 `AutoCollectClickStart` / `AutoCollectDigStart` pipeline 子任务，期间不退出 NaviController，避免每次采集都重建定位/重新 Bootstrap/吃掉起步宽限。
- 支持为单个点标记 `strict`，用于要求该点必须精确抵达。
- 默认复制 `MapNavigator` 可直接粘贴的 canonical `path`：有 zone 时写 `ZONE` 无坐标声明节点，没有 zone 时保留纯坐标点数组。
- 支持独立的 `Assert 模式`：手动选择底图并框选矩形区域，导出 `MapLocateAssertLocation` 节点。
- 支持 `A* 模式`：加载 BaseNav `.nav` / `.nav.gz` 后选择起点和终点，在 GUI 上显示计算路线。

当前需要注意：

- `HEADING` 是无坐标控制节点，不属于 GUI 常规点编辑与导出模型，建议在导出 `path` 后手工补回或维护。
- 运行时 `sprint_threshold` 的语义是“前方连续可跑段长度阈值”，不是只看当前点距离。

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
    ]
]
```

- `ZONE` 是可选的无坐标声明节点，用于给后续点提供区域校验信息。
- 普通坐标点继续使用 `[x, y]` / `[x, y, "ACTION"]`。
- 严格点会导出为 `[x, y, true]` 或 `[x, y, "ACTION", true]`。
- 当前 GUI 导出的 canonical `path` 只覆盖坐标点与 `ZONE` 声明，不会直接生成 `HEADING` 这类无坐标控制节点。
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
4. 点击 `复制断言 JSON`。

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

`.nav` 只连接 GLB 自身共享/重叠边，以及同高度的小距离 component bridge；不会为了跨 level 自动补 portal 或 drop link。游戏本身分离的 level 暂保持不可达。

## 运行方式

### 1) uv（推荐，依赖声明在 `main.py` / `web/serve.py` 的 PEP 723 头里）

```powershell
cd tools/MapNavigator
uv run main.py
```

### 2) 标准 Python

```powershell
cd tools/MapNavigator
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python main.py
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
- `basenav_preview.py`: BaseNav `.nav` 加载与 A\* 路线预览计算。
- `json_import.py`: JSON/JSONC 导入解析与动作语义校验。
- `maptracker_compat.py`: `MapTracker*` 节点到 Base 坐标系的兼容转换表。
- `key_listener.py`: 录制期间的全局按键监听与系统权限检查。
- `model.py`: 路径数据结构、动作类型与路径规范化工具。
- `runtime.py`: 项目路径定位与 maafw 运行时加载。
