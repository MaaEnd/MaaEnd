# 环境监测

使用 `MAA-pipeline-generate` 工具批量生成对应的 Pipeline 文件。

## 运行方式

```bash
# 在仓库根目录运行
pnpm generate:EnvironmentMonitoring

# 仅更新 zmdmap 缓存数据
pnpm fetch:zmdmap

# 如果已经更新过 zmdmap 缓存，也可以在生成器目录单独渲染
cd tools/pipeline-generate/EnvironmentMonitoring/generator
node sync-routes.mjs
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

## 新增/更新观察点

1. **更新游戏数据**：运行 `pnpm fetch:zmdmap`，数据会缓存到 `tools/pipeline-generate/data/kite_station_i18n.json`。
2. **补充路线配置**：在 `routes.json` 中新增或修改对应观察点的条目（传送点、地图名、寻路路径、摄像头朝向等）。若暂无数据，生成器会将该观察点标记为未适配，生成的 Pipeline 只会接取并追踪，不会前往拍照。
3. **重新生成 Pipeline**：运行上方两条命令，分别生成观察点节点文件与终端分组文件。
4. **提交**：将 `routes.json` 与 `assets/resource/pipeline/EnvironmentMonitoring/` 下重新生成的文件一并提交。

> `pnpm generate:EnvironmentMonitoring` 会在渲染前显式运行 `generator/sync-routes.mjs`：按 zmdmap 数据补齐/刷新 `MissionId`、`Name`、`Id`，并按 `MissionId` 排序。单独渲染时也请先运行 `node sync-routes.mjs`。

### `routes.json` 条目字段说明

```jsonc
{
    "MissionId": "m1m30",
        // 用于匹配 kite_station_i18n.json 中对应 mission 的 missionId，是 routes.json 的主键。
    "Name": "我的观察点",
        // 中文名，仅供人工阅读和搜索；不作为主键。
    "Id": "MyObservationPoint",
        // 最终模板使用的节点 ID，用于搜索生成节点/文件名；不作为主键。
    "EnterMap": "SceneEnterWorldWulingXxx",
        // 传送节点名，必须已在 assets/resource/pipeline/SceneManager/ 中存在。
        // 暂无合适传送点时，直接不要加这个 routes.json 条目（生成器会按未适配处理，仅接取并追踪），
        // 不要写 "SceneAnyEnterWorld" 等占位值。
    "MapName": "map02_lv001",
        // 地图标识：使用 MapPath 时填 MapTracker 的 map_name（支持正则）；
        // 使用 MapGoal 时填可加载 NavMesh 的精确 MapTracker map_name；
        // 使用 MapTarget 时填 MapLocate 的 zone_id。必须与录制工具保持一致。
    "MapAssert": [x, y, w, h],
        // 目标矩形；MapPath / MapGoal 使用 MapTrackerAssertLocation 判断，MapTarget 使用 MapLocateAssertLocation 判断。
    "MapPath": [[x1, y1], [x2, y2]],
        // 寻路路径（小地图坐标序列），由 MapTrackerMove 逐点跟随。
        // 与 MapTarget / MapGoal 三选一，用 tools/MapNavigator/ 的 GUI 工具录制。
    // "MapTarget": [x, y],
    //     MapNavigateAction 的 NAVMESH 目标点。默认按 base 坐标解释：
    //     [{ "action": "NAVMESH", "target": [x, y] }]
    //     与 MapPath / MapGoal 三选一，适合不依赖交互、过图、机关的普通可达路线。
    // "MapTargetTier": "ValleyIV_L1_171",
    //     可选，仅用于 MapTarget 路线。目标点与起点不在同一 tier，且 MapTarget 是在
    //     tier 底图上直接点出的坐标时填写；生成时会透传为 NAVMESH 的 target_tier。
    // "MapGoal": [x, y],
    //     MapTrackerGoal 目标点。生成时会自动使用 MapTrackerGoal：
    //     { "map_name": "map02_lv001", "target": [x, y] }
    //     与 MapPath / MapTarget 三选一，适合已有 NavMesh 的 MapTracker 路线。
    "CameraSwipeDirection": "EnvironmentMonitoringSwipeScreenUp",
        // 摄像头朝向调整方向，四选一：Up / Down / Left / Right。
    "CameraMaxHit": 2,
        // 可选；调整摄像头时的最大滑屏命中次数，默认值为 2。
        // 拍照目标较难对准时可适当调大。
    "Replace": [
        [
            "売",
            "壳"
        ]
    ],
        // 可选；任务列表和任务详情页 OCR 易混字符替换。
    "NoEnsureInitialMovementState": true,
        // 可选；默认 false。一般只在路线起点紧贴桥边、悬崖边等危险地形时开启，
        // 用于跳过 MapTrackerMove 开局的冲刺准备动作，避免角色因为这一步直接掉下桥或掉下悬崖。
    "Heading": 90
        // 可选；到达拍照点后、进入拍照模式前，先用 MapNavigator 的 HEADING 动作把
        // 角色朝向旋转到该角度（度数，与 MapNavigator 角度约定一致）。未配置时不调整。
        // 仅影响角色朝向（决定进入拍照模式时的初始视角），与摄像头滑屏（CameraSwipeDirection）相互独立。
}
```

> `routes.json` 是严格 JSON：不允许行内注释、不允许尾随逗号。上面的注释只是文档示意，实际文件里要去掉。`MapPath` / `MapTarget` / `MapGoal` 必须且只能填写其中一个。

> 重新生成 EnvironmentMonitoring 时，生成器会自动同步 `MissionId` / `Name` / `Id` 并按 `MissionId` 排序。手动新增条目时必须填写 `MissionId`；如果 zmdmap 中存在新任务但 `routes.json` 没有对应条目，生成器会自动追加仅含 `MissionId` / `Name` / `Id` 的未适配占位条目，方便维护者看到待补路线。

> 编辑 `routes.json` 时 VS Code 会自动应用 `tools/schema/environment_monitoring_routes.schema.json`（通过 `.vscode/settings.json` 注册），提供字段补全、枚举值（`CameraSwipeDirection`）和必填项校验。

> 完整维护流程见 `docs/zh_cn/developers/tasks/environment-monitoring-maintain.md`。

## 致谢

- 感谢 `zmdmap` 提供的数据
