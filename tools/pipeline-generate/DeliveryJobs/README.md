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

每个仓储节点提供五种处理方式：

- **接取并转交**：装箱、接取任务，并在返回仓储节点后转交；
- **按报价处理**：识别当前选中的报价，并分别按“达到或高于阈值时”和“低于阈值时”的配置执行；
- **仅接取委托**：完成装箱并接取任务，不转交，返回仓储节点后继续处理其他委托；
- **仅装箱货物**：完成装箱后从调度申请界面返回，不接取任务；
- **不处理**：跳过该仓储节点已有的送货任务和装箱入口。

按报价处理默认以整数 `119000`（即 11.9 万）为阈值。“达到或高于阈值时”默认接取并转交，“低于阈值时”默认仅接取委托；两侧均可改为“接取并转交”“仅接取委托”或“不处理”。“仅接取委托”会在接取后返回仓储节点继续遍历，“不处理”会关闭报价页并继续遍历。报价 OCR 失败时会停在报价页并提示用户，不会自动接取。以后接入自动送货时，可在共用的报价动作选项中增加对应动作，让两个报价分支按需选择。

阈值输入虽然只允许数字，但 `pipeline_type` 必须保持为 `string`，因为它会被插入完整的 `ExpressionRecognition.expression` 字符串；设为 `int` 会尝试把整个表达式转换为整数，最终得到 `null`。

“仅装箱货物”遇到已有待运送货物时会关闭当前页面并继续遍历下一个仓储节点；其他需要接取或转交的模式仍会停止任务并提示先完成送货。

启用“填入指定货物”后，先按地区展开配置，再为每个地区设置优先级 1 至 4。优先级 1 默认使用砂叶粉末，
优先级 2 至 4 默认“不指定”。流程会从列表顶部完整查找当前货物，将可用数量填到最大；货箱未满时再尝试下一个
已配置货物。所有已配置货物均无法装满时，任务会停在装箱界面并明确报错。该功能使用全新的优先级配置，旧版单货物配置不会迁移。

旧版的全局“仅接取任务”和“仅装箱货物”开关已由逐仓储节点选项取代，升级后的已有配置需要重新选择各节点的处理方式。

## 新增地区或仓储节点

1. 不需要修改 `model.mjs`：地区、仓储节点完全由 `delivery_jobs.json` 驱动，MaaEnd 标识由
   数据源英文名移除空格和标点后生成，场景节点名按
   `SceneEnterMenuRegionalDevelopment{Id}[DepotNode]` 约定生成。
2. 新地区上线时，在 `assets/resource/pipeline/Interface/SceneRegionalDevelopment.json` 中补充对应的场景节点；
   `global.region.*` 与 `iconRecognition.name.*` 缺失翻译由 `sync-locales.mjs` 根据数据源自动补齐。
3. 装箱物品选项无需手工登记：精简数据依据 `FactoryItemTable.deliverItemTypeList` 与
   `transferDomainIds` 判断物品可运入的地区，生成器再取地区各仓储节点 `fillable_items` 的交集，过滤出
   `assets/data/IconRecognition/recognition_items.json` 已收录的物品，由 IconRecognition（`grid_type=shipment`）识别；
   物品显示名称复用 `iconRecognition.name.*` 多语言 key，配置值使用稳定 item ID；每个优先级槽位使用同一份地区物品列表。
4. 运行 `pnpm generate:DeliveryJobs`，再运行 `node --test tools/pipeline-generate/DeliveryJobs/*.test.mjs`、
   `pnpm check` 和 `pnpm test`。

生成的 Pipeline 和 Task 文件不应手工修改；流程级公共节点仍在 `PackCargo.json` 和 `TransferJob.json` 中维护。
