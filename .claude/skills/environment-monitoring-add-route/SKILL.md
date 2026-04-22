---
name: environment-monitoring-add-route
description: "向 routes.mjs 添加环境监测（EnvironmentMonitoring）新观察点条目。使用时：新增 kite_station 观察点路线配置、适配新版本的环境监测任务、补全缺失的 MapPath / EnterMap 数据。会自动检测缺失任务，逐字段询问路线数据后写入 ROUTE_CONFIG。"
argument-hint: "可选：直接说明要适配哪个观察点名称，否则自动列出所有缺失条目"
---

# 环境监测新增路线配置

## 目的

在 `tools/pipeline-generate/EnvironmentMonitoring/routes.mjs` 的 `ROUTE_CONFIG` 数组末尾追加新的观察点条目，以便后续运行 `npx @joebao/maa-pipeline-generate` 生成 Pipeline 文件。

## 字段说明

`Name` 从 `kite_station.json` 自动提取，**不需要询问用户**：

- `Name`：直接取 `name["zh-CN"]`

需向用户逐字段询问的路线字段：

| 字段                   | 必填 | 说明                                                                                                                                            |
| ---------------------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `EnterMap`             | ✓    | 传送节点名（`SceneEnterWorldXxx`），必须已存在于 `assets/resource/pipeline/SceneManager/`；若无合适传送点填 `SceneAnyEnterWorld` 并加 `// TODO` |
| `MapName`              | ✓    | MapTracker 小地图标识（如 `map02_lv001`），支持正则                                                                                             |
| `MapTarget`            | ✓    | 目标矩形 `[x, y, w, h]`，720p 小地图坐标                                                                                                        |
| `MapPath`              | ✓    | 寻路路径 `[[x1, y1], ...]`，用 `tools/MapNavigator/` 录制                                                                                       |
| `CameraSwipeDirection` | ✓    | `EnvironmentMonitoringSwipeScreenUp/Down/Left/Right`                                                                                            |
| `CameraMaxHit`         | 可选 | 摄像头最大滑屏次数，默认 2；较难对准时调大                                                                                                      |

## 操作流程

### 第一步：确定要适配的观察点

- 若用户已指定（如"我想适配 XX 任务"），直接跳到第二步。
- 否则：运行 [check_missing.mjs](./check_missing.mjs) 自动检测缺失条目。检测逻辑：
    - 从 `kite_station.json` 提取所有 mission 的 `name["zh-CN"]`（作为 Name）
    - 与 `routes.mjs` 中 `ROUTE_CONFIG` 已有的 `Name` 做对比（去符号小写）
    - 列出真正缺失的条目供用户选择

### 第二步：逐字段问路线数据

对每个待适配的观察点，按字段顺序使用 `vscode_askQuestions` **每次只问一个字段**，依次提问：

1. `EnterMap`
2. `MapName`
3. `MapTarget`（格式 `[x, y, w, h]`）
4. `MapPath`（格式 `[[x1,y1],[x2,y2],...]`）
5. `CameraSwipeDirection`（选项：Up / Down / Left / Right）
6. `CameraMaxHit`（可选，跳过则不写入，使用默认值 2）

收到每个字段的答案后，再问下一个字段。

### 第三步：验证 EnterMap

使用 `file_search` 在 `assets/resource/pipeline/SceneManager/` 中确认传送点文件是否存在。若不存在，自动将值替换为 `SceneAnyEnterWorld` 并在条目上方加 `// TODO: 缺少 XXX 传送点` 注释，并提示用户。

### 第四步：写入文件

按现有条目格式拼写 JS 对象，追加到 `ROUTE_CONFIG` 数组末尾（`];` 之前）：

- 保持 4 空格缩进
- `MapPath` 每个坐标对单独一行
- `CameraMaxHit` 仅当值非默认（≠ 2）时才写入

### 第五步：提示后续操作

提醒用户运行以下命令重新生成 Pipeline：

```bash
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

并将 `routes.mjs` 与 `assets/resource/pipeline/EnvironmentMonitoring/` 下的变更一并提交。

## 注意事项

- 坐标必须基于 720p 小地图（`MapTarget`、`MapPath`）。
- 若数据暂缺，可用 `ROUTE_DEFAULTS` 兜底，但生成的 Pipeline **无法真正运行**，需附 `// TODO` 标记。
- `Name` 匹配是去符号小写比较，中文引号、空格等均会被忽略。
