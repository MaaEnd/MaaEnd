# MapNavigator Tool

MapNavigator 是用于 C++ MapNavigator 模块使用的地图路径录制与编辑的 Tk 工具，入口为 `main.py`。

当前支持：

- 通过统一的录制连接层在 `Win32` 与 `ADB` 之间切换。
- 录制地图路径并按区域切换浏览。
- 导入已有 JSON/JSONC，递归搜索可识别的 `path` 数据并显示。
- 导入时严格校验动作语义；未知动作会被拒绝，而不是静默降级。
- 在跨区域边界自动将前一区域的最后一个点和后一区域的第一个点标记为 `PORTAL`。
- 停止录制后只做 canonical 规范化整理，不会自动稀疏化或压缩路径点，**也无需进行手动稀疏化（无需减少打点）**。
- GUI 动作编辑主要面向坐标点动作：`RUN / SPRINT / JUMP / FIGHT / INTERACT / PORTAL / TRANSFER`。
- 支持为单个点标记 `strict`，用于要求该点必须精确抵达。
- 默认复制 `MapNavigator` 可直接粘贴的 canonical `path`：有 zone 时写 `ZONE` 无坐标声明节点，没有 zone 时保留纯坐标点数组。
- 支持独立的 `Assert 模式`：手动选择底图并框选矩形区域，导出 `MapLocateAssertLocation` 节点。

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

1. 打开工具。
2. 勾选顶部的 `Assert 模式`。
3. 在右侧下拉框里选择目标 `zone`。
4. 在底图上按住左键拖拽，框出一个矩形区域。
5. 点击 `复制 Assert`。

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

## 运行方式

### 1) 标准 Python

```powershell
cd tools/MapNavigator
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python main.py
```

### 2) uv

```powershell
cd tools/MapNavigator
uv run main.py
```

## 连接方式

工具顶部提供独立的“连接”配置区，录制前可先选择本次会话使用哪种控制器：

- `Win32 窗口`：通过窗口标题匹配当前 PC 版游戏窗口，默认标题为 `Endfield`。
- `ADB 设备`：通过 `adb devices -l` 枚举模拟器或真机，再连接指定序列号/地址。

### ADB 使用建议

1. 确保 `adb` 已安装，或在工具里手动指定 `adb` 可执行文件路径。
2. 点击 `刷新` 拉取设备列表。
3. 从设备下拉框中选择目标，或手动输入序列号 / `127.0.0.1:5555` 这类地址。
4. 再点击 `开始录制`。

工具会把最近使用的连接配置保存到用户目录下的本地设置文件，不会污染仓库工作区。

## 模块结构

- `main.py`: GUI 入口与 DPI 初始化。
- `app_tk.py`: UI 编排层（事件绑定、组件联动、状态展示）。
- `connection_models.py`: 录制会话、Win32/ADB 配置与设备模型。
- `connectors.py`: 录制连接器抽象，以及 Win32/ADB controller 建连实现。
- `settings_store.py`: 本地用户连接偏好持久化。
- `zone_index.py`: 当前区域索引与区域标签逻辑。
- `point_editing.py`: 点编辑领域逻辑（命中、插点、改动作、删点、拖拽）。
- `history_store.py`: 撤销/重做快照栈。
- `recording_service.py`: Maa Agent 录制线程与数据采集，不再直接耦合具体 controller 类型。
- `renderer_tk.py`: 地图底图异步渲染。
- `model.py`: 路径数据结构、动作类型与路径规范化工具。
- `runtime.py`: 项目路径定位与 maafw 运行时加载。
