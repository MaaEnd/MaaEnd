# 环境监测

使用 `MAA-pipeline-generate` 工具批量生成对应的 Pipeline 文件。

## 运行方式

```bash
npx @joebao/maa-pipeline-generate
npx @joebao/maa-pipeline-generate --config terminals-config.json
```

## 新增/更新观察点

1. **更新游戏数据**：将最新的 `kite_station.json` 替换到本目录（数据来源：`zmdmap`）。
2. **补充路线配置**：在 `routes.mjs` 的 `ROUTE_CONFIG` 中新增或修改对应观察点的条目（传送点、地图名、寻路路径、摄像头朝向等）。若暂无数据，`ROUTE_DEFAULTS` 会自动兜底，但生成的 Pipeline **无法真正运行**。
3. **重新生成 Pipeline**：运行上方两条命令，分别生成观察点节点文件与终端分组文件。
4. **提交**：将 `routes.mjs` 与 `assets/resource/pipeline/EnvironmentMonitoring/` 下重新生成的文件一并提交。

### `ROUTE_CONFIG` 条目字段说明

```javascript
{
    Id: "MyObservationPoint",
        // PascalCase 英文名，作为节点前缀与输出文件名（${Station}/${Id}.json）。
        // 默认从 kite_station.json 的 name["en-US"] 转换；有歧义或官方英文名变动时显式指定。
    Name: "我的观察点",
        // 用于匹配 kite_station.json 中对应 mission 的 name["zh-CN"]（去符号小写对比）。
        // 匹配失败时 data.mjs 会 console.warn 并以 ROUTE_DEFAULTS 兜底。
    EnterMap: "SceneEnterWorldWulingXxx",
        // 传送节点名，必须已在 assets/resource/pipeline/SceneManager/ 中存在。
        // 无合适传送点时填 "SceneAnyEnterWorld"（直接回大世界），并附 // TODO: 注释。
    MapName: "map02_lv001",
        // MapTracker 小地图标识，支持正则（如 "^map\\d+_lv\\d+$"）。
    MapTarget: [x, y, w, h],
        // 目标矩形（小地图坐标），用于 MapTrackerAssertLocation 判断是否已就位。
    MapPath: [[x1, y1], [x2, y2], ...],
        // 寻路路径（小地图坐标序列），由 MapTrackerMove 逐点跟随。
        // 用 tools/MapNavigator/ 的 GUI 工具录制。
    CameraSwipeDirection: "EnvironmentMonitoringSwipeScreenUp",
        // 摄像头朝向调整方向，四选一：Up / Down / Left / Right。
    CameraMaxHit: 2,
        // 可选；调整摄像头时的最大滑屏命中次数，默认值见 ROUTE_DEFAULTS。
        // 拍照目标较难对准时可适当调大。
}
```

> 完整维护流程见 `docs/zh_cn/developers/tasks/environment-monitoring-maintain.md`。

## 致谢

- 感谢 `zmdmap` 提供的数据
