# 转交委托

使用 `MAA-pipeline-generate` 从地区、仓储节点与可装箱物品模型生成转交委托的 Pipeline 和任务配置。

本文只覆盖生成侧。任务的流程结构、六种仓储节点处理方式的语义、anchor 规则与各选项的作用，见[转交委托任务维护文档](../../../docs/zh_cn/developers/tasks/delivery-jobs-maintain.md)。

`tools/pipeline-generate/data/delivery_jobs.json` 是 zmdmap 数据 CI 从 TableCfg 裁剪并发布的精简游戏数据，MaaEnd 通过 `fetch-data.mjs` 下载。文件包含全部仓储节点及其可装箱物品；统一的数据流与来源边界见[生成数据总览](../README.md)。

## 运行方式

在仓库根目录运行：

```bash
pnpm generate:DeliveryJobs

# 仅同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap
```

`pnpm generate:DeliveryJobs` 会先执行 `pnpm generate:AutoDelivery`（内含 `fetch:zmdmap`、`sync-routes.mjs`、`sync-catalog.mjs`），再依次执行本目录的 `sync-locales.mjs`、`prepare.mjs`，最后 `run-all.mjs DeliveryJobs`。数据版本未变、只需重新生成 Pipeline 时，可以跳过网络直接跑本目录三步：

```bash
node tools/pipeline-generate/DeliveryJobs/sync-locales.mjs
node tools/pipeline-generate/DeliveryJobs/prepare.mjs
node tools/pipeline-generate/run-all.mjs DeliveryJobs
```

生成内容：

- `assets/resource/pipeline/DeliveryJobs.json`：通用任务入口与地区调度；
- `assets/resource/pipeline/DeliveryJobs/Region/*.json`：各地区入口、循环与界面判定；
- `assets/resource/pipeline/DeliveryJobs/Depot/**/*.json`：每个仓储节点的任务、货物识别和进入节点；
- `assets/resource/pipeline/DeliveryJobs/PriorityItems.json`：各地区四级装箱货物选择与回退入口；
- `assets/resource_adb/pipeline/DeliveryJobs/PriorityItems.json`：同名节点的 ADB 坐标变体；
- `assets/tasks/DeliveryJobs.json`：地区、仓储节点处理方式和装箱物品选项。

`sync-locales.mjs` 在生成前运行：地区、仓储节点及物品的五语言名称分别来自 `delivery_jobs.json`，物品是否生成则由 `recognition_items.json` 决定。它只补齐缺失或空白的 `global.region.*` / `iconRecognition.name.*` / `task.DeliveryJobs.WhatToFill*`，已有的人工消歧文案会保留；同时按 `^task\.DeliveryJobs\.WhatToFill[A-Za-z0-9]+Priority\d+$` 清理数据源里已不存在的装箱优先级键。同步结束后会再校验一遍全部键，仍缺失即报错。

`prepare.mjs` 在生成前删除 `assets/resource/pipeline/DeliveryJobs/` 下的 `Depot/` 与 `Region/` 两个目录，模板里删掉的仓储节点或地区不会留下孤儿文件；其余产物由生成器整体覆盖写入。

## 生成器结构

生成器是「模板 + 数据装配」成对出现的，由 `*-config.json` 登记：

| 配置 | 模板 | 数据 | 产物 |
| --------------------------- | ------------------------- | ---------------- | -------------------------------------------------------- |
| `core-config.json` | `core-template.jsonc` | `core-data.mjs` | `pipeline/DeliveryJobs.json` |
| `region-config.json` | `region-template.jsonc` | `region-data.mjs` | `pipeline/DeliveryJobs/Region/{RegionId}.json` |
| `depot-config.json` | `depot-template.jsonc` | `depot-data.mjs` | `pipeline/DeliveryJobs/Depot/{RegionId}/{DepotId}.json` |
| `priority-config.json` | `priority-template.jsonc` | `priority-data.mjs` | `pipeline/DeliveryJobs/PriorityItems.json`（merged） |
| `priority-adb-config.json` | `priority-adb-template.jsonc` | `priority-data.mjs` | `resource_adb/pipeline/DeliveryJobs/PriorityItems.json`（merged） |
| `task-config.json` | `task-template.mjs` | `task-data.mjs` | `assets/tasks/DeliveryJobs.json` |

`model.mjs` 是唯一的数据模型：读取 `delivery_jobs.json`、`delivery_destinations.json` 与 `assets/data/IconRecognition/recognition_items.json`，在导入时完成全部校验与断言，并导出三个消费入口：

- `deliveryJobRegions`：地区（`Id` / `RegionScene` / `DepotScene` / `Depots` / `FillItems` / `DefaultFillItem`）；
- `deliveryJobDepots`：仓储节点（`Id` / `GameId` / `RegionId` / `Expected` / `AutoDeliverySupported` / `DepotScene`）；
- `deliveryJobLocaleEntries`：待同步的多语言条目，供 `sync-locales.mjs` 使用。

地区与仓储节点按数据源 gameId 排序，装箱物品按「分类 → 中文名 → ID」排序，保证不同环境下生成结果一致。`data.test.mjs` 与 `sync-locales.test.mjs` 断言生成产物与模型的一致性。

## 任务选项的生成

`task-template.mjs` 的 `buildTaskOptions()` 按模型展开整棵选项树：

```text
{RegionId}                                             switch  启停地区，Yes 时展开该地区全部仓储节点
  └─ {DepotId}                                         select  该仓储节点的处理方式（六选一）
       ├─ DeliveryJobsQuoteThreshold{DepotId}                input   仅「按报价处理」
       ├─ DeliveryJobsAtLeastMinimumQuoteAction{DepotId}     select  仅「按报价处理」
       └─ DeliveryJobsBelowMinimumQuoteAction{DepotId}       select  仅「按报价处理」
PackCargoSelectItem                                    switch  启用后展开每地区的装箱优先级槽位
  └─ FillItemPriorities{RegionId}                      switch  逐地区启停
       └─ WhatToFill{RegionId}Priority1..4             select  每地区 4 个槽位
DeliveryJobsAutoDeliveryPreferZipline                  switch  覆盖 AutoDeliveryNavigate* 的 attach.zip
DeliveryJobsOngoingDeliveryFallback                    switch  覆盖 DeliveryJobsDeliverByAutoDelivery.on_error
```

仓储节点的六个 case 分两类生成，覆盖内容全部落在少数几个参数上：

| case | 生成函数 | 覆盖内容 |
| ---------------- | --------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `Transfer` | `buildModeOverride` | 两个入口启用；`cargoExpected=ALL_CARGO_EXPECTED`；`bidAction=DeliveryJobsRedistributionBidNextStep`；`ongoingDeliveryAction=DeliveryJobsTransferOngoingJob` |
| `AutoDelivery` | `buildAutoDeliveryOverride` | 两个入口启用；`DeliveryJobsEnter{DepotId}DeliveryJob.next` 改指 `DeliveryJobsWait{DepotId}DeliveryMissionDetail`（等任务详情界面稳定后再转 `DeliveryJobsAutoDelivery{DepotId}`）；cargo anchor 的 `DeliveryJobsGoToDepot` 改指该节点；分派节点 `next` 改指该节点 |
| `ByQuote` | `buildModeOverride` | 只启用货物入口；`bidAction=DeliveryJobsDecide{DepotId}Quote`；`ongoingDeliveryAction=DeliveryJobsSkipOngoingDelivery` |
| `AcceptJobOnly` | `buildModeOverride` | 只启用货物入口；`bidAction=DeliveryJobsRedistributionBidNextStep`；`ongoingDeliveryAction=DeliveryJobsSkipOngoingDelivery` |
| `PackCargoOnly` | `buildModeOverride` | 只启用货物入口；`cargoExpected=PACK_CARGO_EXPECTED`（不含「查看报价」）；`bidAction=DeliveryJobsCloseRedistributionBid`；`ongoingDeliveryAction=DeliveryJobsSkipOngoingDelivery` |
| `Disabled` | `buildModeOverride` | 两个入口 `enabled: false`，不覆盖其余节点 |

`buildModeOverride()` 的 `deliveryEnabled` / `cargoEnabled` 落到 `DeliveryJobsEnter{DepotId}DeliveryJob` / `DeliveryJobsEnter{DepotId}Cargo` 的 `enabled`；`cargoEnabled` 为真时还会覆盖 `DeliveryJobsCheck{DepotId}Cargo.expected` 与分派节点 `DeliveryJobsOngoingDeliveryFor{DepotId}.next`。

> [!IMPORTANT]
>
> **`pipeline_override` 对 `DeliveryJobsEnter{DepotId}Cargo` 的 `anchor` 提供的是整个对象**（`buildCargoAnchor()` 的返回值），模板中该节点的 `anchor` 会被整体替换；`DeliveryJobsCheck{DepotId}Cargo.expected` 同理。改模板的 anchor 键或货物识别文本时，必须同步改 `buildCargoAnchor()` / `cargoExpected`，否则改动会被静默覆盖。

没有送货终点的仓储节点不生成 `AutoDelivery` case：`model.mjs` 用 `delivery_destinations.json` 的 `destinations[].depot_id` 归并终点，`AutoDeliverySupported` 为假的仓储节点在仓储节点模式与两个报价分支里都不出现「全自动送货」。终点坐标与路线的生成属于 AutoDelivery 生成器，见 [AutoDelivery README](../AutoDelivery/README.md)。

`buildQuoteThresholdOption()` 把阈值注入 `ExpressionRecognition.expression`（`{DeliveryJobsSelectedBidPrice}>=<阈值>` 与 `{DeliveryJobsSelectedBidPrice}<{阈值}`）。输入的 `pipeline_type` **必须保持 `"string"`**：输入值会被插入表达式字符串，设为 `int` 会把整个表达式当整数解析，最终得到 `null`。

## 数据约束与生成期断言

`model.mjs` 在导入时完成以下校验，任一不满足即中断生成：

- **五语言名称完整**：地区、仓储节点、物品的 `zh_cn` / `zh_tw` / `en_us` / `ja_jp` / `ko_kr` 缺一即报错。识别用的 `expected` 由 `buildLocalizedExpected()` 生成：`en_us` 转成带 `(?i)` 前缀、词间空格放宽为 `\s*` 的弹性正则，其余语言原样。
- **区域名恒等**：`delivery_destinations.json` 的 `destinations[].area` 必须与所属仓储节点 `depots[].names` 在五种语言下逐字一致（`assertDepotAreaNames()`）。Go 侧 `DeliveryJobsResolveOngoingDepotAction` 用区域 ID 直接拼出分派节点名，这条恒等关系是跨语言免映射表的前提。
- **MaaEnd 标识可生成**：`Id` 由英文名移除非字母数字字符得到，必须匹配 `^[A-Za-z][A-Za-z0-9]*$`；地区 ID、仓储节点 ID，以及两者的合集都不得重复。
- **归属一致**：仓储节点的 `region_id` 必须等于它被挂到的地区；地区至少要有一个仓储节点。
- **可装箱物品**：取该地区各仓储节点 `fillable_items` 的**交集**，再过滤 `recognition_items.json` 未收录的物品（跳过时告警），最后排序。每个地区都必须能装箱默认物品 `item_plant_moss_powder_3`（砂叶粉末），否则报错。

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
4. 运行 `pnpm generate:DeliveryJobs`，并检查 `git diff` 只包含预期的生成产物；`pnpm check` / `pnpm test`
   按需执行，改动未包含 `tests/**` 时交给 PR 的 CI 校验即可。

## 维护边界

生成的 Pipeline 和 Task 文件不应手工修改；流程级公共节点仍在 `PackCargo.json`、`TransferJob.json` 和 `AutoDelivery.json` 中维护，改动时的检查清单见[转交委托任务维护文档](../../../docs/zh_cn/developers/tasks/delivery-jobs-maintain.md)。
