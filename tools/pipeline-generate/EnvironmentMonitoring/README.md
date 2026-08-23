# 环境监测

使用 `MAA-pipeline-generate` 工具批量生成对应的 Pipeline 文件。

`generator/model.mjs` 统一读取 zmdmap 精简游戏数据与 `routes.json`，并规范化为观察点任务模型；`data.mjs` 与
`terminals-data.mjs` 再分别投影为观察点路线模板和终端分组模板所需的最小数据。

## 运行方式

```bash
# 在仓库根目录运行
pnpm generate:EnvironmentMonitoring

# 仅同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap

# 如果已经更新过环境监测数据，也可以在生成器目录单独渲染
cd tools/pipeline-generate/EnvironmentMonitoring/generator
node sync-routes.mjs
pnpm exec maa-pipeline-generate
pnpm exec maa-pipeline-generate --config terminals-config.json
```

## 新增/更新观察点

1. **更新游戏数据**：运行 `pnpm fetch:zmdmap`，下载 zmdmap 数据 CI 从 TableCfg 裁剪并发布的精简游戏数据 `tools/pipeline-generate/data/environment_monitoring.json`。
2. **补充路线配置**：在 `routes.json` 中新增或修改对应观察点的条目（传送点、地图名、寻路路径、摄像头朝向等）。若暂无数据，生成器会将该观察点标记为未适配，生成的 Pipeline 只会接取并追踪，不会前往拍照。
3. **重新生成 Pipeline**：运行上方两条命令，分别生成观察点节点文件与终端分组文件。
4. **提交**：将 `routes.json` 与 `assets/resource/pipeline/EnvironmentMonitoring/` 下重新生成的文件一并提交。

> `pnpm generate:EnvironmentMonitoring` 会先同步 zmdmap 精简游戏数据，再运行 `generator/sync-routes.mjs`：补齐/刷新 `MissionId`、`Name`、`Id`，按 `MissionId` 排序，并同步五语言路线失败提示。单独渲染时也请先运行 `node sync-routes.mjs`。

### `routes.json` 条目字段说明

```jsonc
{
    "MissionId": "m1m30",
        // 用于匹配 environment_monitoring.json 中对应 mission 的 mission_id，是 routes.json 的主键。
    "Name": "我的观察点",
        // 中文名，仅供人工阅读和搜索；不作为主键。
    "Id": "MyObservationPoint",
        // 最终模板使用的节点 ID，用于搜索生成节点/文件名；不作为主键。
    "EnterMap": "SceneEnterWorldWulingXxx",
        // 传送入口 Pipeline 节点名，不限制名称前缀，必须已在 assets/resource/pipeline/ 中存在，
        // 并能作为 SubTask 完整执行后正常返回。
        // 启用 QuickTeleport 时不会使用该节点，可以省略。
        // 暂无合适传送点且未启用 QuickTeleport 时，不要填写占位值（生成器会按未适配处理，仅接取并追踪），
        // 不要写 "SceneAnyEnterWorld" 等占位值。
    "QuickTeleport": true,
        // 可选，默认 false。启用后通过追踪任务打开的地图依次点击“前往传送”和“传送”，
        // 不再调用 EnterMap 配置的 Pipeline 入口节点。
    "NavZoneId": "Wuling_Base",
    "NavAssert": [x, y, w, h],
    "NavPath": [{ "action": "ZONE", "zone_id": "..." }, [x1, y1], [x2, y2]],
        // 寻路路线的写法。寻路时 NavPath 必填，用 tools/MapNavigator/ 的 GUI 工具录制。
        // NavZoneId + NavAssert 渲染为 MapLocateAssertLocation（NavZoneId 分区坐标，
        // tier 分区为 tier 局部坐标）；NavPath 原样作为 MapNavigateAction 的 path
        // （不含 HEADING，朝向仍由 Heading 字段追加）。
        // 普通传送必须有 NavZoneId / NavAssert 用来判断是否调用 EnterMap；
        // 快捷传送不执行起点断言，可同时省略二者。
        // 如果传送点可以直接拍照，则整组字段一起省略。
    "Replace": [
        [
            "売",
            "壳"
        ]
    ],
        // 可选；任务列表和任务详情页 OCR 易混字符替换。
    "Heading": 90,
        // 可选；到达拍照点后、进入拍照模式前，先用 MapNavigator 的 HEADING 动作把
        // 角色朝向旋转到该角度（度数，与 MapNavigator 角度约定一致）。未配置时不调整。
        // 直拍路线也支持：传送后只原地调整朝向，再进入拍照流程。
}
```

> `routes.json` 是严格 JSON：不允许行内注释、不允许尾随逗号。上面的注释只是文档示意，实际文件里要去掉。需要寻路时配置 `NavPath`，普通传送再配置 `NavZoneId` / `NavAssert`。如果传送点可以直接拍照，则整组地图断言和寻路字段都不要填，但可以按实测结果保留可选的 `Heading`。生成器会根据“存在真实传送入口，同时没有地图断言和寻路配置”自动进入直拍分支，不需要额外开关字段。

> 传送后的处理取决于入口和路线类型：传送后直拍不做位置断言或寻路，配置 `Heading` 时只原地调整朝向，随后进入任务专属拍照包装节点。`QuickTeleport` 的固定传送落点可直接开始寻路，因此允许省略 `NavZoneId` / `NavAssert`。普通传送的寻路路线仍会在决定是否调用 `EnterMap` 前用到断言配置，所以不能省略；传送完成后 `NavPath` 直接开始寻路，不再复核起点。
>
> 仅含 `NAVMESH` 的 `NavPath` 不需要前置 `ZONE`：导航器会从 MapLocator 当前定位自动确定起点区域。`ZONE` 只为后续手录坐标点声明和校验分区，多分区或过图路径应原样保留录制工具导出的 `ZONE`。

> 传送入口由 `QuickTeleport` 决定：默认通过 `EnterMap` 调用配置的 Pipeline 节点，不限制节点名称；该节点需能作为 SubTask 完整执行后正常返回。启用快捷传送后，“开始追踪”会直接等待任务地图，“已追踪”会先点击“停止追踪”旁的定位图标打开任务地图，随后依次点击“前往传送”和“传送”，此时 `EnterMap` 可省略。

> 重新生成 EnvironmentMonitoring 时，生成器会自动同步 `MissionId` / `Name` / `Id` 并按 `MissionId` 排序。手动新增条目时必须填写 `MissionId`；如果环境监测数据中存在新任务但 `routes.json` 没有对应条目，生成器会自动追加仅含 `MissionId` / `Name` / `Id` 的未适配占位条目，方便维护者看到待补路线。

> 编辑 `routes.json` 时 VS Code 会自动应用 `tools/schema/environment_monitoring_routes.schema.json`（通过 `.vscode/settings.json` 注册），提供字段补全和必填项校验。

### `routes.json` 写法参考

所有完整适配条目都需要元数据，以及 `EnterMap` / `QuickTeleport: true` 中的一种传送入口。其余字段按路线类型填写：

| 类型 | 地图与路线字段 | 断言矩形 | 生成流程 |
| ----------------------------- | --------------------------------------------------------------- | --------------------------- | -------------------------------------------- |
| metadata-only | 仅 `MissionId` / `Name` / `Id` | 不填 | 仅接取并追踪，不传送或拍照 |
| 传送后直拍 | 不填任何地图和寻路字段；`Heading` 可选 | 不填 | 传送 →（可选原地调整朝向）→ 拍照 |
| 寻路 | `NavPath`；普通传送再加 `NavZoneId`，可选 `Heading` | `NavAssert`，快捷传送可省略 | `MapNavigateAction` 按 `NavPath` 寻路 → 拍照 |

`Replace` 可用于所有已适配路线，不改变路线类型。直拍必须经过游戏实测确认，不能用来代替尚未录制的路线数据。寻路路线一律用 `NavPath`，普通传送再补 `NavZoneId` / `NavAssert`。拍照目标未命中时由公共 `CameraScanAction` 自动执行九宫格与 fallback 镜头扫描，不需要路线级镜头方向配置。

> 完整维护流程见 `docs/zh_cn/developers/tasks/environment-monitoring-maintain.md`。
