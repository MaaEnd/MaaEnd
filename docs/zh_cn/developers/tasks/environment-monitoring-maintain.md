# 开发手册 - EnvironmentMonitoring 维护文档

本文说明 `EnvironmentMonitoring`（环境监测）任务的 Pipeline 组织、路线数据、终端分组、自动生成机制及新观察点的接入方式。

环境监测的核心特点是 **「数据驱动 + 模板批量生成」**：每个观察点对应的 Pipeline JSON 不直接手写，而是通过 [`@joebao/maa-pipeline-generate`](https://www.npmjs.com/package/@joebao/maa-pipeline-generate) 工具，将 `tools/pipeline-generate/EnvironmentMonitoring/` 下的模板和数据批量渲染到 `assets/resource/pipeline/EnvironmentMonitoring/` 中。维护工作的重心在 **数据文件**，而不是手改 JSON。

> [!WARNING]
>
> `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` 与 `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` 都是 **生成产物**。手改这些文件会在下次重新生成时被覆盖。所有维护都应该改 `tools/pipeline-generate/EnvironmentMonitoring/` 下的源数据。

## 概览

环境监测的核心维护点如下：

| 模块               | 路径                                                                                | 作用                                                                                                                                     |
| ------------------ | ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| 任务入口           | `assets/tasks/EnvironmentMonitoring.json`                                           | interface 任务定义（无可配置选项，控制器 = Win32-Front / Wlroots / ADB）                                                                 |
| 主流程 Pipeline    | `assets/resource/pipeline/EnvironmentMonitoring.json`                               | 主入口节点 `EnvironmentMonitoringMain`，循环识别两个监测终端                                                                             |
| 终端分组（生成）   | `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`                     | 城郊监测终端 / 首墩监测终端的入口节点与各自的观察点 `next` 列表（**生成**）                                                              |
| 终端跳转           | `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`                     | `EnvironmentMonitoringGoTo*` 与 `Select*` 节点，从主菜单进入对应终端                                                                     |
| 拍照流程           | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                     | 进入拍照模式、调整朝向、识别拍照按钮、达成目标后回到终端                                                                                 |
| 摄像头滑动         | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                     | `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 四向调整朝向                                                                      |
| 公共按钮           | `assets/resource/pipeline/EnvironmentMonitoring/Button.json`                        | `TrackMissionButton` 等环境监测专用通用按钮                                                                                              |
| 观察点节点（生成） | `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`                | **每个观察点一份 JSON**，由模板渲染（**生成**）                                                                                          |
| 观察点模板         | `tools/pipeline-generate/EnvironmentMonitoring/template.jsonc`                      | 单观察点 Pipeline 模板（识别文本、接取/前往、传送、寻路、拍照）                                                                          |
| 终端模板           | `tools/pipeline-generate/EnvironmentMonitoring/terminals-template.jsonc`            | 终端分组节点模板                                                                                                                         |
| 路线/坐标数据      | `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs`                          | `ROUTE_CONFIG`：每个观察点的传送点、地图、路径、摄像头滑动方向                                                                           |
| 终端列表数据       | `tools/pipeline-generate/EnvironmentMonitoring/terminals-data.mjs`                  | 终端 ID 列表，对每个观察点 Job 节点串成 `next`                                                                                           |
| 游戏数据快照       | `tools/pipeline-generate/EnvironmentMonitoring/kite_station.json`                   | 由 `zmdmap` 提供的官方监测终端/委托数据（多语言名称、`shotTargetName`）                                                                  |
| 生成器配置         | `tools/pipeline-generate/EnvironmentMonitoring/config.json`                         | 单观察点输出配置：`outputPattern: "${Station}/${Id}.json"`                                                                               |
| 终端生成器配置     | `tools/pipeline-generate/EnvironmentMonitoring/terminals-config.json`               | 合并到单文件的终端输出配置：`outputFile: "Terminals.json"`                                                                               |
| 多语言文案         | `assets/locales/interface/*.json`                                                   | `task.EnvironmentMonitoring.*` 的 label / description（任务级；观察点名走 OCR）                                                          |
| 通用组件依赖       | `agent/go-service/map-tracker/`                                                     | `MapTrackerMove`、`MapTrackerAssertLocation`（详见 [map-tracker.md](../components/map-tracker.md)）                                      |
| 场景跳转依赖       | `assets/resource/pipeline/SceneManager/`、`Interface/`                              | `SceneEnterWorldWuling*`、`SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring`（详见 [scene-manager.md](../scene-manager.md)） |

## 主流程

环境监测在运行时按以下层次循环：

```text
EnvironmentMonitoringMain
  └─ EnvironmentMonitoringLoop                   （识别监测终端选择界面）
       ├─ [JumpBack]OutskirtsMonitoringTerminal  （城郊监测终端）
       │    └─ OutskirtsMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N        （遍历该终端下的所有观察点）
       │         └─ EnvironmentMonitoringFinish
       ├─ [JumpBack]MarkerStoneMonitoringTerminal（首墩监测终端）
       │    └─ MarkerStoneMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N
       │         └─ EnvironmentMonitoringFinish
       └─ EnvironmentMonitoringFinish
```

每个观察点 `{Id}Job` 内部的链路（由 `template.jsonc` 渲染）：

```text
{Id}Job                              （识别该观察点列表项）
  ├─ Accept{Id}                      （委托可接 → 点击接取）
  └─ GoTo{Id}Mission                 （委托已接 → 点击前往）
       └─ {Id}TrackOrGoTo
            ├─ Track{Id}             （存在「开始追踪」按钮则点击）
            └─ GoTo{Id}              （SubTask: SceneAnyEnterWorld 回大世界）
                 ├─ GoTo{Id}StartPos （MapTrackerAssertLocation 已就位 → MapTrackerMove）
                 └─ GoTo{Id}NotAtStartPos
                      └─ SubTask: ${EnterMap}            （传送）
                           ├─ GoTo{Id}RecheckStartPos    （传送后复核）
                           └─ GoTo{Id}ReEnterMap         （二次传送 → FinalCheck）
                                └─ GoTo{Id}MapTrackerMove
                                     ├─ anchor: EnvironmentMonitoringBackToTerminal → ${GoToMonitoringTerminal}
                                     ├─ anchor: EnvironmentMonitoringAdjustCamera   → ${Id}AdjustCamera
                                     └─ next:   EnvironmentMonitoringTakePhoto
EnvironmentMonitoringTakePhoto       （进入拍照模式 → 朝向 → 拍照）
  └─ [Anchor]EnvironmentMonitoringBackToTerminal
       └─ EnvironmentMonitoringGoTo{Outskirts|MarkerStone}MonitoringTerminal
```

> [!NOTE]
>
> `anchor` 字段的两个 key 是模板里硬编码的占位符名，运行时被替换为：
>
> - `EnvironmentMonitoringBackToTerminal` → 当前观察点所属终端的 `EnvironmentMonitoringGoTo{Station}` 节点（拍完回到正确终端）
> - `EnvironmentMonitoringAdjustCamera` → `{Id}AdjustCamera`（执行该观察点的摄像头滑动方向）

## 命名规则

### 观察点 ID（`Id`）

`routes.mjs` 里 `ROUTE_CONFIG[*].Id`，等价于生成出的所有节点名前缀：

```text
{PascalCase 英文名}
```

例如：

```text
WaterTemperatureController        → 净水温控装置
EcologyNearTheFieldLogisticsDepot → 储备站周围的生态环境
MysteriousCryptidGraffiti         → 谜之生物的涂鸦
```

`Id` 默认从 `kite_station.json` 中该任务的 `name["en-US"]` 转 PascalCase 得到，规则在 `data.mjs` 的 `buildDefaultId()` / `toPascalCase()`。**当 `ROUTE_CONFIG` 中显式提供 `Id` 时以显式值为准**——这是 `Id` 与游戏官方英文名脱钩的唯一入口。

> [!IMPORTANT]
>
> 不要把 `Id` 当作展示文案。展示文案走 `Name`（中文）或 OCR；`Id` 只用于拼接节点名、文件名（`outputPattern: "${Station}/${Id}.json"`）。`Id` 必须是合法的标识符（仅 `[A-Za-z0-9]`），且应与该观察点 `next` 列表里的 `[JumpBack]{Id}Job` 完全一致。

### 终端分组（`Station`）

由 `data.mjs` 的 `buildStationName()` 从 `mission.kiteStation`（或回退到 `__terminalId`）对应的 `kite_station.json[terminalId].level.name["en-US"]` PascalCase 而来。当前仓库内只有两组：

| 中文名       | Station ID                      | 对应 terminalId     | `GoToMonitoringTerminal` 锚点                            |
| ------------ | ------------------------------- | ------------------- | -------------------------------------------------------- |
| 城郊监测终端 | `OutskirtsMonitoringTerminal`   | `kitestation_002_1` | `EnvironmentMonitoringGoToOutskirtsMonitoringTerminal`   |
| 首墩监测终端 | `MarkerStoneMonitoringTerminal` | `kitestation_004_1` | `EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal` |

如果出现新的 Station，**生成器侧（`routes.mjs` + `data.mjs`）零改动**：`MONITORING_TERMINAL_IDS` 自动从 `kite_station.json` 派生，`GoToMonitoringTerminal` 锚点名按 `EnvironmentMonitoringGoTo{Station}` 模板拼接。但生成出来的 Pipeline 引用的下列**手写联动节点**必须先补齐，否则 MaaFramework 运行时会报「引用了未定义的任务」：

1. `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`：新增 `EnvironmentMonitoringGoTo{Station}MonitoringTerminal` 与 `EnvironmentMonitoringSelect{Station}MonitoringTerminal` 节点。
2. `assets/resource/pipeline/EnvironmentMonitoring.json` 的 `EnvironmentMonitoringLoop.next`：加入 `[JumpBack]{Station}MonitoringTerminal`。
3. 如有新文本识别节点（如 `EnvironmentMonitoringCheck{Station}MonitoringTerminalText`、`EnvironmentMonitoringIn{Station}MonitoringTerminal`），在 Pipeline 中补齐（手写）。

## 自动生成机制

### 单观察点：`config.json`

```json
{
    "template": "template.jsonc",
    "data": "data.mjs",
    "outputDir": "../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputPattern": "${Station}/${Id}.json",
    "format": true,
    "merged": false
}
```

`data.mjs` 的默认导出是数组，每个元素 = 一个观察点的渲染上下文（字段名与 `template.jsonc` 中 `${Xxx}` 占位符对应）。它从 `routes.mjs` 读取维护者手动维护的 `ROUTE_CONFIG` / `ROUTE_DEFAULTS`，再结合 `kite_station.json` 装配出最终行：

| 字段                                | 来源                                                                                   |
| ----------------------------------- | -------------------------------------------------------------------------------------- |
| `Station`                           | `kite_station.json` 的英文站名（PascalCase）                                           |
| `Id`                                | `ROUTE_CONFIG[*].Id` 优先；否则官方英文名 PascalCase                                   |
| `Name`                              | `kite_station.json` 的 `name["zh-CN"]`，去掉特殊符号                                   |
| `GoToMonitoringTerminal`            | 由 `Station` 决定                                                                      |
| `EnterMap`                          | `ROUTE_CONFIG[*].EnterMap`，**必须是 SceneManager 中存在的节点名**                     |
| `MapName` / `MapTarget` / `MapPath` | `ROUTE_CONFIG[*]`，对应 `MapTrackerMove` / `MapTrackerAssertLocation` 参数             |
| `CameraSwipeDirection`              | `ROUTE_CONFIG[*]`，必须是 `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 之一  |
| `CameraMaxHit`                      | `ROUTE_CONFIG[*].CameraMaxHit`，缺省用 `ROUTE_DEFAULTS.CameraMaxHit`（`2`）；对应 `${Id}AdjustCamera` 滑屏动作的最大命中次数 |
| `ExpectedText`                      | 由 `kite_station.json` 的 `mission.name` 多语言 map 自动展开（5 语言，英文转柔性正则） |
| `InExpectedText`                    | 由 `kite_station.json` 的 `mission.shotTargetName` 自动展开                            |

### 终端分组：`terminals-config.json`

```json
{
    "template": "terminals-template.jsonc",
    "data": "terminals-data.mjs",
    "outputDir": "../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputFile": "Terminals.json",
    "format": true,
    "merged": true
}
```

`terminals-data.mjs` 会扫描 `data.mjs` 装配后的全部行，按 `Station` 分组，把每个观察点的 `[JumpBack]{Id}Job` 串到对应终端的 `next` 列表里，并以 `EnvironmentMonitoringFinish` 收尾。

### 运行命令

```bash
# 安装依赖（首次）
npm i -g @joebao/maa-pipeline-generate
# 或一次性：npx @joebao/maa-pipeline-generate

# 在 tools/pipeline-generate/EnvironmentMonitoring/ 目录下运行：

# 1) 渲染所有观察点 Pipeline
npx @joebao/maa-pipeline-generate

# 2) 渲染终端入口
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

> [!NOTE]
>
> `data.mjs` 在渲染时如果某观察点缺字段，会用 `ROUTE_DEFAULTS`（`SceneAnyEnterWorld` + 占位坐标）兜底，并 `console.warn`。**占位渲染出的 Pipeline 能跑过 lint，但运行时无法真正抵达观察点**。新增观察点后务必确认 `ROUTE_CONFIG` 字段已全部填写，避免遗留占位。

## 关键依赖

### MapTracker

观察点的「传送 → 复核 → 寻路」三段都依赖 `agent/go-service/map-tracker/`：

- `MapTrackerAssertLocation`（识别）：根据当前小地图判断是否在 `MapTarget` 矩形内。
- `MapTrackerMove`（动作）：沿 `MapPath` 路径走到目标点，过程中支持 anchor 机制改写 `EnvironmentMonitoringBackToTerminal` / `EnvironmentMonitoringAdjustCamera`。

详细参数与坐标录制方式见 [map-tracker.md](../components/map-tracker.md) 与 [map-navigator.md](../components/map-navigator.md)。

### SceneManager

`EnterMap` 字段必须填写 SceneManager 中已存在的传送节点名，例如 `SceneEnterWorldWulingJingyuValley7`。如果新增观察点位于尚未支持的传送点，需要先在 `assets/resource/pipeline/SceneManager/` 与 `assets/resource/pipeline/Interface/` 下补齐对应的 `SceneEnterWorld*` 与场景识别节点（参见 [scene-manager.md](../scene-manager.md)）。

特殊兜底：`SceneAnyEnterWorld` 表示「不传送、直接回到当前世界」。当观察点本身没有合适的传送点（比如本分支里的「彩虹（缺少栖云窟传送点）」），可以临时填 `SceneAnyEnterWorld`，配合精确的 `MapTarget` / `MapPath` 让玩家跑过去；但要在 `routes.mjs` 注释 `// TODO:` 标记。

### 主菜单入口

环境监测主入口节点 `EnvironmentMonitoringMain` 通过 `[JumpBack]SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring` 进入终端选择界面。该节点维护在 `assets/resource/pipeline/Interface/SceneInMenu.json`，新增地区监测终端时需要确认主菜单入口已能进入对应界面。

## 添加新观察点

新增的观察点一般来自游戏更新，体现在 `kite_station.json` 中多出一条 `mission`。维护流程：

> [!TIP]
>
> 如果你在使用支持 AI Skill 的客户端（如 Claude Code 或 GitHub Copilot），可以直接调用 **`environment-monitoring-add-route` skill**，它会自动检测缺失条目并通过交互式问答帮你填写 `ROUTE_CONFIG`，省去手动查表的步骤。

### 1. 更新游戏数据

把最新的 `kite_station.json` 替换到 `tools/pipeline-generate/EnvironmentMonitoring/kite_station.json`（数据来源：`zmdmap`）。

### 2. 检查缺失项

对比 `kite_station.json` 中的 `entrustTasks` 与 `ROUTE_CONFIG` 的条目，确认：

- **缺失配置**：`ROUTE_CONFIG` 完全没有该观察点 → 走步骤 3。
- **占位待补全**：`ROUTE_CONFIG` 已加但 `EnterMap` / `MapPath` 等字段仍是默认值 → 走步骤 4。

### 3. 在 `ROUTE_CONFIG` 中新增条目

`tools/pipeline-generate/EnvironmentMonitoring/routes.mjs` → `ROUTE_CONFIG`：

```javascript
{
    Id: "MyNewObservationPoint",         // PascalCase，作为节点前缀和文件名
    Name: "我的新观察点",                 // 必须与 kite_station.json 中的 zh-CN 名（去掉特殊符号后）匹配
    EnterMap: "SceneEnterWorldWulingXxx",// SceneManager 中存在的传送节点
    MapName: "map02_lv001",              // MapTracker 的小地图标识
    MapTarget: [x, y, w, h],             // 目标矩形（小地图坐标）
    MapPath: [[x1, y1], [x2, y2], ...],  // 寻路路径（小地图坐标）
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp", // 朝向调整方向
    // CameraMaxHit: 2,  // 可选；滑屏最大命中次数，默认为 2；拍摄目标较难对准时可适当调大
}
```

> [!IMPORTANT]
>
> `Name` 是 `data.mjs` 内部 `normalizeMissionName()` 的匹配键，会与 `kite_station.json` 中的 `mission.name["zh-CN"]` 做去符号、小写对比。如果对不上，`ROUTE_CONFIG` 里的覆盖不会生效，会被默认值兜底。

### 4. 录制坐标和路径

参考 [map-navigator.md](../components/map-navigator.md) 的 GUI 工具录制 `MapTarget` / `MapPath`，并在游戏中确认：

- 拍照时摄像头需要往哪个方向滑（决定 `CameraSwipeDirection`）。
- 站位是否能让 `EnvironmentMonitoringTakePhoto` 走 `EnvironmentMonitoringEnterCameraMode`（自动朝向目标）成功；如果不行，会自动回退到 `EnvironmentMonitoringTakePhotoDirectly` + 手动滑屏 `${Id}AdjustCamera`。

### 5. 重新生成 Pipeline

```bash
cd tools/pipeline-generate/EnvironmentMonitoring
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

确认生成出的两类文件：

- `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`
- `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`（`{Station}MonitoringTerminalLoop.next` 中包含 `[JumpBack]{Id}Job`）

## 修改已有观察点路线

只调整路线/朝向（不变更英文名）：

1. 改 `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs` 的 `ROUTE_CONFIG[i]`。
2. 重新生成（仅需跑 `npx @joebao/maa-pipeline-generate`，终端列表未变化无需重新生成 `Terminals.json`）。
3. 提交 `routes.mjs` 与重生成的 `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`。

如果观察点的官方英文名变了导致 `Id` 漂移：

1. 在 `ROUTE_CONFIG` 中显式加 `Id: "ExistingId"` 锁定旧 ID（避免 `next` 链路里所有 `[JumpBack]{Id}Job` 都失效）。
2. 重新生成。

## 自检清单

提交前至少检查：

1. `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs` 的 `ROUTE_CONFIG` 中新增/修改条目是否字段齐全。
2. `ROUTE_CONFIG` 中新增条目的 `EnterMap`、`MapTarget`、`MapPath`、`CameraSwipeDirection` 均已填写真实值（非 `ROUTE_DEFAULTS` 占位）。
3. 重生成的 `Terminals.json` 中两个 `{Station}MonitoringTerminalLoop.next` 包含全部新 `[JumpBack]{Id}Job`，并以 `EnvironmentMonitoringFinish` 收尾。
4. `EnterMap` 引用的 `Scene*` 节点确实存在于 `assets/resource/pipeline/SceneManager/` 与 `Interface/` 中。
5. `CameraSwipeDirection` 是 `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 四者之一。
6. **没有手改** `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` 或 `Terminals.json`（手改会被下次生成覆盖；如确需特殊节点，应在 `template.jsonc` / `terminals-template.jsonc` 中扩展）。
7. JSON 文件遵循 `.prettierrc` 格式（生成器自带 `format: true`，但提交前 `pnpm prettier --write` 一遍更稳）。

## 常见坑

- **手改生成产物**：直接编辑 `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` 或 `Terminals.json`，下次重新生成时改动会丢。改源数据 + 重新生成才是正确做法。
- **`Name` 与游戏文本对不上**：`ROUTE_CONFIG[i].Name` 只用于在 `data.mjs` 内部匹配 `kite_station.json` 的 `mission.name["zh-CN"]`，不是显示文本也不是 OCR 期望。匹配失败时 `console.warn` 提示，并使用占位值兜底。
- **`Id` 与 `kite_station.json` 英文名漂移**：当游戏侧改英文名后，自动算出的 `Id` 会变，导致 `Terminals.json` 中旧的 `[JumpBack]{Id}Job` 失效。补 `ROUTE_CONFIG[i].Id` 显式锁定旧 ID 即可。
- **`EnterMap` 写了不存在的 Scene 节点**：生成器不校验，运行时会卡在 `GoTo{Id}NotAtStartPos` 死循环。
- **`MapPath` 经过未解锁区域 / 战斗 / 互动物**：MapTracker 不处理战斗与剧情，路径只能选纯通行段。
- **`Station` 新增但 `Locations.json` / `EnvironmentMonitoringLoop.next` 没同步**：新终端无法被识别进入，所有观察点都跑不到。
- **`anchor` 占位符名一致性**：`template.jsonc` 中 `anchor` 的 key 名 `EnvironmentMonitoringBackToTerminal` 必须与 `TakePhoto.json` 中的 `[Anchor]EnvironmentMonitoringBackToTerminal` 保持完全一致，否则 anchor 机制失效。
- **「占位值能跑通生成 ≠ 任务能跑通运行」**：`ROUTE_DEFAULTS` 让生成阶段不报错，但运行时 `EnterMap=SceneAnyEnterWorld` + `MapPath=[[0,0]]` 永远走不到目标。提交前请人工核对 `ROUTE_CONFIG` 中没有遗留占位条目（`EnterMap` 为 `SceneAnyEnterWorld` 且没有 `// TODO:` 注释时应引起注意）。
