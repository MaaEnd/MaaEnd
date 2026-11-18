# RecoGrid / GridTracker / EssenceGrid 架构指南

基质库存扫描由三个可独立理解的 C++ 模块组成，不再由单体引擎包办识别、追踪和业务流程：

```mermaid
flowchart LR
    P["Pipeline 截图"] --> E["EssenceGrid 业务实例"]
    E --> R["RecoGrid 单帧识别"]
    R --> T["GridTracker 跨帧追踪"]
    T --> E
    E --> C["Pipeline 点击格子"]
    C --> G["Go OCR 与匹配决策"]
    G --> A["Pipeline 锁定 / 废弃 / 跳过"]
```

## 模块边界

| 模块 | 负责 | 不负责 |
| --- | --- | --- |
| `RecoGrid` | 单张截图中的行列、格子矩形、占用状态、pHash 和比较特征 | 历史帧、滚动顺序、业务品质、点击 |
| `GridTracker` | 相邻帧对齐、全局行列、去重、顺序和结束确认 | 图像分割、模板加载、Pipeline 路由 |
| `EssenceGrid` | 基质品质、锁定/废弃缩略图、待处理队列和 Maa 回调 | 技能 OCR、匹配规则、实际 UI 操作 |
| Go `essencefilter` | OCR 归一化、目标组合匹配、Lock/Discard/Skip 决策 | 网格识别、点击按钮 |
| Pipeline | 截图时机、点击、滑动、等待、操作后确认 | 图像算法和匹配数据结构 |

源码旁还有更精确的模块文档：

- [RecoGrid README](../../../../agent/cpp-algo/source/RecoGrid/README.md)
- [GridTracker README](../../../../agent/cpp-algo/source/GridTracker/README.md)
- [EssenceGrid README](../../../../agent/cpp-algo/source/EssenceGridScan/README.md)
- [EssenceFilter Go README](../../../../agent/go-service/essencefilter/README.md)

## RecoGrid：只识别一帧

公共入口：

```cpp
recogrid::GridFrame RecognizeGrid(
    const cv::Mat& image,
    const recogrid::GridRecognitionOptions& options = {});
```

识别过程：

1. 把截图映射到 `normalizedSize`。
2. 裁剪 `roi`，通过行列投影提取 segment。
3. 组合出规则格子并换算回原截图坐标。
4. 对每个格子应用固定装饰 mask。
5. 计算占用状态、pHash 和紧凑颜色特征。

### 主要输入

| 字段 | 含义 |
| --- | --- |
| `detect.normalizedSize` | 坐标基准，MaaEnd 通常使用 `1280x720` |
| `detect.roi` | 网格区域 |
| `rowThresholdRatio` / `colThresholdRatio` | 行列投影阈值 |
| `minRawSegmentLength` | 原始 segment 最小长度 |
| `minKeptSegmentRatio` | 相对主流格子尺寸的保留比例 |
| `lockedRowHeight` / `lockedColWidth` | 首帧后锁定的格子尺寸，用于稳定后续检测 |
| `mask` | 忽略格子内固定角标或底部区域 |
| 占用亮度参数 | 判断格子是否有内容 |

### 主要输出

`GridFrame` 包含行列数、主流格子宽高、诊断信息和 `GridCell` 列表。每个 `GridCell` 包含局部行列、当前帧 index、屏幕矩形、特征以及 `occupied`。

RecoGrid 不加载物品分类模板，也不保存 session。空图、无效通道或没有检测到网格时返回空 `GridFrame`，调用方决定如何重试。

## GridTracker：跨帧顺序与结束

公共入口：

```cpp
gridtracker::Result observe(
    const recogrid::GridFrame& frame,
    const gridtracker::Options& options);
```

一个滚动任务持有一个 `GridTracker`。新任务开始时调用 `reset()`。

### 状态

| 状态 | 含义 | EssenceGrid 路由 |
| --- | --- | --- |
| `Initial` | 已验证顶端，建立首屏坐标 | 处理新格 |
| `Advanced` | 新增了可定位的格位 | 处理新格 |
| `Stalled` | 与上一页对齐但没有新增格位 | 短滑一次，仍无进展则失败 |
| `Complete` | 已确认底部且不同格位数等于库存数量 | 处理剩余队列，然后独立复核 |
| `Rejected` | 起点、网格或重叠关系未能验证 | 起始时可回顶，对齐丢失时可回滑找重叠 |

### 对齐与去重

使用控制器对应的首格、格距和滚动条位置确定布局。完整卡片的轮廓提供行位置，每一列独立判断占用。滚动条位置限定可能的绝对行号。滚动条和数量只允许一个行偏移时，一整行各列核心一致即可确认重叠。存在多个可能偏移时需要至少两行和唯一的图案匹配，否则保留原进度。只有符合库存数量的连续占用位置才进入全局坐标表。

### 完成条件

起点已确认，各页之间有连续重叠，每个库存格位都有识别结果。符合跳过选项的格位可直接记账。需要处理的格位在确认选中并从详情流程成功返回后记账。到达底部时，再次读取数量、检查底部和当前页，已处理格位数须等于库存数量。单屏库存可以在首屏完成。

仓库盘点与普通筛选会启用 `collect_inventory`，逐格读取两种品质和全部标记。`selected_for_filter` 决定这一格是否进入标记流程。Go 在提交前再次核对已识别件数与库存总数。

`EssenceFilterMain` 至少要有一个已启用的扫描模式成功结束。盘点和筛选同时启用时，先完成盘点，再开始筛选。任一阶段失败都会让整项任务失败。

## EssenceGrid：基质业务实例

`main.cpp` 创建一个 `EssenceGrid`，并通过 `trans_arg` 同时注册：

- `EssenceGridAdvanceRecognition`
- `EssenceGridPendingRecognition`
- `EssenceGridResetAction`

实例持有 Tracker、任务 id、合并后的配置、缩略图模板、当前页队列和 pending cell。任务 id 变化时会重置 Tracker 与队列；Pipeline 执行 `EssenceGridResetAction` 时也会立即清空这些追踪状态，让下一次 `EssenceGridAdvanceRecognition` 从头扫描，已加载的模板与配置保留。

### `EssenceGridAdvance.attach`

| 字段 | 用途 |
| --- | --- |
| `roi`、`normalized_size`、`first_cell`、`row_pitch`、`scrollbar_track` | 定位卡片和滚动条 |
| `preview_core` | 详情面板中央的基质图案 |
| `thumb_lock_template_paths` | 识别已锁定缩略图 |
| `thumb_discard_template_path` | 识别已废弃缩略图 |
| `flawless_essence` / `pure_essence` | 品质过滤 |
| `skip_thumb_lock` / `skip_thumb_discard` | 缩略图状态过滤 |

MaaFramework 会先把基础资源、任务选项和控制器资源字典合并，C++ 再从 `MaaContextGetNodeData` 读取最终 `attach`。不要在 Go 中再复制这些字段。

### 队列构建

EssenceGrid 只处理 Tracker 的 `new_cells`：

1. 采样格子底部颜色判断无瑕/高纯。
2. 在格子左下区域匹配锁定/废弃缩略图。
3. 应用品质与跳过选项。
4. 把通过的格子加入队列。
5. Pipeline 点击待处理格的中心。扫描器用点击前该坐标的核心图案核对右侧详情。同图标的格位按选中标记或长边的收放区分。确认选中后才读取词条，返回时再次核对选择和滚动条位置。

## Go 与 Pipeline

Pipeline 点击格子后，三个技能和等级由 OCR 节点读取。Go 的 `runUnifiedSkillDecision` 只决定分支：

- 命中目标：进入 `EssenceFilterLockItem`；
- 未命中且开启废弃：进入 `EssenceFilterDiscardItem`；
- 其他情况：回到 `EssenceGridAdvance`。

实际按钮识别、点击和 `EssenceFilterCheckLocked` / `EssenceFilterCheckDiscarded` 确认都属于 Pipeline。Go 不能直接跳到确认节点，否则会跳过动作。

## 控制器差异

基础资源定义 Win32 几何；`resource_adb` 等控制器资源只覆盖确实不同的 ROI、模板和滑动参数。

锁定与废弃按钮必须使用彼此独立的 ROI。`Click` 会点击模板匹配返回的 box；把两个按钮放进同一个大 ROI，可能让相似模板命中相邻按钮。

## 排查入口

| 现象 | 先查哪一层 |
| --- | --- |
| 行列数或格子矩形错误 | RecoGrid 的 ROI 和 segment 参数 |
| 累计错行、重复、提前结束 | GridTracker 的 offset、support、match ratio 和状态 |
| 品质或已锁/已弃判断错误 | EssenceGrid 的 HSV 与缩略图模板 |
| 技能匹配不符合预期 | Go OCR 归一化、数据和匹配选项 |
| 锁定/废弃按钮点错或不确认 | Pipeline 模板、ROI、等待和 next 顺序 |

## 构建

三个模块最终由生产 `cpp-algo` 组合：

```powershell
cmake --build agent\cpp-algo\build --config RelWithDebInfo --target cpp-algo
```

CMake 中 `recogrid` 与 `grid-tracker` 是独立静态库；`EssenceGrid` 编译进 `cpp-algo`。
