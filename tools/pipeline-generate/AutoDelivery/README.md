# AutoDelivery 路线生成器

`tools/pipeline-generate/data/delivery_destinations.json` 是 zmdmap 数据 CI 生成并由 `fetch:zmdmap` 下载的仓储/终点目录；本目录的 `routes.json` 参考 EnvironmentMonitoring 的 metadata-only 维护方式，自动同步全部仓储和终点的检索元数据，并仅在需要覆盖自动 `NAVMESH` 目标时保存实测路线。`tools/schema/auto_delivery_routes.schema.json` 为其提供 IDE 校验。两者共同生成：

- `assets/resource/pipeline/AutoDelivery/Routes.json`：每条路线一个可直接试跑的 `AutoDeliveryRoute...` 节点，并为主路线生成允许滑索的变体；终点路线的 `desc` 会注明起点仓储节点；
- `assets/data/AutoDelivery/catalog.json`：Go Service 运行时 OCR 匹配目录，只包含文本、归属关系与对应的生成节点名，不再包含坐标或路径。

运行：

```powershell
pnpm generate:AutoDelivery
```

完整送货业务调用方仍只进入 `AutoDelivery`。生成的 `AutoDeliveryRoute...` 节点是公开的单路线测试入口，可在节点测试工具中单独运行；正常流程由 Go Service 识别当前仓储/终点后，通过固定 `SubTask` 分发节点动态调用。

运行生成命令时，`sync-routes.mjs` 会按 `source_id` 刷新仓储的 `name` 以及终点的 `name` / `depot_id`，为游戏数据中的新增项补充 metadata-only 条目，并保留已有的人工字段。仓储和终点都支持 `description` / `path` / `retry_path`；`departure_path` 仅用于仓储，并会拼接到该仓储所有终点主路线之前。

修改 `routes.json` 时只使用 MapNavigator 工具实测得到的路径。普通可达目标仅保留同步出的元数据，由生成器使用游戏数据坐标生成单个 `NAVMESH` 点；跨层、断网格、交互或需要站位修正的路线才填写对应的路径覆盖。不要为了补齐字段而复制默认坐标。
