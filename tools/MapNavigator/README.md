# MapNavigator Tool

MapNavigator 是用于 C++ MapNavigator 模块使用的地图路径录制与编辑的 Tk 工具，入口为 `main.py`。

当前支持：

- 录制地图路径并按区域切换浏览。
- 导入已有 JSON/JSONC，递归搜索可识别的 `path` 数据并显示。
- 在跨区域边界自动将前一区域的最后一个点和后一区域的第一个点标记为 `PORTAL`。
- 支持为单个点标记 `strict`，用于要求该点必须精确抵达。
- 默认复制 `MapNavigator` 可直接粘贴的 canonical `path`：有 zone 时写 `ZONE` 无坐标声明节点，没有 zone 时保留纯坐标点数组。

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
- 复制出来的内容可以直接粘贴到 pipeline 的 `custom_action_param.path`。

## 运行方式

### 1) 标准 Python

```powershell
cd tools/maplocator
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python main.py
```

### 2) uv

```powershell
cd tools/maplocator
uv run main.py
```

## 模块结构

- `main.py`: GUI 入口与 DPI 初始化。
- `app_tk.py`: UI 编排层（事件绑定、组件联动、状态展示）。
- `zone_index.py`: 当前区域索引与区域标签逻辑。
- `point_editing.py`: 点编辑领域逻辑（命中、插点、改动作、删点、拖拽）。
- `history_store.py`: 撤销/重做快照栈。
- `recording_service.py`: Maa Agent 录制线程与数据采集。
- `renderer_tk.py`: 地图底图异步渲染。
- `model.py`: 路径数据结构、动作类型与轨迹简化算法。
- `runtime.py`: 项目路径定位与 maafw 运行时加载。
