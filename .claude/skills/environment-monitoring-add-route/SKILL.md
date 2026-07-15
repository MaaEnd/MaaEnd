---
name: environment-monitoring-add-route
description: "向 routes.json 添加环境监测（EnvironmentMonitoring）新观察点条目。使用时：新增 zmdmap / kite_station_i18n 观察点路线配置、适配新版本的环境监测任务、补全缺失的 EnterMap、MapPath 或 MapTarget 数据。会自动检测缺失任务，逐字段询问路线数据后写入 routes.json。"
argument-hint: "可选：直接说明要适配哪个观察点名称，否则自动列出所有缺失条目"
---

# 环境监测新增路线配置

## 目的

在 `tools/pipeline-generate/EnvironmentMonitoring/routes.json` 末尾追加新的观察点条目，以便后续运行 `npx @joebao/maa-pipeline-generate` 生成 Pipeline 文件。

## 字段说明

`MissionId` / `Name` / `Id` 从 `tools/pipeline-generate/data/kite_station_i18n.json` 自动提取，**不需要询问用户提供 `MissionId`**：

- `MissionId`：直接取 `missionId`，这是 `routes.json` 的匹配主键
- `Name`：直接取 `name["zh-CN"]`
- `Id`：按生成器规则从 `name["en-US"]` 转成最终模板使用的节点 ID，仅供人工搜索生成节点/文件名

需向用户逐字段询问的路线字段：

| 字段                           | 必填 | 说明                                                                                                                                        |
| ------------------------------ | ---- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `EnterMap`                     | ✓    | 传送节点名（`SceneEnterWorldXxx`），必须已存在于 `assets/resource/pipeline/SceneManager/`；若无合适传送点，**不要**写占位值，跳过该条目即可 |
| `MapName`                      | ✓    | 地图标识。`MapPath` 填 MapTracker `map_name`（如 `map02_lv001`，支持正则）；`MapTarget` 填 MapLocate `zone_id`，必须与录制工具一致          |
| `MapAssert`                    | ✓    | 初始位置判断矩形 `[x, y, w, h]`，720p 小地图坐标；仅 `MapPath` 传送后再次复核，`MapTarget` 直接开始 NavMesh 寻路                            |
| `MapPath` / `MapTarget`        | ✓    | 二选一。`MapPath` 是手录路径 `[[x1, y1], ...]`；`MapTarget` 是 NAVMESH 目标点 `[x, y]`，适合不依赖交互/过图/机关的普通可达路线              |
| `CameraSwipeDirection`         | ✓    | `EnvironmentMonitoringSwipeScreenUp/Down/Left/Right`                                                                                        |
| `CameraMaxHit`                 | 可选 | 摄像头最大滑屏次数，默认 2；较难对准时调大                                                                                                  |
| `NoEnsureInitialMovementState` | 可选 | 仅对 `MapPath` 有意义，默认 false。路线起点紧贴桥边/悬崖边等危险地形时设为 true，跳过开局冲刺准备动作，避免掉下悬崖                         |

## 操作流程

### 第一步：确定要适配的观察点

- 若用户已指定（如"我想适配 XX 任务"），用用户给出的名称自动匹配观察点，不要反问 `MissionId`。匹配范围包括每个 mission 的 `name` 与 `shotTargetName` 下全部 locale（如 `zh-CN` / `zh-TW` / `en-US` / `ja-JP` / `ko-KR`）；匹配时忽略大小写、空格、常见中英文标点、引号和连字符差异。匹配唯一时，自动取该 mission 的 `missionId` / `name["zh-CN"]` / 最终模板 `Id` 写入 `MissionId` / `Name` / `Id`。
- 若用户给出的名称匹配到多个候选，列出候选的 `Name` / `Id` / `MissionId` 让用户选择；若没有匹配到，说明未找到并改为列出缺失条目供用户选择。
- 否则：运行 [check_missing.mjs](./check_missing.mjs) 自动检测缺失或未完整适配的条目。检测逻辑：
    - 从 `kite_station_i18n.json` 提取所有 mission 的 `missionId`（作为 MissionId）
    - 与 `routes.json` 中已有的 `MissionId` 做对比；没有条目，或条目缺少任一必填路线字段（`EnterMap` / `MapName` / `MapAssert` / `CameraSwipeDirection` / `MapPath` 或 `MapTarget` 二选一）都算待适配
    - 列出真正待适配的条目供用户选择，展示 `Name` / `Id` / `MissionId`

### 第二步：逐字段问路线数据

对每个待适配的观察点，按字段顺序使用 `vscode_askQuestions` **每次只问一个字段**，依次提问：

1. `EnterMap`
2. `MapName`
3. `MapAssert`（格式 `[x, y, w, h]`）
4. 询问寻路方式：`MapPath` 或 `MapTarget`
    - 选择 `MapPath` 时，继续询问路径（格式 `[[x1,y1],[x2,y2],...]`）
    - 选择 `MapTarget` 时，继续询问目标点（格式 `[x,y]`）
5. `CameraSwipeDirection`（选项：Up / Down / Left / Right）
6. `CameraMaxHit`（可选，跳过则不写入，使用默认值 2）
7. `NoEnsureInitialMovementState`（可选，仅 `MapPath` 路线询问；跳过则不写入，默认 false；起点紧贴危险地形时选 true）

收到每个字段的答案后，再问下一个字段。

### 第三步：验证 EnterMap

使用 `file_search` 在 `assets/resource/pipeline/SceneManager/` 中确认传送点文件是否存在。**若不存在**：

- **不要**把 `EnterMap` 替换成占位值后写入条目。直接跳过该条目（不写入 `routes.json`），让生成器走未适配分支（仅接取并追踪）。
- 在交付消息 / PR 描述中以 TODO 形式记录"等 XXX 传送点补齐后再适配 YYY 观察点"。

### 第四步：写入文件

按现有条目格式追加到 `routes.json` 末尾（数组 `]` 之前）：

- 严格 JSON 语法（双引号、不允许尾随逗号、不允许 `// TODO` 等注释）
- 4 空格缩进，`MapPath` 每个坐标对单独一行；`MapTarget` 写成 `[x, y]`
- `MapPath` 与 `MapTarget` 必须且只能写一个，不要同时写入
- `CameraMaxHit` 仅当值非默认（≠ 2）时才写入
- `NoEnsureInitialMovementState` 仅用于 `MapPath`，`MapTarget` 条目不要写入
- 数据暂缺时整个条目都不要写入（参见第三步）；TODO 留在交付消息里

### 第五步：提示后续操作

提醒用户运行以下命令重新生成 Pipeline：

```bash
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

并将 `routes.json` 与 `assets/resource/pipeline/EnvironmentMonitoring/` 下的变更一并提交。

## 注意事项

- 坐标必须基于 720p 小地图（`MapAssert`、`MapPath`、`MapTarget`）。
- `MapName` 必须和录制工具一致：`MapPath` 路线使用 MapTracker 的 `map_name`，`MapTarget` 路线使用 MapLocate / MapNavigator 的 `zone_id`。
- `MapTarget` 最终会生成 `MapNavigateAction` 的 `{ "action": "NAVMESH", "target": [...] }` 语义点；它只适合普通可达路线，不处理交互、过图、机关、战斗等特殊段。
- 若数据暂缺，**不要**填占位值后再加 TODO，直接不加该条目，把 TODO 放进 PR 描述/提交信息。
- `MissionId` 是匹配主键；`Name` 只是给维护者阅读，`Id` 是最终模板使用的节点 ID，方便搜索生成节点/文件名；重新生成时会以 zmdmap 当前数据为准自动刷新。
- 重新生成 EnvironmentMonitoring 时，生成器会为 zmdmap 中存在但 `routes.json` 缺失的任务自动追加仅含 `MissionId` / `Name` / `Id` 的未适配占位条目；skill 仍应把这类 metadata-only 条目视为待适配。
