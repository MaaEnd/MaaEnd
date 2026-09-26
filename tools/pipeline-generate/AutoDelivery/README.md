# AutoDelivery 路线生成器

`tools/pipeline-generate/data/delivery_destinations.json` 是 zmdmap 数据 CI 生成并由 `fetch:zmdmap` 下载的仓储/终点目录；本目录的 `routes.json` 参考 EnvironmentMonitoring 的 metadata-only 维护方式，自动同步全部仓储和终点的检索元数据，并仅在需要覆盖自动 `NAVMESH` 目标时保存实测路线。`tools/schema/auto_delivery_routes.schema.json` 为其提供 IDE 校验。两者共同生成：

- `assets/resource/pipeline/AutoDelivery/Routes/{RouteFileId}.json`：按仓储节点分组保存可直接试跑的 `AutoDeliveryRoute...` 节点，并为主路线生成允许滑索的变体；文件名取自仓储英文名（如 `OriginLodespring.json`），终点路线的 `desc` 会注明起点仓储节点；
- `assets/resource/pipeline/AutoDelivery/RecycleBinAreas.json`：按区域生成资源回收站任务的二次判定入口；`AutoDeliveryRecognizeDestination` 发现同一地图的同一区域存在多个回收站后开始追踪或打开任务地图，并依次检查该区域的全部候选节点；
- `assets/resource/pipeline/AutoDelivery/RecycleBinCandidates.json`：为每个资源回收站单独生成 `MapFind` 候选节点，使用数据源中的 `u/v` 坐标检查 `RecycleBin` 图标，并把命中的精确终点交给既有路线分发节点；
- `assets/data/AutoDelivery/catalog.json`：Go Service 运行时 OCR 匹配目录，只包含文本、归属关系与对应的生成节点名，不再包含坐标或路径。

运行：

```powershell
pnpm generate:AutoDelivery
```

完整送货业务调用方仍只进入 `AutoDelivery`。生成的 `AutoDeliveryRoute...` 节点是公开的单路线测试入口，可在节点测试工具中单独运行；正常流程由 Go Service 识别当前仓储/终点后，通过固定 `SubTask` 分发节点动态调用。

运行生成命令时，`sync-routes.mjs` 会按 `source_id` 刷新仓储的 `name` 以及终点的 `name` / `depot_id`，为游戏数据中的新增项补充 metadata-only 条目，并保留已有的人工字段。仓储和终点都支持 `description` / `path` / `retry_path` / `yaw` / `offset` / `walk_only` / `zipline_only`；`departure_path` 仅用于仓储，并会拼接到该仓储所有终点主路线之前。所有仓储和送货目标默认还会用自动两点接近路线生成 retry 节点，显式 `retry_path` 可覆盖其站位修正路径。

部分收货 NPC 实际朝向墙体，或数据源缺失朝向（`yaw` 缺省为 0），此时自动接近点会落进不可达处，retry 站位修正必然失败。这类情况只需在对应条目上写 `yaw` 覆盖生成接近点使用的朝向，不必整条重写 `retry_path`。取值与数据源 `yaw` 同义：接近点在实体该朝向的正方向 8 米处，0 度指向底图上方，数值增大顺时针旋转（90 度向右、180 度向下、270 度向左）。仓储的 `yaw` 同时作用于主路线接近点与自动重试路线；终点的 `yaw` 作用于回收站主路线接近点与自动重试路线，NPC 主路线仍只生成原始坐标的单个 `NAVMESH` 点，因此只影响其 retry 路线。条目同时覆盖 `path` 与 `retry_path` 时 `yaw` 没有作用对象，生成器会直接报错。

数据源的 `u/v` 由游戏世界坐标投影得来，落点可能与实际可交互位置差几米（落在另一张可走面上、被高架步道挡住、交互区域在相邻几米处）。这类偏差用条目上的 `offset`（`[du, dv]`，底图像素，右为正 u、下为正 v）微调落点，主路线终点、自动接近点与自动重试路线会一起平移，不必整条重写路线；偏移后的落点必须仍在底图范围内，否则生成器直接报错。`offset` 只改生成的导航坐标，回收站大地图图标判定用的坐标仍取自数据源。它同样在 `path` 与 `retry_path` 都被覆盖时报错。

需要完整保留录制路径、禁止全局滑索规划跳过作者路点时，在对应仓储或终点条目上设置 `"walk_only": true`。生成器仍会保留普通节点和 `WithZipline` 节点名，但两个节点都会使用 `"zip": false`；因此用户全局启用滑索时，该条路线仍严格按作者路径步行执行。`required: true` 只用于标记启用滑索规划时的必经点，不等价于整条路线仅步行。反过来，某条路线只有坐滑索才到得了（如终点裴令容）时设置 `"zipline_only": true`：两个节点都会使用 `"zip": true`，不会留下一条已知走不通的步行路线；运行时若用户选择步行送货（「送货时优先使用滑索」为关），Go 侧会输出提示说明原因并让动作直接失败，而不是退化成步行。`walk_only` 与 `zipline_only` 互斥，同时声明时生成器直接报错。

目标周围还有别的角色、只有图标模板匹配时容易点到别人时，在对应终点条目上设置 `"name_check": true`。世界交互提示会把目标名称渲染在交货图标旁的提示区域里，开启后到达判定除图标模板外还要求提示文本命中该终点的名称；名称按运行时目录里的五语言文本匹配，因此不受游戏语言影响。只有 NPC 终点的提示会显示角色名，其他 `kind` 开启时生成器直接报错。

所有生成路线的首点都会由生成器统一插入 `ZONE` 区域声明（`ValleyIV_Base` / `Wuling_Base`，取自仓储所在 map 的 BaseNav 地区），把仓储/终点所在区域作为定位器起步时的期望区域，避免冷启动定位落到别的区域图上导致目标点不可达。因此 `path` / `retry_path` / `departure_path` **不需要也不应该**再书写首点 `ZONE`：写了同区域声明会被丢弃后重新生成，声明了其他区域则直接报错。跨区路线在路径中段保留的 `ZONE` 不受影响。

修改 `routes.json` 时只使用 MapNavigator 工具实测得到的路径。普通可达目标仅保留同步出的元数据：仓储节点和资源回收站会在终点的 yaw 正方向 8 米处生成一个 `required: true` 的 `NAVMESH` 必经点，再前往原始坐标，保证从交互正面接近；普通收货 NPC 仍只生成原始坐标的单个 `NAVMESH` 点。这里的 yaw 取自数据源，朝向不合适时用条目上的 `yaw` 覆盖，而不是复制整条接近路线。自动生成的 `NAVMESH` 点一律带 `target_deck_y`，取值是数据源中该实体的世界高度，接近点与终点同层、共用这个高度，用于在上下重叠的可走面里选中目标层。跨层、断网格、交互或需要站位修正的路线才填写对应的路径覆盖，人工 `path` 不会自动插入接近点，层声明按实测结果原样保留。不要为了补齐字段而复制默认坐标。
