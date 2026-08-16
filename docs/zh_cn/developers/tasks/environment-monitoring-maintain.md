# 开发手册 - EnvironmentMonitoring 维护文档

本文说明 `EnvironmentMonitoring`（环境监测）任务的 Pipeline 组织、路线数据、终端分组、自动生成机制及新观察点的接入方式。

环境监测的核心特点是 **「数据驱动 + 模板批量生成」**：每个观察点对应的 Pipeline JSON 不直接手写，而是通过 [`@joebao/maa-pipeline-generate`](https://www.npmjs.com/package/@joebao/maa-pipeline-generate) 工具，将 `tools/pipeline-generate/EnvironmentMonitoring/` 下的模板/路线配置和 `tools/pipeline-generate/data/environment_monitoring.json` 批量渲染到 `assets/resource/pipeline/EnvironmentMonitoring/` 中。维护工作的重心在 **生成配置与 zmdmap 精简游戏数据**，而不是手改 JSON。

> [!WARNING]
>
> `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` 与 `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` 都是 **生成产物**。手改这些文件会在下次重新生成时被覆盖。所有维护都应该改 `tools/pipeline-generate/EnvironmentMonitoring/` 下的生成配置，或通过 `pnpm fetch:zmdmap` 更新 zmdmap 精简游戏数据。

## 概览

环境监测的核心维护点如下：

| 模块 | 路径 | 作用 |
| ------------------- | --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 任务入口 | `assets/tasks/EnvironmentMonitoring.json` | interface 任务定义（无可配置选项，控制器 = Win32-Front / Wlroots / ADB） |
| 主流程 Pipeline | `assets/resource/pipeline/EnvironmentMonitoring.json` | 主入口节点 `EnvironmentMonitoringMain`，循环识别两个监测终端 |
| 终端分组（生成） | `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json` | 城郊监测终端 / 首墩监测终端的入口节点与各自的观察点 `next` 列表（**生成**） |
| 终端跳转 | `assets/resource/pipeline/EnvironmentMonitoring/Locations.json` | `EnvironmentMonitoringGoTo*` 与 `Select*` 节点，从主菜单进入对应终端 |
| 拍照流程 | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json` | 进入拍照模式、调整朝向、识别拍照按钮、达成目标后回到终端 |
| 摄像头滑动 | `assets/resource/pipeline/EnvironmentMonitoring/TakePhoto.json` | `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 四向调整朝向 |
| 公共按钮 | `assets/resource/pipeline/EnvironmentMonitoring/Button.json` | `TrackMissionButton` 等环境监测专用通用按钮 |
| 观察点节点（生成） | `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` | **每个观察点一份 JSON**，由模板渲染（**生成**）；`Id` 由 `model.mjs` 自动生成，通常不用手写 |
| 观察点模板 | `tools/pipeline-generate/EnvironmentMonitoring/generator/template.json` | 单观察点 Pipeline 模板（识别文本、接取/前往、传送、寻路、拍照） |
| 终端模板 | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-template.json` | 终端分组节点模板 |
| 路线/坐标数据 | `tools/pipeline-generate/EnvironmentMonitoring/routes.json` | 按观察点 `MissionId` 匹配的路线覆盖（传送点、地图、路径、摄像头滑动方向）；`Name` 仅供人工阅读，`Id` 是最终模板节点 ID，方便搜索生成节点/文件名 |
| 路线 JSON Schema | `tools/schema/environment_monitoring_routes.schema.json` | `routes.json` 的字段约束（必填项、枚举、坐标数组形状），通过 `.vscode/settings.json` 自动关联，提供 IDE 字段补全和校验 |
| 失败收集参数 Schema | `tools/schema/components/failure_collector.schema.json` | 通用失败收集 Custom Action 的参数约束；动作名称注册在 `tools/schema/custom.action.schema.json` |
| 路线同步逻辑 | `tools/pipeline-generate/EnvironmentMonitoring/generator/sync-routes.mjs` | 在生成前自动同步 `routes.json` 的 `MissionId` / `Name` / `Id`，并按 `MissionId` 排序 |
| 路线解析逻辑 | `tools/pipeline-generate/EnvironmentMonitoring/generator/route-resolver.mjs` | 将 `routes.json` 条目解析为模板需要的寻路识别/动作参数，并统一处理未适配降级 |
| 规范化任务模型 | `tools/pipeline-generate/EnvironmentMonitoring/generator/model.mjs` | 统一读取 zmdmap 精简游戏数据与 `routes.json`，生成路线和终端模板共享的观察点任务模型 |
| 终端列表数据 | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-data.mjs` | 从 `model.mjs` 的规范化任务和自动派生的终端列表生成各终端 `next` |
| zmdmap 精简游戏数据 | `tools/pipeline-generate/data/environment_monitoring.json` | zmdmap 数据 CI 从 TableCfg 裁剪并发布的终端、观察点及五语言名称；MaaEnd 通过 `pnpm fetch:zmdmap` 更新 |
| 生成器配置 | `tools/pipeline-generate/EnvironmentMonitoring/generator/config.json` | 单观察点输出配置：`outputPattern: "${Station}/${Id}.json"` |
| 终端生成器配置 | `tools/pipeline-generate/EnvironmentMonitoring/generator/terminals-config.json` | 合并到单文件的终端输出配置：`outputFile: "Terminals.json"` |
| 多语言文案 | `assets/locales/interface/*.json` | `task.EnvironmentMonitoring.*` 的 label / description（任务级；观察点名走 OCR） |
| 通用组件依赖 | `agent/go-service/maptracker/` / `3rdparty/maa-copilot` | 寻路统一走 `MapLocateAssertLocation` + `MapNavigateAction`；少量尚未迁移的存量路线仍在用原有实现（详见 [map-locator.md](../components/map-locator.md)、[map-navigator.md](../components/map-navigator.md)、[map-tracker.md](../components/map-tracker.md)） |
| 场景跳转依赖 | `assets/resource/pipeline/SceneManager/`、`Interface/` | `SceneEnterWorldWuling*`、`SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring`（详见 [scene-manager.md](../scene-manager.md)） |

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
                      │              └─ 有 Heading → GoTo{Id}Move（仅原地调整朝向）
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
GoTo{Id}Move                         （寻路动作；直拍有 Heading 时只原地调整朝向）
  └─ {Id}TakePhoto
       ├─ anchor: EnvironmentMonitoringBackToTerminal → ${GoToMonitoringTerminal}
       ├─ anchor: EnvironmentMonitoringAdjustCamera   → ${Id}AdjustCamera
       └─ next: EnvironmentMonitoringTakePhoto
EnvironmentMonitoringTakePhoto       （进入拍照模式 → 朝向 → 拍照）
  └─ [Anchor]EnvironmentMonitoringBackToTerminal
       └─ EnvironmentMonitoringGoTo{Outskirts|MarkerStone}MonitoringTerminal
```

每个 `{Id}Job` 仍负责识别观察点列表项，命中后通过通用 `FailureCollectorRunTask` 执行 `{Id}Execute` 路线。生成器根据环境监测数据的五语言 `mission.names` 为新任务补齐 `task.EnvironmentMonitoring.route.{Id}.failed`；已有失败提示会保留，避免覆盖人工调整。若路线内部任意节点失败，包装 Action 会记录 `{Id}Failed`、运行 `recovery_task` 返回当前监测终端，并向外返回成功以继续后续路线；全部终端遍历结束后，`EnvironmentMonitoringFinish` 通过 `FailureCollectorFinish` 按失败顺序依次调用这些提示节点，再返回总任务失败。Agent 不直接输出用户提示。

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

默认情况下，`Id` 从 `environment_monitoring.json` 中该任务的 `names.en_us` 转 PascalCase 得到，规则在 `common.mjs` 的 `buildDefaultId()` / `toPascalCase()`。如果英文名缺失，会回退到 `mission_id` / `entrust_index`；如果出现重复，`ensureUniqueId()` 会自动追加后缀。

维护 `routes.json` 时不需要手算 `Id`。路线匹配键是 `MissionId`，`Id` 会在重新生成时自动写入 `routes.json`，等价于最终模板使用的节点名前缀，方便直接搜索生成节点和文件名。

> [!IMPORTANT]
>
> 不要把 `Id` 当作展示文案。展示文案走官方名称 / OCR；`Name` 是 routes.json 的人工备注，`Id` 只用于拼接节点名、文件名（`outputPattern: "${Station}/${Id}.json"`），并由生成器自动刷新。

### 终端分组（`Station`）

由 `model.mjs` 从观察点所属终端对应的 `environment_monitoring.json.terminals[terminalId].names.en_us` PascalCase 而来。当前仓库内只有两组：

| 中文名 | Station ID | 对应 terminalId | `GoToMonitoringTerminal` 锚点 |
| ------------ | ------------------------------- | ------------------- | -------------------------------------------------------- |
| 城郊监测终端 | `OutskirtsMonitoringTerminal` | `kitestation_002_1` | `EnvironmentMonitoringGoToOutskirtsMonitoringTerminal` |
| 首墩监测终端 | `MarkerStoneMonitoringTerminal` | `kitestation_004_1` | `EnvironmentMonitoringGoToMarkerStoneMonitoringTerminal` |

如果出现新的 Station，**生成器侧（`routes.json` + `model.mjs`）零改动**：`MONITORING_TERMINAL_IDS` 自动从 `environment_monitoring.json` 派生，`GoToMonitoringTerminal` 锚点名按 `EnvironmentMonitoringGoTo{Station}` 模板拼接。但生成出来的 Pipeline 引用的下列**手写联动节点**必须先补齐，否则 MaaFramework 运行时会报「引用了未定义的任务」：

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

`data.mjs` 的默认导出是数组，每个元素 = 一个观察点的渲染上下文（字段名与 `template.json` 中 `${Xxx}` 占位符对应）。`pnpm generate:EnvironmentMonitoring` 会先同步 zmdmap 精简游戏数据，再调用 `sync-routes.mjs` 刷新上一级 `routes.json`；随后 `model.mjs` 只读 `routes.json` 与 `environment_monitoring.json`，通过 `route-resolver.mjs` 装配规范化任务，`data.mjs` 再投影出最终行：

| 字段 | 来源 |
| -------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Station` | `environment_monitoring.json` 的英文站名（PascalCase） |
| `Id` | 默认由官方英文名 PascalCase 自动生成；会同步写回 `routes.json`，等价于最终模板使用的节点 ID |
| `Name` | 来自 `environment_monitoring.json` 的中文名；`MissionId` 只作为 `model.mjs` 匹配 `routes.json` 的主键，不透传给模板 |
| `GoToMonitoringTerminal` | 由 `Station` 决定 |
| `EnterMap` | `routes.json[*].EnterMap`，默认传送入口；可填写任意已定义、能作为 SubTask 正常返回的 Pipeline 节点名，不限制名称前缀；启用 `QuickTeleport` 时不会使用，可省略 |
| `QuickTeleport` | `routes.json[*].QuickTeleport`，可选布尔值，默认 `false`；启用后从追踪任务地图依次点击“前往传送”和“传送”，不调用 `EnterMap` |
| `NavZoneId` / `NavAssert` / `NavPath` | `routes.json[*]`，寻路路线的默认写法；`NavPath` 必填，普通传送还需 `NavZoneId` / `NavAssert`，生成 `MapLocateAssertLocation` + `MapNavigateAction`（`Heading` 会追加 `HEADING` 动作）；配置后不再填写下面那组遗留字段，路线自己接管传送落点，传送后不再复核起点 |
| `MapName` / `MapAssert` / `MapTarget` / `MapTargetTier` / `MapTargetDeckY` | `routes.json[*]`，单目标点的简写，同样生成 `MapLocateAssertLocation` + `MapNavigateAction` 的 `NAVMESH` 目标点，`MapTargetTier` 可选生成 `target_tier`，`MapTargetDeckY` 可选生成 `target_deck_y`；新增路线直接用上面的 `NavPath` 写法 |
| `MapPath` / `MapGoal` | `routes.json[*]`，遗留字段，只保留给尚未迁移的存量路线，新增路线不要使用 |
| `CameraSwipeDirection` | `routes.json[*]`，必须是 `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 之一 |
| `CameraMaxHit` | `routes.json[*].CameraMaxHit`，缺省为 `2`；对应 `${Id}AdjustCamera` 滑屏动作的最大命中次数 |
| `OcrReplace` | 由 `routes.json[*].Replace` 透传到 `Check${Id}Text.replace` 与 `In${Id}Mission.replace`；用于按任务配置任务列表和任务详情页 OCR 的易混字符替换，不影响路线是否已适配的判断 |
| `ExpectedText` | 由 `environment_monitoring.json` 的 `mission.names` 自动展开（5 语言，英文转柔性正则） |
| `InExpectedText` | 由 `environment_monitoring.json` 的 `mission.shot_target_names` 自动展开 |
| `TrackOrGoToNext` / `AfterTrackNext` / `AfterAlreadyTrackedNext` | 由 `data.mjs` 根据路线是否完整及 `QuickTeleport` 自动决定：默认进入 `GoTo${Id}`；快捷传送时，开始追踪后等待任务地图，已追踪则先点击定位图标打开任务地图；未适配时进入 `${Id}NotAdapted` |
| `GoToNext` / `AfterTeleportDescription` / `AfterTeleportNext` | 由 `data.mjs` 根据传送入口和路线类型自动决定：传送后直拍始终执行真实传送并进入 `${Id}TakePhoto`；`QuickTeleport` 与任一寻路方式组合时传送后直接进入 `GoTo${Id}Move`；普通传送的 `NavPath` / `MapTarget` / `MapGoal` 直接进入 `GoTo${Id}Move`，只有 `MapPath` 返回 `GoTo${Id}StartPos` 复核落点 |

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

# 只同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap

# 如果已经更新过环境监测数据，也可以在 tools/pipeline-generate/EnvironmentMonitoring/generator/ 目录下单独渲染：

# 0) 同步 routes.json 的 MissionId/Name/Id
node sync-routes.mjs

# 1) 渲染所有观察点 Pipeline
pnpm exec maa-pipeline-generate

# 2) 渲染终端入口
pnpm exec maa-pipeline-generate --config terminals-config.json
```

> [!NOTE]
>
> `model.mjs` 在渲染时如果某观察点没有 `routes.json` 条目，或条目存在但任一必填字段缺失（`null` / 空字符串 / 空数组），会 `console.warn` 并把该观察点视为 **未适配**。未适配观察点仍会生成 Pipeline，但运行时只会接取并追踪任务，在 `${Id}NotAdapted` 提示后结束，不会执行传送或寻路。

## 关键依赖

### 寻路组件

观察点的寻路统一交给 MapNavigator，只有少数尚未迁移的存量路线还在跑原有实现：

- `MapLocateAssertLocation`（识别）：根据当前小地图判断是否在 `NavAssert` / `MapAssert` 矩形内，普通传送路线用它决定要不要调用 `EnterMap`；`QuickTeleport` 从固定传送落点直接开始寻路，不进入该节点，可省略 `NavZoneId` / `NavAssert`。
- `MapNavigateAction`（动作）：`NavPath` 原样作为它的 path，其中 `NAVMESH` 航点可按需携带 `target_tier` / `target_deck_y`；断言坐标取 `NavZoneId` 分区。遗留的 `MapTarget` 简写会生成一个 `NAVMESH` 动作，`MapTargetTier` / `MapTargetDeckY` 分别透传为 `target_tier` / `target_deck_y`。两种写法传送后都直接进入寻路动作，不再复核起点。
- `${Id}TakePhoto`（包装节点）：为寻路和直拍路线统一设置 `EnvironmentMonitoringBackToTerminal` / `EnvironmentMonitoringAdjustCamera` anchor，再进入公共拍照流程。
- 传送后直拍不执行地图断言或寻路，配置 `Heading` 时只在原地调整角色朝向再拍照。
- 存量的 `MapPath` / `MapGoal` 路线仍走原有实现：`MapPath` 在普通传送后会再复核一次 `MapAssert`。这两种写法只保留给还没录制新路线的观察点，不要用于新增路线。

详细参数与坐标录制方式见 [map-locator.md](../components/map-locator.md) 与 [map-navigator.md](../components/map-navigator.md)。

### 传送入口

默认传送路线的 `EnterMap` 字段必须填写 `assets/resource/pipeline/` 中已存在的 Pipeline 节点名。名称不要求以 `Scene` 开头，也不限制字符组合；普通 SceneManager 万能跳转与封装了传送、交互等流程的任务入口均可使用，但节点必须能作为 SubTask 完整执行后正常返回。启用 `QuickTeleport: true` 时不会调用 `EnterMap`，该字段可以省略。

`model.mjs` 通过判断 `routes.json` 条目是否完整决定是否进入寻路/拍照流程，未适配点会直接走 `${Id}NotAdapted` 分支。所有已适配条目都必须在真实 `EnterMap` 与 `QuickTeleport: true` 中至少选择一种传送入口，并配置 `CameraSwipeDirection`。如果传送落点已经满足拍照条件，则省略全部地图断言和寻路字段，生成器自动进入直拍分支；否则配置 `NavPath`，普通传送寻路路线还必须有 `NavZoneId` / `NavAssert` 断言配置，快捷传送寻路路线可同时省略二者。

### `routes.json` 配置类型

所有完整适配条目都需要元数据、`CameraSwipeDirection`，以及 `EnterMap` / `QuickTeleport: true` 中的一种传送入口。不同路线类型的字段组合如下：

| 类型 | 地图与路线字段 | 断言矩形 | 运行行为 |
| ----------------------------- | ------------------------------------------------------------------------------------------ | --------------------------- | ------------------------------------------ |
| metadata-only | 仅 `MissionId` / `Name` / `Id` | 不填 | 仅接取并追踪，不传送或拍照 |
| 传送后直拍 | 不填任何地图和寻路字段；可选 `Heading` | 不填 | 传送 →（可选原地调整朝向）→ 拍照 |
| 寻路（推荐写法） | `NavPath`；普通传送再加 `NavZoneId`，可选 `Heading` | `NavAssert`，快捷传送可省略 | `MapNavigateAction` 寻路 → 拍照 |
| `MapTarget`（简写） | `MapName` + `MapTarget`；跨层时可加 `MapTargetTier`，目标点有重叠面时可加 `MapTargetDeckY` | `MapAssert`，快捷传送可省略 | `MapNavigateAction` 的 NAVMESH 寻路 → 拍照 |
| `MapPath` / `MapGoal`（遗留） | `MapName` + 对应字段；可选 `Heading` / `NoEnsureInitialMovementState` | `MapAssert`，快捷传送可省略 | 原有实现寻路 → 拍照，仅存量路线保留 |

`CameraMaxHit` 和 `Replace` 适用于所有已适配路线，不改变路线类型。直拍配置只能用于已经游戏实测确认的传送落点；缺少路线数据时继续保留 metadata-only 状态。新增寻路路线一律用 `NavPath`，普通传送再补 `NavZoneId` / `NavAssert`；最后两行只是存量路线的现状记录。

### 主菜单入口

环境监测主入口节点 `EnvironmentMonitoringMain` 通过 `[JumpBack]SceneEnterMenuRegionalDevelopmentWulingEnvironmentMonitoring` 进入终端选择界面。该节点维护在 `assets/resource/pipeline/Interface/InScene/Region.json`，新增地区监测终端时需要确认主菜单入口已能进入对应界面。

## 添加新观察点

新增的观察点一般来自游戏更新，体现在 `environment_monitoring.json` 中多出一条 `mission`。维护流程：

> [!TIP]
>
> 如果你在使用支持 AI Skill 的客户端（如 Claude Code 或 GitHub Copilot），可以直接调用 **`environment-monitoring-add-route` skill**，它会自动检测缺失条目并通过交互式问答帮你填写 `routes.json`，省去手动查表的步骤。

### 1. 更新游戏数据

运行 `pnpm fetch:zmdmap`，会按 `tools/pipeline-generate/data/version.txt` 检查 zmdmap 数据版本，并在版本变化时下载 `tools/pipeline-generate/data/environment_monitoring.json`。原始 TableCfg 的解析由 zmdmap 数据 CI 中的 `data/scripts/environment_monitoring_data.py` 完成，不属于 MaaEnd 日常生成流程。

### 2. 检查路线适配状态

对比 `environment_monitoring.json` 中各终端的 `missions` 与 `routes.json` 条目，确认每个观察点的状态。匹配方式是 `mission_id` 对 `routes.json` 中的 `MissionId`，而不是 `Name` 或 `Id`：

- **未适配**：`routes.json` 没有该观察点，或条目存在但缺失任一必填字段（含 `null` / 空字符串 / 空数组） → 生成后只会接取并追踪。
- **准备适配**：需要让该观察点自动前往并拍照 → 走步骤 3，补齐真实路线。

### 3. 在 `routes.json` 中新增/补全条目

`tools/pipeline-generate/EnvironmentMonitoring/routes.json`：

```jsonc
{
    "MissionId": "m1m30",                    // 必须与 environment_monitoring.json 中的 mission_id 匹配
    "Name": "我的新观察点",                  // 中文名，仅供人工阅读
    "Id": "MyNewObservationPoint",           // 最终模板节点 ID，仅供人工搜索节点/文件名
    "EnterMap": "SceneEnterWorldWulingXxx", // 已定义且能作为 SubTask 正常返回的 Pipeline 节点
    // "QuickTeleport": true,             // 可选；启用后从追踪任务地图快捷传送，EnterMap 可省略
    "NavZoneId": "Wuling_Base",             // MapLocate 的 zone_id，用于解释 NavAssert 坐标
    "NavAssert": [x, y, w, h],              // 起点判断矩形；普通传送必填，快捷传送可省略
    "NavPath": [
        // MapNavigateAction 的 path，直接从 MapNavigator 录制结果复制
        { "action": "ZONE", "zone_id": "Wuling_Base" },
        [x1, y1],
        [x2, y2]
    ],
    "CameraSwipeDirection": "EnvironmentMonitoringSwipeScreenUp", // 朝向调整方向
    // "CameraMaxHit": 2,  // 可选；滑屏最大命中次数，默认为 2；拍摄目标较难对准时可适当调大
    // "Replace": [["売", "壳"]] // 可选；任务列表和任务详情页 OCR 易混字符替换
    // "Heading": 90       // 可选；在路线末尾追加一个 HEADING 动作，把角色朝向转到该角度
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

直拍条目不要填写任何地图断言和寻路字段（`NavZoneId` / `NavAssert` / `NavPath`，以及遗留的 `MapName` / `MapAssert` / `MapPath` / `MapTarget` / `MapTargetTier` / `MapTargetDeckY` / `MapGoal` / `NoEnsureInitialMovementState`）。可按实测结果配置 `Heading`，生成器会在传送后先原地调整朝向，再进入拍照流程。生成器根据“传送入口与拍照方向完整，同时没有地图断言和寻路配置”识别直拍模式。仅含 `MissionId` / `Name` / `Id` 的 metadata-only 条目仍然属于未适配。

> [!IMPORTANT]
>
> `routes.json` 是严格 JSON：双引号、不允许行内注释、不允许尾随逗号。上述代码块里的 `//` 只是文档示意，写进真实文件会让 JSON 解析失败。
>
> `MissionId` 是 `model.mjs` 的匹配键，会与 `environment_monitoring.json` 中的 `mission_id` 精确匹配。`Name` 只供人工阅读，`Id` 只供人工搜索生成节点/文件名；如果与当前游戏数据不一致，生成器会直接刷新为当前正确值，不影响匹配。

> 重新生成 EnvironmentMonitoring 时，`sync-routes.mjs` 会先按环境监测数据自动刷新 `MissionId` / `Name` / `Id`，并按 `MissionId` 排序。手写条目时必须填写 `MissionId`；如果数据中存在新任务但 `routes.json` 没有对应条目，生成器会自动追加仅含 `MissionId` / `Name` / `Id` 的未适配占位条目，方便维护者看到待补路线。

### 4. 录制坐标和路径

如果传送点不能直接拍照，参考 [map-navigator.md](../components/map-navigator.md) 用 GUI 工具录制路线，把录出来的起点矩形填入 `NavAssert`、整条 path 原样复制进 `NavPath`。`QuickTeleport` 路线不执行起点断言，不需要录制 `NavZoneId` / `NavAssert`。若 `NAVMESH` 终点存在上下重叠可走面，在工具中选定目标面，让导出的航点携带 `target_deck_y`。随后在游戏中确认：

- `NavZoneId` 填 MapLocate 的 `zone_id`（如 `Wuling_Base`），它决定 `NavAssert` 坐标按哪个分区解释；终点落在分层平台上时，`NavPath` 里的 `NAVMESH` 动作按 tier 坐标写并带上 `target_tier`；目标点存在上下重叠面时带上工具选出的 `target_deck_y`。
- 仅含 `NAVMESH` 的 `NavPath` 不需要在开头添加 `ZONE`；`NAVMESH` 会从 MapLocator 当前定位自动确定起点区域。`ZONE` 只为后续手录坐标点声明和校验分区，多分区或过图路径应保留录制工具导出的 `ZONE`。

- 拍照时摄像头需要往哪个方向滑（决定 `CameraSwipeDirection`）。
- 站位是否能让 `EnvironmentMonitoringTakePhoto` 走 `EnvironmentMonitoringEnterCameraMode`（自动朝向目标）成功；如果不行，会自动回退到 `EnvironmentMonitoringTakePhotoDirectly` + 手动滑屏 `${Id}AdjustCamera`。

### 5. 重新生成 Pipeline

```bash
# 在仓库根目录运行
pnpm generate:EnvironmentMonitoring

# 或在生成器目录分别执行
cd tools/pipeline-generate/EnvironmentMonitoring/generator
node sync-routes.mjs
pnpm exec maa-pipeline-generate
pnpm exec maa-pipeline-generate --config terminals-config.json
```

确认生成出的两类文件：

- `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`
- `assets/resource/pipeline/EnvironmentMonitoring/Terminals.json`（`{Station}MonitoringTerminalLoop.next` 中包含 `[JumpBack]{Id}Job`）

这里的 `{Id}` 是生成结果里的节点 ID。通常直接看生成出的文件名即可确认；维护 `routes.json` 时不需要提前手算。

## 修改已有观察点路线

只调整路线/朝向（不变更英文名）：

1. 改 `tools/pipeline-generate/EnvironmentMonitoring/routes.json` 里对应条目。
2. 重新生成。常规情况下可直接在仓库根目录运行 `pnpm generate:EnvironmentMonitoring`；如果确认终端列表未变化，也可以只在 `tools/pipeline-generate/EnvironmentMonitoring/generator/` 目录运行 `node sync-routes.mjs && pnpm exec maa-pipeline-generate`，无需重新生成 `Terminals.json`。
3. 提交 `routes.json` 与重生成的 `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json`。

如果观察点的官方英文名变了，生成出的 `Id` / 文件名也会跟着变；重新生成后 `routes.json` 里的 `Id` 会同步刷新成新的最终模板 ID。

## 自检清单

提交前至少检查：

1. `tools/pipeline-generate/EnvironmentMonitoring/routes.json` 中新增/修改条目是否字段齐全。
2. `routes.json` 中新增条目的 `MissionId` 是否能匹配 `environment_monitoring.json` 的 `mission_id`；`Id` 由生成器自动刷新。
3. 已适配条目的传送入口已在真实 `EnterMap` 与 `QuickTeleport: true` 中至少选择一种，`CameraSwipeDirection` 已填写真实值；直拍路线没有任何地图与寻路字段，新增寻路路线配置 `NavPath`，且普通传送路线补齐真实 `NavZoneId` / `NavAssert`；遗留路线仍在 `MapPath` / `MapTarget` / `MapGoal` 中三选一。
4. 重生成的 `Terminals.json` 中各 `{Station}MonitoringTerminalLoop.next` 包含全部新 `[JumpBack]{Id}Job`，并以 `EnvironmentMonitoringTerminalFinish` 收尾。
5. 若填写了 `EnterMap`，其引用的节点确实存在于 `assets/resource/pipeline/`，并能作为 SubTask 完整执行后正常返回。
6. `CameraSwipeDirection` 是 `EnvironmentMonitoringSwipeScreen{Up/Down/Left/Right}` 四者之一。
7. **没有手改** `assets/resource/pipeline/EnvironmentMonitoring/{Station}/*.json` 或 `Terminals.json`（手改会被下次生成覆盖；如确需特殊节点，应在 `template.json` / `terminals-template.json` 中扩展）。
8. JSON 文件遵循 `.prettierrc` 格式（生成器自带 `format: true`，但提交前 `pnpm prettier --write` 一遍更稳）。

## 常见坑

- **手改生成产物**：直接编辑 `assets/resource/pipeline/EnvironmentMonitoring/{Station}/{Id}.json` 或 `Terminals.json`，下次重新生成时改动会丢。改生成配置 / 更新环境监测数据后重新生成才是正确做法。
- **`MissionId` 与游戏数据对不上**：`routes.json` 条目里的 `MissionId` 才是匹配主键；`Name` / `Id` 只用于人工阅读和搜索。`MissionId` 匹配失败时生成器会提示该条目未使用，对应观察点会按未适配处理（仅接取并追踪）。
- **把 `Id` 当匹配键**：`Id` 只是最终模板节点 ID，方便搜索生成节点/文件名；匹配仍然只看 `MissionId`。
- **`Id` 与 `environment_monitoring.json` 英文名漂移**：当游戏侧改英文名后，自动算出的 `Id` 会变，可能带来生成文件重命名或旧文件残留；重新生成后 `routes.json` 里的 `Id` 会同步刷新。
- **默认传送路线的 `EnterMap` 写了不存在或无法正常返回的节点**：生成本身不校验节点引用及运行行为，运行时会在 `GoTo{Id}NotAtStartPos` 失败；只有明确启用 `QuickTeleport: true` 时才能省略。
- **路线经过未解锁区域 / 战斗 / 互动物**：寻路组件不处理战斗、剧情、过图和机关交互，路径只能选纯通行段。
- **把缺路线数据误写成直拍**：只有游戏实测确认传送落点能完成拍照时，才能保留“传送入口 + `CameraSwipeDirection`”的精简配置；缺少真实路线数据时应继续保留 metadata-only 未适配条目。
- **`Station` 新增但 `Locations.json` / `EnvironmentMonitoringLoop.next` 没同步**：新终端无法被识别进入，所有观察点都跑不到。
- **`anchor` 占位符名一致性**：`template.json` 中 `anchor` 的 key 名 `EnvironmentMonitoringBackToTerminal` 必须与 `TakePhoto.json` 中的 `[Anchor]EnvironmentMonitoringBackToTerminal` 保持完全一致，否则 anchor 机制失效。
- **「生成成功 ≠ 已完整适配」**：没有 `routes.json` 条目、或条目存在但必填字段缺失的观察点会生成成降级流程，只接取并追踪，不会前往拍照。完整自动化必须选择真实传送入口并配置 `CameraSwipeDirection`；随后要么使用经过实测的传送后直拍精简写法，要么补齐地图断言和寻路配置。
