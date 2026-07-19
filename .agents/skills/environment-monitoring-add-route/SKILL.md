---
name: environment-monitoring-add-route
description: "新增、补全或改写 MaaEnd EnvironmentMonitoring 环境监测观察点路线配置。凡是用户要求适配 zmdmap / kite_station_i18n 新观察点、补充 routes.json、配置传送后直拍、选择 MapPath / MapTarget / MapGoal、配置 MapTargetTier / Heading / Replace，或重新生成环境监测 Pipeline 时都应使用。会先同步 metadata-only 条目，按 MissionId 原位更新，验证传送点与路线字段，并自动同步五语言失败提示。"
argument-hint: "可选：观察点名称，以及录制好的 EnterMap、MapAssert、MapPath / MapTarget / MapGoal 等路线数据"
---

# 环境监测观察点路线维护

## 目标与边界

维护 `tools/pipeline-generate/EnvironmentMonitoring/routes.json`，再通过生成器更新 `assets/resource/pipeline/EnvironmentMonitoring/`。

本 skill 只接收由游戏实测或 MapNavigator 工具录制的路线数据。不要猜测传送点、地图名、坐标、tier、角色朝向或 OCR 替换；缺少真实数据时保留 metadata-only 条目，让生成器走“仅接取并追踪”的未适配分支。

以这些文件为当前事实来源：

- `tools/schema/environment_monitoring_routes.schema.json`：允许字段与组合约束
- `tools/pipeline-generate/EnvironmentMonitoring/generator/route-resolver.mjs`：运行时字段语义
- `tools/pipeline-generate/EnvironmentMonitoring/README.md`：生成命令与维护示例
- `docs/zh_cn/developers/tasks/environment-monitoring-maintain.md`：完整架构和验收说明

## 当前生成机制

运行 `generator/sync-routes.mjs` 时会：

1. 从 `tools/pipeline-generate/data/kite_station_i18n.json` 收集观察点；
2. 按 `MissionId` 刷新 `Name` 和 `Id`；
3. 为数据源中新增的任务创建仅含 `MissionId` / `Name` / `Id` 的 metadata-only 条目；
4. 按 `MissionId` 排序 `routes.json`；
5. 自动补齐五语言 `task.EnvironmentMonitoring.route.{Id}.failed`，保留已有人工文案。

因此不要重复追加同一 `MissionId`，也不要依赖手工排列条目顺序。

## 字段说明

### 自动维护的元数据

| 字段        | 来源                         | 说明                                                 |
| ----------- | ---------------------------- | ---------------------------------------------------- |
| `MissionId` | zmdmap `missionId`           | 唯一匹配主键；查找和更新条目时以它为准               |
| `Name`      | `mission.name["zh-CN"]`      | 仅供阅读；同步时会刷新                               |
| `Id`        | `mission.name["en-US"]` 派生 | 节点名和输出文件名前缀；同步时会刷新，不作为匹配主键 |

不要询问用户提供这三个字段；先从数据源自动匹配。用户给出的中英文任务名、拍照目标名或 `Id` 只用于定位对应 mission。

### 完整适配必填字段

| 字段                   | 说明                                                                                                                      |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| 传送入口               | 默认填写真实 `EnterMap`（通常为 `SceneEnterWorld*`）；或启用 `QuickTeleport: true`，从追踪任务地图快捷传送                |
| 路线方式               | 传送点可直接拍照时不填地图与寻路字段；否则 `MapPath` / `MapTarget` / `MapGoal` 必须且只能填写一个                         |
| `MapName`              | 仅寻路路线必填。`MapPath`：MapTracker `map_name`；`MapGoal`：精确 MapTracker `map_name`；`MapTarget`：MapLocate `zone_id` |
| `MapAssert`            | 直拍路线不填；普通传送和 `QuickTeleport + MapPath` 必填，`QuickTeleport + MapTarget/MapGoal` 可省略                       |
| `CameraSwipeDirection` | `EnvironmentMonitoringSwipeScreenUp/Down/Left/Right`，用于进入拍照后的摄像头调整                                          |

### 四种路线模式

| 模式        | 数据格式          | 生成动作            | 适用场景                                                                    |
| ----------- | ----------------- | ------------------- | --------------------------------------------------------------------------- |
| `MapPath`   | `[[x1, y1], ...]` | `MapTrackerMove`    | 需要按实录路径逐点行走；传送后会再次用 `MapAssert` 复核固定起点             |
| `MapTarget` | `[x, y]`          | `MapNavigateAction` | 使用 MapLocate / MapNavigator 的 NAVMESH 目标；快捷传送时可省略 `MapAssert` |
| `MapGoal`   | `[x, y]`          | `MapTrackerGoal`    | 使用 MapTracker NavMesh 自动寻路；快捷传送时可省略 `MapAssert`              |
| 传送后直拍  | 不配置地图字段    | 可选转向后拍照      | 传送落点已经满足拍照条件；可配置 `Heading`，但不配置地图断言或寻路字段      |

三种寻路模式都只适合普通可通行路线，不负责战斗、剧情、过图、机关或交互。传送后直拍必须经过游戏实测确认；不能因为缺少路线数据就把未适配条目写成直拍。遇到这些情况不要用更多重试或硬延迟掩盖，应保留未适配状态或重新设计真实可通行路线。

### 可选字段

| 字段                           | 使用条件与写法                                                                                                         |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `MapTargetTier`                | 仅用于 `MapTarget`。目标点取自 tier 底图且与起点不在同一层时填写 MapNavigator `target_tier`；否则省略                  |
| `CameraMaxHit`                 | 摄像头最大滑屏次数，默认 2；只有实测需要其他值时才写                                                                   |
| `Replace`                      | OCR 易混字符替换表 `[["误识别", "正确字符"], ...]`；仅有实际误识别证据时填写                                           |
| `Heading`                      | 可选。进入拍照模式前的角色朝向，范围 `[0, 360)`；直拍路线会在传送后独立调用 `MapTrackerToward`                         |
| `NoEnsureInitialMovementState` | 仅对 `MapPath` / `MapGoal` 的 MapTracker 动作有意义。起点紧贴桥边、悬崖等危险地形时设为 `true`；默认 `false` 时省略    |
| `QuickTeleport`                | 可选布尔值，默认 `false`。启用后依次点击任务地图的“前往传送”和“传送”，绕过 `EnterMap` 万能跳转；此时 `EnterMap` 可省略 |

所有坐标均使用 720p（1280×720）基准，并与录制工具所用地图体系保持一致。

## 操作流程

### 1. 同步数据并列出待适配条目

在仓库根目录运行：

```bash
pnpm fetch:zmdmap
node tools/pipeline-generate/EnvironmentMonitoring/generator/sync-routes.mjs
node .agents/skills/environment-monitoring-add-route/check_missing.mjs
```

第一条命令更新 zmdmap 缓存；第二条刷新元数据并创建 metadata-only 条目；第三条列出缺失或不完整的路线。

若用户已指定观察点，在 `mission.name`、`mission.shotTargetName` 的所有语言以及生成后的 `Id` 中匹配。匹配时忽略大小写、空格、常见中英文标点、引号和连字符：

- 唯一匹配：直接使用对应 `MissionId`；
- 多个匹配：列出 `Name` / `Id` / `MissionId` 让用户选择；
- 无匹配：展示 `check_missing.mjs` 的待适配列表，不要编造任务。

### 2. 收集真实路线数据

用户没有一次性提供全部数据时，每次只询问一个字段，按以下顺序进行：

1. 传送入口：真实 `EnterMap`，或已实测任务地图支持的 `QuickTeleport: true`
2. 路线方式：已实测可传送后直拍，或 `MapPath` / `MapTarget` / `MapGoal`
3. 直拍时直接跳到第 7 项；寻路时收集 `MapName`
4. `MapAssert`（普通传送或 `QuickTeleport + MapPath` 必填；`QuickTeleport + MapTarget/MapGoal` 跳过）
5. 所选寻路字段的坐标
6. `MapTargetTier`（仅 `MapTarget` 且确有跨 tier 目标时）
7. `CameraSwipeDirection`
8. `CameraMaxHit`（可选）
9. `Heading`（可选）
10. `Replace`（可选，有 OCR 误识别证据时）
11. `NoEnsureInitialMovementState`（可选，仅 `MapPath` / `MapGoal`）

先确认寻路方式再询问 `MapName`，因为三种模式使用的地图标识不同。接受字段后检查数组长度、数值类型和取值范围，不要等写入后才发现格式错误。

### 3. 验证传送节点

使用默认传送入口时，在 `assets/resource/pipeline/SceneManager/` 中搜索 `EnterMap`，确认节点真实存在。启用 `QuickTeleport: true` 时跳过此项，但必须已有任务详情页、任务地图和传送按钮的实测界面信息。

若不存在：

- 不写占位传送点；
- 不补其余路线字段；
- 保留同步器生成的 metadata-only 条目；
- 在交付说明中记录“等待传送点补齐后再适配”的 TODO。

生成器内部的 `SceneAnyEnterWorld` 等值只用于渲染不可达的未适配节点，不是可以提交到 `routes.json` 的路线数据。

### 4. 原位更新 `routes.json`

按 `MissionId` 找到现有条目并补充路线字段。只有在同步器未创建条目的异常情况下才新增；新增前再次确认不存在相同 `MissionId`。

写入规则：

- 严格 JSON：双引号、无注释、无尾随逗号、4 空格缩进；
- `MapPath` 的每个坐标对单独一行；
- 寻路路线中 `MapPath` / `MapTarget` / `MapGoal` 只能保留一个；切换模式时删除旧模式字段；
- 传送后直拍不增加开关字段，删除 `MapName`、`MapAssert`、三种寻路字段、`MapTargetTier` 和 `NoEnsureInitialMovementState`；按实测结果可保留 `Heading`；
- `MapTargetTier` 只能与 `MapTarget` 同时存在；
- 默认值不写：`CameraMaxHit: 2`、`NoEnsureInitialMovementState: false`、`QuickTeleport: false`；
- 不确定的可选值直接省略，不写占位值或 TODO 注释；
- 不手改 `Name` / `Id` 排序或 locale 失败提示，这些内容由同步器维护。

### 5. 生成并检查产物

优先运行统一命令：

```bash
pnpm generate:EnvironmentMonitoring
```

它会依次拉取数据、同步 `routes.json` 和五语言失败提示，并生成普通路线与终端分组文件。只有已经更新过缓存、明确需要单独调试生成器时，才在生成器目录运行：

```bash
node sync-routes.mjs
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

随后运行：

```bash
node .agents/skills/environment-monitoring-add-route/check_missing.mjs
pnpm format
pnpm check
pnpm test
```

检查：

- 目标观察点不再出现在待适配列表；有意保留 metadata-only 时除外；
- `routes.json` 中没有重复 `MissionId`；寻路路线恰好配置一个寻路字段，直拍路线没有地图与寻路字段；
- `EnterMap`、`MapName`、坐标和方向仍与用户提供的数据一致；
- 生成目录包含对应 `{Station}/{Id}.json`，终端列表已接线；
- 五语言存在对应 `.failed`，且已有人工文案未被覆盖；
- `git diff` 只包含预期的 routes、生成 Pipeline、locale 和文档变更；格式化命令产生的无关改动应排除。

## 交付说明

说明：

- 更新了哪个观察点及其 `MissionId`；
- 使用哪种寻路方式和关键可选项（如 `MapTargetTier` / `Heading` / `Replace`）；
- 哪些命令已经通过；
- 因缺少真实传送点或录制数据而保留未适配状态的 TODO。

提醒将 `routes.json`、`assets/resource/pipeline/EnvironmentMonitoring/`、自动变化的五语言 locale 与相关文档一起审查和提交。
