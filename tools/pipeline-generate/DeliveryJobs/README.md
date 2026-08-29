# 转交委托

使用 `MAA-pipeline-generate` 从地区、仓储节点与可装箱物品模型生成转交委托的 Pipeline 和任务配置。

`tools/pipeline-generate/data/delivery_jobs.json` 是 zmdmap 数据 CI 从 TableCfg 裁剪并发布的精简游戏数据，MaaEnd 通过
`fetch-data.mjs` 下载。文件包含全部仓储节点及其可装箱物品；统一的数据流与来源边界见[生成数据总览](../README.md)。

## 运行方式

在仓库根目录运行：

```bash
pnpm generate:DeliveryJobs

# 仅同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap
```

生成内容：

- `assets/resource/pipeline/DeliveryJobs.json`：通用任务入口与地区调度；
- `assets/resource/pipeline/DeliveryJobs/Region/*.json`：各地区入口、循环与界面判定；
- `assets/resource/pipeline/DeliveryJobs/Depot/**/*.json`：每个仓储节点的任务、货物识别和进入节点；
- `assets/resource/pipeline/DeliveryJobs/PriorityItems.json`：各地区四级装箱货物选择与回退入口；
- `assets/tasks/DeliveryJobs.json`：地区、仓储节点处理方式和装箱物品选项。

生成前会先运行 `sync-locales.mjs`：地区、仓储节点及物品的五语言名称分别来自
`delivery_jobs.json`，物品是否生成则由 `recognition_items.json` 决定。同步器只补齐缺失或空白的
`global.region.*` / `iconRecognition.name.*`，已有的人工消歧文案会保留。

每个仓储节点默认提供五种处理方式；支持全自动送货的仓储节点额外提供一种：

- **接取并转交**：装箱、接取任务，并在返回仓储节点后转交；
- **全自动送货**：装箱、接取任务，随后自动取货、寻路并提交货物；
- **按报价处理**：识别当前选中的报价，并分别按“达到或高于阈值时”和“低于阈值时”的配置执行；
- **仅接取委托**：完成装箱并接取任务，不转交，返回仓储节点后继续处理其他委托；
- **仅装箱货物**：完成装箱后从调度申请界面返回，不接取任务；
- **不处理**：跳过该仓储节点已有的送货任务和装箱入口。

按报价处理默认以整数 `119000`（即 11.9 万）为阈值。“达到或高于阈值时”默认接取并转交，“低于阈值时”默认仅接取委托；两侧均可改为“接取并转交”“全自动送货”“仅接取委托”或“不处理”。“仅接取委托”会在接取后返回仓储节点继续遍历，“不处理”会关闭报价页并继续遍历。报价 OCR 失败时会停在报价页并提示用户，不会自动接取。

全自动送货只使用 [AutoDelivery](../../../docs/zh_cn/developers/components/auto-delivery.md) 的唯一公共入口 `AutoDelivery`，支持四号谷地和武陵的全部仓储节点。DeliveryJobs 负责打开对应本地仓储节点的“查看任务”入口，并为提交完成配置 anchor；识别、传送、寻路、取货与提交均由 AutoDelivery 完成，不把完整链路装入一个 `SubTask`。

确认接取任务后会直接利用首次出现的“查看任务”详情判断下一步，不会先回到大世界再重新打开详情。只有检测到此前已有送货任务、当前不在详情页时，才会回到对应仓储节点打开详情恢复流程；单击“查看任务”后会先识别区域和追踪按钮，确认任务详情已经加载，再开始识别仓储或终点。取货后则通过 `SceneEnterMenuMission` 直接进入任务界面，在左侧任务列表中识别并点击“送货任务”；当前页未找到时最多向下滑动三次，确认右侧详情标题已经切换后再识别送货目的地，不再返回地区建设的运送委托列表。

每次打开任务详情时，DeliveryJobs 都把 `AutoDelivery` 放入 `next`，当前取货或送货阶段由组件根据任务详情自行判断。已进入送货阶段时，组件取消任务追踪并从大世界前往终点；需要取货时，组件快速传送到仓储附近，通过 `SubTask` 进入任务界面并确认选中“送货任务”，取消追踪后前往仓储取货，再以同样方式重新打开送货任务详情并继续终点流程。所有追踪状态节点都会同时确认任务界面和送货任务详情，避免在其他界面误判通用按钮。

仓储坐标与全部送货终点均由 `delivery_destinations_data.py` 从游戏数据和 BaseNav 变换生成，普通点只需一个 `NAVMESH` 目标；断网格、分层或需要重新靠近取货点的路线才在 `tools/pipeline-generate/AutoDelivery/routes.json` 中保留覆盖。AutoDelivery 生成器将每条路线渲染为可独立试跑的 Pipeline 节点，并生成不含坐标的运行时匹配目录；Go Service 识别目标后动态选择对应节点。终点覆盖的 `path` 是包含最终航点的完整路线；仓储 `departure_path` 作为所有归属终点的公共离开路线前缀。`retry_path` 的用途、执行边界与维护要求见 AutoDelivery 文档，DeliveryJobs 不直接调用该内部路线。

风险确认与滑索偏好仅对 `Win32-Front` / `Linux` 控制器开放；受项目接口能力限制，其他控制器仍可能显示“全自动送货”处理方式，但无法关闭默认安全守卫，选择后会直接停止任务。滑索偏好只允许 MapNavigator 在预计更快且满足供电、上下索条件时规划滑索，不保证每条路线都会使用。送货成功后返回 DeliveryJobs 仓储节点循环，送货失败则停止整个任务，避免继续装箱。该功能仍处于测试阶段，使用前必须确认风险提示。

阈值输入虽然只允许数字，但 `pipeline_type` 必须保持为 `string`，因为它会被插入完整的 `ExpressionRecognition.expression` 字符串；设为 `int` 会尝试把整个表达式转换为整数，最终得到 `null`。

“仅装箱货物”遇到已有待运送货物时会关闭当前页面并继续遍历下一个仓储节点；“全自动送货”会恢复已有任务；其他需要接取或转交的模式仍会停止任务并提示先完成送货。

启用“填入指定货物”后，先按地区展开配置，再为每个地区设置优先级 1 至 4。优先级 1 默认使用砂叶粉末，
优先级 2 至 4 默认“不指定”。流程会从列表顶部完整查找当前货物，将可用数量填到最大；货箱未满时再尝试下一个
已配置货物。所有已配置货物均无法装满时，任务会停在装箱界面并明确报错。该功能使用全新的优先级配置，旧版单货物配置不会迁移。

旧版的全局“仅接取任务”和“仅装箱货物”开关已由逐仓储节点选项取代，升级后的已有配置需要重新选择各节点的处理方式。

## 新增地区或仓储节点

1. 不需要修改 `model.mjs`：地区、仓储节点完全由 `delivery_jobs.json` 驱动；MaaEnd 标识由
   数据源英文名移除空格和标点后生成，场景节点名按
   `SceneEnterMenuRegionalDevelopment{Id}[DepotNode]` 约定生成。
   AutoDelivery 数据 CI 必须同时生成对应游戏仓储 ID 的坐标与终点，否则配置动作会安全失败。
2. 新地区上线时，在 `assets/resource/pipeline/Interface/SceneRegionalDevelopment.json` 中补充对应的场景节点；
   `global.region.*` 与 `iconRecognition.name.*` 缺失翻译由 `sync-locales.mjs` 根据数据源自动补齐。
3. 装箱物品选项无需手工登记：精简数据依据 `FactoryItemTable.deliverItemTypeList` 与
   `transferDomainIds` 判断物品可运入的地区，生成器再取地区各仓储节点 `fillable_items` 的交集，过滤出
   `assets/data/IconRecognition/recognition_items.json` 已收录的物品，由 IconRecognition（`grid_type=shipment`）识别；
   物品显示名称复用 `iconRecognition.name.*` 多语言 key，配置值使用稳定 item ID；每个优先级槽位使用同一份地区物品列表。
4. 运行 `pnpm generate:DeliveryJobs`，再运行 `node --test tools/pipeline-generate/DeliveryJobs/*.test.mjs`、
   `pnpm check` 和 `pnpm test`。

生成的 Pipeline 和 Task 文件不应手工修改；流程级公共节点仍在 `PackCargo.json` 和 `TransferJob.json` 中维护。
