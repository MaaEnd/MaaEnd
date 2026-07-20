# 开发手册 - EnvironmentMonitoring 维护文档

本文说明 `EnvironmentMonitoring`（环境监测）任务的 Pipeline 组织、路线数据、终端分组、自动生成机制及新观察点的接入方式。

环境监测的核心特点是 **「数据驱动 + 模板批量生成」**：每个观察点对应的 Pipeline JSON 不直接手写，而是通过 [`@joebao/maa-pipeline-generate`](https://www.npmjs.com/package/@joebao/maa-pipeline-generate) 工具，将 `tools/pipeline-generate/EnvironmentMonitoring/` 下的模板/路线配置和 `tools/pipeline-generate/data/` 下的 zmdmap 缓存数据批量渲染到 `assets/resource/pipeline/EnvironmentMonitoring/` 中。维护工作的重心在 **生成配置与数据缓存**，而不是手改 JSON。

> [!WARNING]
>
> `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` 与 `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` 都是 **生成产物**。手改这些文件会在下次重新生成时被覆盖。所有维护都应该改 `tools/pipeline-generate/EnvironmentMonitoring/` 下的生成配置，或通过 `pnpm fetch:zmdmap` 更新 `tools/pipeline-generate/data/` 下的 zmdmap 缓存。

## 概览

环境监测的核心维护点如下：

| 模块                | 路径                                                                              | 作用                                                                                                                                                                                                                                                                      |
| ------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 任务入口            | `assets/tasks/EnvironmentMonitoring.json`                                         | interface 任务定义（无可配置选项，控制器 = Win32-Front / Wlroots / ADB）                                                                                                                                                                                                  |
| 主流程 Pipeline     | `assets/resource/pipeline/EnvironmentMonitoring.json`                             | 主入口节点 `EnvironmentMonitoringMain`，循环识别两个监测终端                                                                                                                                                                                                              |
| 终端分组（生成）    | `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`                   | 城郊监测终端 / 首墩监测终端的入口节点与各自的观察点 `next` 列表（**生成**）                                                                                                                                                                                               |
| 终端跳转            | `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`                   | `EnvironmentMonitoringGoTo*` 与 `Select*` 节点，从主菜单进入对应终端                                                                                                                                                                                                      |
| 拍照流程            | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                   | 进入拍照模式、调整朝向、识别拍照按钮、达成目标后回到终端                                                                                                                                                                                                                  |
| 摄像头滑动          | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json`                   | `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 四向调整朝向                                                                                                                                                                                                       |
| 公共按钮            | `assets/resource/pipeline/EnvironmentMonitoring/Button.json`                      | `TrackMissionButton` 等环境监测专用通用按钮                                                                                                                                                                                                                               |
| 观察点节点（生成）  | `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`              | **每个观察点一份 JSON**，由模板渲染（**生成**）；`Id` 由 `model.mjs` 自动生成，通常不用手写                                                                                                                                                                               |
| 观察点模板          | `tools/pipeline-generate/EnvironmentMonitoring/generator/template.json`           | 单观察点 Pipeline 模板（识别文本、接取/前往、传送、寻路、拍照）                                                                                                                                                                                                           |
| 终端模板            | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-template.json` | 终端分组节点模板                                                                                                                                                                                                                                                          |
| 路线/坐标数据       | `tools/pipeline-generate/EnvironmentMonitoring/routes.json`                       | 按观察点 `MissionId` 匹配的路线覆盖（传送点、地图、路径、摄像头滑动方向）；`Name` 仅供人工阅读，`Id` 是最终模板节点 ID，方便搜索生成节点/文件名                                                                                                                           |
| 路线 JSON Schema    | `tools/schema/environment_monitoring_routes.schema.json`                          | `routes.json` 的字段约束（必填项、枚举、坐标数组形状），通过 `.vscode/settings.json` 自动关联，提供 IDE 字段补全和校验                                                                                                                                                    |
| 失败收集参数 Schema | `tools/schema/components/failure_collector.schema.json`                           | 通用失败收集 Custom Action 的参数约束；动作名称注册在 `tools/schema/custom.action.schema.json`                                                                                                                                                                            |
| 路线同步逻辑        | `tools/pipeline-generate/EnvironmentMonitoring/generator/sync-routes.mjs`         | 在生成前自动同步 `routes.json` 的 `MissionId` / `Name` / `Id`，并按 `MissionId` 排序                                                                                                                                                                                      |
| 路线解析逻辑        | `tools/pipeline-generate/EnvironmentMonitoring/generator/route-resolver.mjs`      | 将 `routes.json` 条目解析为模板需要的寻路识别/动作参数，并统一处理未适配降级                                                                                                                                                                                              |
| 规范化任务模型      | `tools/pipeline-generate/EnvironmentMonitoring/generator/model.mjs`               | 统一读取 zmdmap 与 `routes.json`，生成路线和终端模板共享的观察点任务模型                                                                                                                                                                                                  |
| 终端列表数据        | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-data.mjs`      | 从 `model.mjs` 的规范化任务和自动派生的终端列表生成各终端 `next`                                                                                                                                                                                                          |
| 游戏数据快照        | `tools/pipeline-generate/data/kite_station_i18n.json`                             | 由 `zmdmap` 提供的官方监测终端/委托数据（多语言名称、`shotTargetName`），由 `pnpm fetch:zmdmap` 缓存                                                                                                                                                                      |
| 生成器配置          | `tools/pipeline-generate/EnvironmentMonitoring/generator/config.json`             | 单观察点输出配置：`outputPattern: "${Station}/${Id}.json"`                                                                                                                                                                                                                |
| 终端生成器配置      | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-config.json`   | 合并到单文件的终端输出配置：`outputFile: "Terminals.json"`                                                                                                                                                                                                                |
| 多语言文案          | `assets/locales/interface/*.json`                                                 | `task.EnvironmentMonitoring.*` 的 label / description（任务级；观察点名走 OCR）                                                                                                                                                                                           |
| 通用组件依赖        | `agent/go-service/maptracker/` / `3rdparty/maa-copilot`                           | `MapTrackerMove`、`MapTrackerGoal`、`MapTrackerAssertLocation`、`MapLocateAssertLocation`、`MapNavigateAction`（详见 [map-tracker.md](../components/map-tracker.md)、[map-locator.md](../components/map-locator.md)、[map-navigator.md](../components/map-navigator.md)） |
| 场景跳转依赖        | `assets/resource/pipeline/SceneManager/`、`Interface/`                            | `SceneEnterWorldWuling*`、`SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring`（详见 [scene-manager.md](../scene-manager.md)）                                                                                                                                  |

## 主流程

环境监测在运行时按以下层次循环：

```text
EnvironmentMonitoringMain
  └─ EnvironmentMonitoringLoop                   （识别监测终端选择界面）
       ├─ [JumpBack]OutskirtsMonitoringTerminal  （城郊监测终端）
       │    └─ OutskirtsMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N        （遍历该终端下的所有观察点）
       │         └─ EnvironmentMonitoringTerminalFinish
       ├─ [JumpBack]MarkerStoneMonitoringTerminal（首墩监测终端）
       │    └─ MarkerStoneMonitoringTerminalLoop
       │         ├─ [JumpBack]{Id}Job × N
       │         └─ EnvironmentMonitoringTerminalFinish
       └─ EnvironmentMonitoringFinish
```

每个观察点 `{Id}Job` 内部的链路（由 `template.json` 渲染）：

```text
{Id}Job                              （识别该观察点列表项）
  ├─ Accept{Id}                      （委托可接 → 点击接取）
  └─ GoTo{Id}Mission                 （委托已接 → 点击前往）
       └─ {Id}TrackOrGoTo
            ├─ Track{Id}             （存在「开始追踪」按钮则点击）
            │    ├─ {Id}NotAdapted   （路线未适配 → 仅提示并结束该观察点）
            │    └─ GoTo{Id}         （路线已适配 → 继续前往）
            └─ AlreadyTracked{Id}    （已经在追踪中）
                 ├─ {Id}NotAdapted   （路线未适配 → 仅提示并结束该观察点）
                 └─ GoTo{Id}         （路线已适配 → 继续前往）
                      ├─ 默认传送：GoTo{Id}
                      │    ├─ 寻路路线
                      │    │    ├─ GoTo{Id}StartPos （已在起点 → 寻路）
                      │    │    └─ GoTo{Id}NotAtStartPos → SubTask: ${EnterMap}
                      │    └─ 传送后直拍
                      │         └─ GoTo{Id}NotAtStartPos → SubTask: ${EnterMap}
                      │              ├─ 无 Heading → {Id}TakePhoto
                      │              └─ 有 Heading → GoTo{Id}Move（MapTrackerToward）
                      └─ QuickTeleport: true
                           ├─ Track{Id} → {Id}InQuickTeleportMap
                           └─ AlreadyTracked{Id} → {Id}OpenTrackedMissionMap → {Id}InQuickTeleportMap
                                └─ {Id}QuickTeleportSelect   （点击“前往传送”）
                                     └─ {Id}QuickTeleport    （点击“传送”）
                                          └─ {Id}QuickTeleportDone
                                               ├─ 寻路路线 → GoTo{Id}Move
                                               └─ 传送后直拍
                                                    ├─ 无 Heading → {Id}TakePhoto
                                                    └─ 有 Heading → GoTo{Id}Move
GoTo{Id}Move                         （寻路动作；直拍有 Heading 时为 MapTrackerToward）
  └─ {Id}TakePhoto
       ├─ anchor: EnvironmentMonitoringBackToTerminal → ${GoToMonitoringTerminal}
       ├─ anchor: EnvironmentMonitoringAdjustCamera   → ${Id}AdjustCamera
       └─ next: EnvironmentMonitoringTakePhoto
EnvironmentMonitoringTakePhoto       （进入拍照模式 → 朝向 → 拍照）
  └─ [Anchor]EnvironmentMonitoringBackToTerminal
       └─ EnvironmentMonitoringGoTo{Outskirts|MarkerStone}MonitoringTerminal
```

每个 `{Id}Job` 仍负责识别观察点列表项，命中后通过通用 `FailureCollectorRunTask` 执行 `{Id}Execute` 路线。生成器根据 zmdmap 的五语言 `mission.name` 为新任务补齐 `task.EnvironmentMonitoring.route.{Id}.failed`；已有失败提示会保留，避免覆盖人工调整。若路线内部任意节点失败，包装 Action 会记录 `{Id}Failed`、运行 `recovery_task` 返回当前监测终端，并向外返回成功以继续后续路线；全部终端遍历结束后，`EnvironmentMonitoringFinish` 通过 `FailureCollectorFinish` 按失败顺序依次调用这些提示节点，再返回总任务失败。Agent 不直接输出用户提示。

> [!NOTE]
>
> `anchor` 字段的两个 key 是模板里硬编码的占位符名，运行时被替换为：
>
> - `EnvironmentMonitoringBackToTerminal` → 当前观察点所属终端的 `EnvironmentMonitoringGoTo{Station}` 节点（拍完回到正确终端）
> - `EnvironmentMonitoringAdjustCamera` → `{Id}AdjustCamera`（执行该观察点的摄像头滑动方向）

## 命名规则

### 观察点节点 ID（`Id`，自动生成）

`Id` 是 `model.mjs` 装配出的生成字段，等价于所有观察点节点名和输出文件名的前缀：

```text
{PascalCase 英文名}
```

例如：

```text
WaterTemperatureController        → 净水温控装置
EcologyNearTheFieldLogisticsDepot → 储备站周围的生态环境
MysteriousCryptidGraffiti         → 谜之生物的涂鸦
```

默认情况下，`Id` 从 `kite_station_i18n.json` 中该任务的 `name["en-US"]` 转 PascalCase 得到，规则在 `common.mjs` 的 `buildDefaultId()` / `toPascalCase()`。如果英文名缺失，会回退到 `missionId` / `entrustIdx`；如果出现重复，`ensureUniqueId()` 会自动追加后缀。

维护 `routes.json` 时不需要手算 `Id`。路线匹配键是 `MissionId`，`Id` 会在重新生成时自动写入 `routes.json`，等价于最终模板使用的节点名前缀，方便直接搜索生成节点和文件名。

> [!IMPORTANT]
>
> 不要把 `Id` 当作展示文案。展示文案走 zmdmap 名称 / OCR；`Name` 是 routes.json 的人工备注，`Id` 只用于拼接节点名、文件名（`outputPattern: "${Station}/${Id}.json"`），并由生成器自动刷新。

### 终端分组（`Station`）

由 `model.mjs` 从 `mission.kiteStation`（或回退到 `__terminalId`）对应的 `kite_station_i18n.json[terminalId].level.name["en-US"]` PascalCase 而来。当前仓库内只有两组：

| 中文名       | Station ID                      | 对应 terminalId     | `GoToMonitoringTerminal` 锚点                            |
| ------------ | ------------------------------- | ------------------- | -------------------------------------------------------- |
| 城郊监测终端 | `OutskirtsMonitoringTerminal`   | `kitestation_002_1` | `EnvironmentMonitoringGoToOutskirtsMonitoringTerminal`   |
| 首墩监测终端 | `MarkerStoneMonitoringTerminal` | `kitestation_004_1` | `EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal` |

如果出现新的 Station，**生成器侧（`routes.json` + `model.mjs`）零改动**：`MONITORING_TERMINAL_IDS` 自动从 `kite_station_i18n.json` 派生，`GoToMonitoringTerminal` 锚点名按 `EnvironmentMonitoringGoTo{Station}` 模板拼接。但生成出来的 Pipeline 引用的下列**手写联动节点**必须先补齐，否则 MaaFramework 运行时会报「引用了未定义的任务」：

1. `assets/resource/pipeline/EnvironmentMonitoring/Locations.json`：新增 `EnvironmentMonitoringGoTo{Station}MonitoringTerminal` 与 `EnvironmentMonitoringSelect{Station}MonitoringTerminal` 节点。
2. `assets/resource/pipeline/EnvironmentMonitoring.json` 的 `EnvironmentMonitoringLoop.next`：加入 `[JumpBack]{Station}MonitoringTerminal`。
3. 如有新文本识别节点（如 `EnvironmentMonitoringCheck{Station}MonitoringTerminalText`、`EnvironmentMonitoringIn{Station}MonitoringTerminal`），在 Pipeline 中补齐（手写）。

## 自动生成机制

### 单观察点：`config.json`

```json
{
    "template": "template.json",
    "data": "data.mjs",
    "outputDir": "../../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputPattern": "${Station}/${Id}.json",
    "format": true,
    "merged": false
}
```

`data.mjs` 的默认导出是数组，每个元素 = 一个观察点的渲染上下文（字段名与 `template.json` 中 `${Xxx}` 占位符对应）。`pnpm generate:EnvironmentMonitoring` 会先调用 `sync-routes.mjs` 刷新上一级 `routes.json`；随后 `model.mjs` 只读 `routes.json` 与 `kite_station_i18n.json`，通过 `route-resolver.mjs` 装配规范化任务，`data.mjs` 再投影出最终行：

| 字段                                                                            | 来源                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Station`                                                                       | `kite_station_i18n.json` 的英文站名（PascalCase）                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `Id`                                                                            | 默认由官方英文名 PascalCase 自动生成；会同步写回 `routes.json`，等价于最终模板使用的节点 ID                                                                                                                                                                                                                                                                                                                                                                       |
| `Name`                                                                          | 来自 `kite_station_i18n.json` 的中文名；`MissionId` 只作为 `model.mjs` 匹配 `routes.json` 的主键，不透传给模板                                                                                                                                                                                                                                                                                                                                                    |
| `GoToMonitoringTerminal`                                                        | 由 `Station` 决定                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `EnterMap`                                                                      | `routes.json[*].EnterMap`，默认传送入口，必须是 SceneManager 中存在的节点名；启用 `QuickTeleport` 时不会使用，可省略                                                                                                                                                                                                                                                                                                                                              |
| `QuickTeleport`                                                                 | `routes.json[*].QuickTeleport`，可选布尔值，默认 `false`；启用后从追踪任务地图依次点击“前往传送”和“传送”，绕过 `EnterMap` 万能跳转                                                                                                                                                                                                                                                                                                                                |
| `MapName` / `MapAssert` / `MapPath` / `MapTarget` / `MapTargetTier` / `MapGoal` | `routes.json[*]`，对应初始位置判断与后续寻路参数；`MapPath` 生成 `MapTrackerAssertLocation` + `MapTrackerMove`，`MapTarget` 生成 `MapLocateAssertLocation` + `MapNavigateAction` 的 `NAVMESH` 目标点，`MapTargetTier` 可选生成 `target_tier`，`MapGoal` 生成 `MapTrackerAssertLocation` + `MapTrackerGoal`。传送后直拍时这些字段全部省略；寻路时 `MapPath` / `MapTarget` / `MapGoal` 三者必须且只能选一个；`QuickTeleport + MapTarget/MapGoal` 可省略 `MapAssert` |
| `CameraSwipeDirection`                                                          | `routes.json[*]`，必须是 `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 之一                                                                                                                                                                                                                                                                                                                                                                              |
| `CameraMaxHit`                                                                  | `routes.json[*].CameraMaxHit`，缺省为 `2`；对应 `${Id}AdjustCamera` 滑屏动作的最大命中次数                                                                                                                                                                                                                                                                                                                                                                        |
| `OcrReplace`                                                                    | 由 `routes.json[*].Replace` 透传到 `Check${Id}Text.replace` 与 `In${Id}Mission.replace`；用于按任务配置任务列表和任务详情页 OCR 的易混字符替换，不影响路线是否已适配的判断                                                                                                                                                                                                                                                                                        |
| `ExpectedText`                                                                  | 由 `kite_station_i18n.json` 的 `mission.name` 多语言 map 自动展开（5 语言，英文转柔性正则）                                                                                                                                                                                                                                                                                                                                                                       |
| `InExpectedText`                                                                | 由 `kite_station_i18n.json` 的 `mission.shotTargetName` 自动展开                                                                                                                                                                                                                                                                                                                                                                                                  |
| `TrackOrGoToNext` / `AfterTrackNext` / `AfterAlreadyTrackedNext`                | 由 `data.mjs` 根据路线是否完整及 `QuickTeleport` 自动决定：默认进入 `GoTo${Id}`；快捷传送时，开始追踪后等待任务地图，已追踪则先点击定位图标打开任务地图；未适配时进入 `${Id}NotAdapted`                                                                                                                                                                                                                                                                           |
| `GoToNext` / `AfterTeleportDescription` / `AfterTeleportNext`                   | 由 `data.mjs` 根据路线类型自动决定：传送后直拍始终执行真实传送并进入 `${Id}TakePhoto`；`MapPath` 传送后返回 `GoTo${Id}StartPos` 复核落点；`MapTarget` / `MapGoal` 传送后直接进入 `GoTo${Id}Move`                                                                                                                                                                                                                                                                  |

### 终端分组：`terminals-config.json`

```json
{
    "template": "terminals-template.json",
    "data": "terminals-data.mjs",
    "outputDir": "../../../../assets/resource/pipeline/EnvironmentMonitoring",
    "outputFile": "Terminals.json",
    "format": true,
    "merged": true
}
```

`terminals-data.mjs` 会扫描 `model.mjs` 装配后的全部规范化任务，按 `Station` 分组，把每个观察点的 `[JumpBack]{Id}Job` 串到对应终端的 `next` 列表里，并以 `EnvironmentMonitoringTerminalFinish` 收尾。单条路线失败由 `{Id}Job` 的 `FailureCollectorRunTask` 包装 Action 处理；两个终端都结束后，由主流程的 `EnvironmentMonitoringFinish` 汇总结果。

### 运行命令

```bash
# 推荐：在仓库根目录运行
pnpm generate:EnvironmentMonitoring

# 只更新 zmdmap 缓存
pnpm fetch:zmdmap

# 如果已经更新过 zmdmap 缓存，也可以在 tools/pipeline-generate/EnvironmentMonitoring/generator/ 目录下单独渲染：

# 0) 同步 routes.json 的 MissionId/Name/Id
node sync-routes.mjs

# 1) 渲染所有观察点 Pipeline
npx @joebao/maa-pipeline-generate

# 2) 渲染终端入口
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

> [!NOTE]
>
> `model.mjs` 在渲染时如果某观察点没有 `routes.json` 条目，或条目存在但任一必填字段缺失（`null` / 空字符串 / 空数组），会 `console.warn` 并把该观察点视为 **未适配**。未适配观察点仍会生成 Pipeline，但运行时只会接取并追踪任务，在 `${Id}NotAdapted` 提示后结束，不会执行传送或寻路。

## 关键依赖

### 寻路组件

观察点的传送与寻路流程会按路线类型组合使用 MapTracker 与 MapNavigator：

- `MapTrackerAssertLocation` / `MapLocateAssertLocation`（识别）：根据当前小地图判断是否在 `MapAssert` 矩形内。普通传送路线用于判断是否需要调用 `EnterMap`，`QuickTeleport + MapPath` 用于传送后复核固定起点；`QuickTeleport + MapTarget/MapGoal` 不进入该节点，可省略 `MapAssert`。
- `MapTrackerMove` / `MapTrackerGoal` / `MapNavigateAction`（动作）：沿 `MapPath` 路径走到目标点，按 `MapGoal` 调用 `MapTrackerGoal` 自动规划并前往目标，或按 `MapTarget` 生成 `NAVMESH` 目标点并前往目标；`MapTargetTier` 会透传为 `target_tier`，用于目标坐标与起点不在同一 tier 的情况。
- `${Id}TakePhoto`（包装节点）：为寻路和直拍路线统一设置 `EnvironmentMonitoringBackToTerminal` / `EnvironmentMonitoringAdjustCamera` anchor，再进入公共拍照流程。
- 传送后直拍不执行地图断言或寻路；配置 `Heading` 时会独立调用 `MapTrackerToward` 调整角色朝向。`MapPath` 路线传送后再次复核 `MapAssert`；`MapTarget` / `MapGoal` 路线依靠 NavMesh 从传送点附近自行前往目标。

详细参数与坐标录制方式见 [map-tracker.md](../components/map-tracker.md)、[map-locator.md](../components/map-locator.md) 与 [map-navigator.md](../components/map-navigator.md)。

### SceneManager

默认传送路线的 `EnterMap` 字段必须填写 SceneManager 中已存在的传送节点名，例如 `SceneEnterWorldWulingJingyuValley7`。如果新增观察点位于尚未支持的传送点，需要先在 `assets/resource/pipeline/SceneManager/` 与 `assets/resource/pipeline/Interface/` 下补齐对应的 `SceneEnterWorld*` 与场景识别节点（参见 [scene-manager.md](../scene-manager.md)）。启用 `QuickTeleport: true` 时不会调用 `EnterMap`，该字段可以省略。

`model.mjs` 通过判断 `routes.json` 条目是否完整决定是否进入寻路/拍照流程，未适配点会直接走 `${Id}NotAdapted` 分支。所有已适配条目都必须在真实 `EnterMap` 与 `QuickTeleport: true` 中至少选择一种传送入口，并配置 `CameraSwipeDirection`。如果传送落点已经满足拍照条件，则省略 `MapName`、`MapAssert` 和全部寻路相关字段，生成器自动进入直拍分支；否则需要配置 `MapName`，在 `MapPath` / `MapTarget` / `MapGoal` 中三选一，并按路线类型补 `MapAssert`。

### `routes.json` 配置类型

所有完整适配条目都需要元数据、`CameraSwipeDirection`，以及 `EnterMap` / `QuickTeleport: true` 中的一种传送入口。不同路线类型的字段组合如下：

| 类型          | 地图与路线字段                                                         | `MapAssert`                  | 运行行为                                   |
| ------------- | ---------------------------------------------------------------------- | ---------------------------- | ------------------------------------------ |
| metadata-only | 仅 `MissionId` / `Name` / `Id`                                         | 不填                         | 仅接取并追踪，不传送或拍照                 |
| 传送后直拍    | 不填 `MapName` 和寻路字段；可选 `Heading`                              | 不填                         | 传送 → 可选 `MapTrackerToward` → 拍照      |
| `MapPath`     | `MapName` + `MapPath`；可选 `Heading` / `NoEnsureInitialMovementState` | 默认传送和快捷传送都必填     | 断言固定起点 → `MapTrackerMove` → 拍照     |
| `MapTarget`   | `MapName` + `MapTarget`；跨层时可加 `MapTargetTier`                    | 默认传送必填；快捷传送可省略 | `MapNavigateAction` 的 NAVMESH 寻路 → 拍照 |
| `MapGoal`     | `MapName` + `MapGoal`；可选 `Heading` / `NoEnsureInitialMovementState` | 默认传送必填；快捷传送可省略 | `MapTrackerGoal` 自动寻路 → 拍照           |

`CameraMaxHit` 和 `Replace` 适用于所有已适配路线，不改变路线类型。直拍配置只能用于已经游戏实测确认的传送落点；缺少路线数据时继续保留 metadata-only 状态。

### 主菜单入口

环境监测主入口节点 `EnvironmentMonitoringMain` 通过 `[JumpBack]SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring` 进入终端选择界面。该节点维护在 `assets/resource/pipeline/Interface/InScene/Region.json`，新增地区监测终端时需要确认主菜单入口已能进入对应界面。

## 添加新观察点

新增的观察点一般来自游戏更新，体现在 `kite_station_i18n.json` 中多出一条 `mission`。维护流程：

> [!TIP]
>
> 如果你在使用支持 AI Skill 的客户端（如 Claude Code 或 GitHub Copilot），可以直接调用 **`environment-monitoring-add-route` skill**，它会自动检测缺失条目并通过交互式问答帮你填写 `routes.json`，省去手动查表的步骤。

### 1. 更新游戏数据

运行 `pnpm fetch:zmdmap`，会从 zmdmap API 下载并缓存最新的 `tools/pipeline-generate/data/kite_station_i18n.json`。

### 2. 检查路线适配状态

对比 `kite_station_i18n.json` 中的 `entrustTasks` 与 `routes.json` 的条目，确认每个观察点的状态。匹配方式是 `missionId` 对 `routes.json` 中的 `MissionId`，而不是 `Name` 或 `Id`：

- **未适配**：`routes.json` 没有该观察点，或条目存在但缺失任一必填字段（含 `null` / 空字符串 / 空数组） → 生成后只会接取并追踪。
- **准备适配**：需要让该观察点自动前往并拍照 → 走步骤 3，补齐真实路线。

### 3. 在 `routes.json` 中新增/补全条目

`tools/pipeline-generate/EnvironmentMonitoring/routes.json`：

```jsonc
{
    "MissionId": "m1m30",                    // 必须与 kite_station_i18n.json 中的 missionId 匹配
    "Name": "我的新观察点",                  // 中文名，仅供人工阅读
    "Id": "MyNewObservationPoint",           // 最终模板节点 ID，仅供人工搜索节点/文件名
    "EnterMap": "SceneEnterWorldWulingXxx", // SceneManager 中存在的传送节点
    // "QuickTeleport": true,             // 可选；启用后从追踪任务地图快捷传送，EnterMap 可省略
    "MapName": "map02_lv001",               // 地图标识：MapPath 用 MapTracker map_name；MapGoal 用可加载 NavMesh 的精确 MapTracker map_name；MapTarget 用 MapLocate zone_id
    "MapAssert": [x, y, w, h],              // 普通传送和 QuickTeleport + MapPath 必填；快捷传送的 MapTarget / MapGoal 可省略
    "MapPath": [[x1, y1], [x2, y2]],        // 寻路路径（小地图坐标），与 MapTarget / MapGoal 三选一
    // "MapTarget": [x, y],             // MapNavigateAction 的 NAVMESH 目标点
    // "MapTargetTier": "ValleyIV_L1_171", // 可选；MapTarget 坐标所在的 target_tier，目标与起点不在同一 tier 时填写
    // "MapGoal": [x, y],               // MapTrackerGoal 目标点，生成时会自动使用 MapTrackerGoal
    "CameraSwipeDirection": "EnvironmentMonitoringSwipeScreenUp", // 朝向调整方向
    // "CameraMaxHit": 2,  // 可选；滑屏最大命中次数，默认为 2；拍摄目标较难对准时可适当调大
    // "Replace": [["売", "壳"]] // 可选；任务列表和任务详情页 OCR 易混字符替换
}
```

传送点可直接拍照时使用精简写法，不增加额外开关字段：

```jsonc
{
    "MissionId": "m1m30",
    "Name": "我的新观察点",
    "Id": "MyNewObservationPoint",
    "EnterMap": "SceneEnterWorldWulingXxx",
    // 或使用 "QuickTeleport": true
    "CameraSwipeDirection": "EnvironmentMonitoringSwipeScreenUp",
    "Heading": 90, // 可选；传送后先原地调整角色朝向
}
```

直拍条目不要填写 `MapName`、`MapAssert`、`MapPath`、`MapTarget`、`MapTargetTier`、`MapGoal` 或 `NoEnsureInitialMovementState`。可按实测结果配置 `Heading`；生成器会在传送后独立调用 `MapTrackerToward`，再进入拍照流程。生成器根据“传送入口与拍照方向完整，同时没有地图断言和寻路配置”识别直拍模式。仅含 `MissionId` / `Name` / `Id` 的 metadata-only 条目仍然属于未适配。

> [!IMPORTANT]
>
> `routes.json` 是严格 JSON：双引号、不允许行内注释、不允许尾随逗号。上述代码块里的 `//` 只是文档示意，写进真实文件会让 JSON 解析失败。
>
> `MissionId` 是 `model.mjs` 的匹配键，会与 `kite_station_i18n.json` 中的 `missionId` 精确匹配。`Name` 只供人工阅读，`Id` 只供人工搜索生成节点/文件名；如果与 zmdmap 当前数据不一致，生成器会直接刷新为当前正确值，不影响匹配。

> 重新生成 EnvironmentMonitoring 时，`sync-routes.mjs` 会先按 zmdmap 数据自动刷新 `MissionId` / `Name` / `Id`，并按 `MissionId` 排序。手写条目时必须填写 `MissionId`；如果 zmdmap 中存在新任务但 `routes.json` 没有对应条目，生成器会自动追加仅含 `MissionId` / `Name` / `Id` 的未适配占位条目，方便维护者看到待补路线。

### 4. 录制坐标和路径

如果传送点不能直接拍照，参考 [map-navigator.md](../components/map-navigator.md) 的 GUI 工具录制所需的 `MapAssert` / `MapPath`，复制 MapNavigateAction 的 `NAVMESH` 目标点填入 `MapTarget`，需要跨层目标时把 `target_tier` 填入 `MapTargetTier`，或复制 MapTrackerGoal 目标点填入 `MapGoal`。仅 `QuickTeleport + MapTarget/MapGoal` 不需要录制 `MapAssert`。随后在游戏中确认：

- `MapName` 与使用的工具一致：`MapPath` 路线填写 MapTracker 的 `map_name`（如 `map02_lv001` / 正则），`MapGoal` 路线填写可加载 NavMesh 的精确 MapTracker `map_name`（如 `map02_lv001`），`MapTarget` 路线填写 MapLocate 的 `zone_id`（如 `Wuling_Base`），可选的 `MapTargetTier` 填 MapNavigator `target_tier` 的区域名。两套标识不要混用。

- 拍照时摄像头需要往哪个方向滑（决定 `CameraSwipeDirection`）。
- 站位是否能让 `EnvironmentMonitoringTakePhoto` 走 `EnvironmentMonitoringEnterCameraMode`（自动朝向目标）成功；如果不行，会自动回退到 `EnvironmentMonitoringTakePhotoDirectly` + 手动滑屏 `${Id}AdjustCamera`。

### 5. 重新生成 Pipeline

```bash
# 在仓库根目录运行
pnpm generate:EnvironmentMonitoring

# 或在生成器目录分别执行
cd tools/pipeline-generate/EnvironmentMonitoring/generator
node sync-routes.mjs
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

确认生成出的两类文件：

- `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`
- `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`（`{Station}MonitoringTerminalLoop.next` 中包含 `[JumpBack]{Id}Job`）

这里的 `{Id}` 是生成结果里的节点 ID。通常直接看生成出的文件名即可确认；维护 `routes.json` 时不需要提前手算。

## 修改已有观察点路线

只调整路线/朝向（不变更英文名）：

1. 改 `tools/pipeline-generate/EnvironmentMonitoring/routes.json` 里对应条目。
2. 重新生成。常规情况下可直接在仓库根目录运行 `pnpm generate:EnvironmentMonitoring`；如果确认终端列表未变化，也可以只在 `tools/pipeline-generate/EnvironmentMonitoring/generator/` 目录运行 `node sync-routes.mjs && npx @joebao/maa-pipeline-generate`，无需重新生成 `Terminals.json`。
3. 提交 `routes.json` 与重生成的 `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`。

如果观察点的官方英文名变了，生成出的 `Id` / 文件名也会跟着变；重新生成后 `routes.json` 里的 `Id` 会同步刷新成新的最终模板 ID。

## 自检清单

提交前至少检查：

1. `tools/pipeline-generate/EnvironmentMonitoring/routes.json` 中新增/修改条目是否字段齐全。
2. `routes.json` 中新增条目的 `MissionId` 是否能匹配 `kite_station_i18n.json` 的 `missionId`；`Id` 由生成器自动刷新。
3. 已适配条目的传送入口已在真实 `EnterMap` 与 `QuickTeleport: true` 中至少选择一种，`CameraSwipeDirection` 已填写真实值；直拍路线没有任何地图与寻路字段，寻路路线则在 `MapPath` / `MapTarget` / `MapGoal` 中三选一，并按类型补齐真实 `MapAssert`。
4. 重生成的 `Terminals.json` 中各 `{Station}MonitoringTerminalLoop.next` 包含全部新 `[JumpBack]{Id}Job`，并以 `EnvironmentMonitoringTerminalFinish` 收尾。
5. 若填写了 `EnterMap`，其引用的 `Scene*` 节点确实存在于 `assets/resource/pipeline/SceneManager/` 与 `Interface/` 中。
6. `CameraSwipeDirection` 是 `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 四者之一。
7. **没有手改** `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` 或 `Terminals.json`（手改会被下次生成覆盖；如确需特殊节点，应在 `template.json` / `terminals-template.json` 中扩展）。
8. JSON 文件遵循 `.prettierrc` 格式（生成器自带 `format: true`，但提交前 `pnpm prettier --write` 一遍更稳）。

## 常见坑

- **手改生成产物**：直接编辑 `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` 或 `Terminals.json`，下次重新生成时改动会丢。改生成配置 / 更新 zmdmap 缓存后重新生成才是正确做法。
- **`MissionId` 与游戏数据对不上**：`routes.json` 条目里的 `MissionId` 才是匹配主键；`Name` / `Id` 只用于人工阅读和搜索。`MissionId` 匹配失败时生成器会提示该条目未使用，对应观察点会按未适配处理（仅接取并追踪）。
- **把 `Id` 当匹配键**：`Id` 只是最终模板节点 ID，方便搜索生成节点/文件名；匹配仍然只看 `MissionId`。
- **`Id` 与 `kite_station_i18n.json` 英文名漂移**：当游戏侧改英文名后，自动算出的 `Id` 会变，可能带来生成文件重命名或旧文件残留；重新生成后 `routes.json` 里的 `Id` 会同步刷新。
- **默认传送路线的 `EnterMap` 写了不存在的 Scene 节点**：生成本身不校验 Scene 引用，运行时会卡在 `GoTo{Id}NotAtStartPos`；只有明确启用 `QuickTeleport: true` 时才能省略。
- **`MapPath` / `MapTarget` / `MapGoal` 经过未解锁区域 / 战斗 / 互动物**：MapTracker 与 MapNavigateAction 都不处理战斗、剧情、过图和机关交互，路径只能选纯通行段。
- **把缺路线数据误写成直拍**：只有游戏实测确认传送落点能完成拍照时，才能保留“传送入口 + `CameraSwipeDirection`”的精简配置；缺少真实路线数据时应继续保留 metadata-only 未适配条目。
- **`Station` 新增但 `Locations.json` / `EnvironmentMonitoringLoop.next` 没同步**：新终端无法被识别进入，所有观察点都跑不到。
- **`anchor` 占位符名一致性**：`template.json` 中 `anchor` 的 key 名 `EnvironmentMonitoringBackToTerminal` 必须与 `TakePhoto.json` 中的 `[Anchor]EnvironmentMonitoringBackToTerminal` 保持完全一致，否则 anchor 机制失效。
- **「生成成功 ≠ 已完整适配」**：没有 `routes.json` 条目、或条目存在但必填字段缺失的观察点会生成成降级流程，只接取并追踪，不会前往拍照。完整自动化必须选择真实传送入口并配置 `CameraSwipeDirection`；随后要么使用经过实测的传送后直拍精简写法，要么补齐地图断言和寻路配置。
