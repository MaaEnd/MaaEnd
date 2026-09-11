# 开发手册 - SeizeDeliveryJobs 抢委托送货维护

本文说明 `SeizeDeliveryJobs`（抢委托送货）任务的运行流程、Pipeline 与 Go Service 的职责边界、生成器输入输出，以及新增地图、区域和送货终点时的维护方法。

本任务由两条相对独立的链路组成：

- 不限制送货终点时，Go Service 只按奖励下限扫描委托，并点击第一个符合条件的接取按钮。
- 开启终点筛选时，Go Service 先缓存所有符合奖励下限的委托；Pipeline 逐个打开「查看位置」，再用各区域生成的 `MapFind` 候选判断终点。

两条链路共用仓储入口、委托列表、奖励 OCR 和接取后处理。维护时应让 Pipeline 负责界面状态和业务跳转，让 Go Service 负责委托列表的复杂识别与动态点击坐标。

> [!WARNING]
>
> `assets/tasks/SeizeDeliveryJobs.json`、`assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsCommission.json`、`SeizeDeliveryJobsEndpointCandidates.json`、`SeizeDeliveryJobsEndpointDispatcher.json`、`SeizeDeliveryJobsDestinations.json` 都是生成产物。不要直接编辑这些文件；下次运行生成器时，手改内容会被覆盖。
>
> `assets/locales/interface/*.json` 中的区域、终点和命中提示也由生成器维护。终点文案应编辑 `endpoint-labels.json`，不要直接改生成后的 locale 键。

## 要点速览

| 模块 | 路径 | 作用 |
| ---------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 任务定义（生成） | `assets/tasks/SeizeDeliveryJobs.json` | 奖励下限、委托来源、终点筛选、后处理方式和重复次数等任务选项 |
| 主流程（手工） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobs.json` | 任务入口、仓储入口分支、抢单循环、奖励下限识别和抢单成功/失败处理 |
| 委托列表公共节点（手工） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsCommon.json` | 进入委托列表、列表加载完成判断和委托卡片的 OCR 节点 |
| 区域仓储节点（生成） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsCommission.json` | 每张地图的调度券识别、仓储入口、仓储文本确认和地区筛选节点 |
| 终点候选（生成） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsEndpointCandidates.json` | 每个送货区域的 OCR 门控和 `MapFind` 候选列表 |
| 终点叶子节点（生成） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsDestinations.json` | 每个终点的 `enabled` 开关、命中提示和统一后继 |
| 终点调度器（生成） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsEndpointDispatcher.json` | 确认地图已打开，并按左上角子区域名路由到对应候选组 |
| 终点筛选循环（手工） | `assets/resource/pipeline/SeizeDeliveryJobs/SeizeDeliveryJobsEndpointFilter.json` | 缓存委托、打开查看位置、判断终点、关闭地图、接单和刷新 |
| 自动送货接续（手工） | `assets/resource/pipeline/SeizeDeliveryJobs/AutoDeliveryAdapter.json` | 通过 continuation anchor 调用公共 `AutoDelivery`，不复制送货流程 |
| 终点展示名（手工） | `tools/pipeline-generate/SeizeDeliveryJobs/endpoint-labels.json` | 人工覆盖终点 ID 对应的五种语言展示名；留空时回退到游戏数据中的收货人名称 |
| Go Service | `agent/go-service/seizedeliveryjobs/` | 委托卡片链式 OCR、奖励解析、动态点击坐标和一次扫描的会话状态 |
| 共享送货目录 | `tools/pipeline-generate/data/delivery_destinations.json` | zmdmap 发布的仓储、终点、地图坐标、区域和五语言源数据；由 `AutoDelivery` 生成器读取 |

生成器总览和 zmdmap 数据更新方式见 [`tools/pipeline-generate/README.md`](../../../../tools/pipeline-generate/README.md)；自动送货路线的详细规则见 [`AutoDelivery` 组件文档](../components/auto-delivery.md)。

## 运行流程

### 任务入口和来源筛选

`SeizeDeliveryJobsMain` 先执行风险知悉拦截，再进入对应地图的仓储管理页面。进入委托列表后，`SeizeDeliveryJobsReadyToSeize` 依次检查已有委托、今日接单次数是否耗尽，以及当前地区筛选是否正确。

当前来源与地图的关系如下：

| 任务选项 ID | 委托来源 | 仓储入口地图 | 委托列表筛选 | 终点选项 |
| ------------------------ | ----------------------------------------- | -------------------------- | ---------------------------- | ----------------------------------------------- |
| `WulingCity` | 武陵城 | `Wuling` | 武陵 | 武陵城终点 |
| `TestArea` | 试验园区 | `Wuling` | 武陵 | 试验园区终点 |
| `OriginiumSciencePark` | 源石研究园 | `ValleyIV` | 四号谷地 | 源石研究园终点 |
| `OriginLodespring` | 矿脉源区 | `ValleyIV` | 四号谷地 | 矿脉源区终点 |
| `PowerPlateau` | 供能高地 | `ValleyIV` | 四号谷地 | 供能高地终点 |
| `Unlimited` | 武陵城 + 试验园区 | `Wuling` | 武陵 | 武陵城与试验园区终点；保留旧选项名 |
| `ValleyIVUnlimited` | 四号谷地三个区域 | `ValleyIV` | 四号谷地 | 四号谷地三个区域终点 |
| `AllUnlimited` | 目录中的全部区域 | `Wuling` | 全部区域 | 目录中的全部终点 |

`Unlimited` 是历史兼容项，不能随意改名或删除：用户保存的配置可能仍引用这个选项 case。`AllUnlimited` 才是覆盖全部区域的聚合选项。来源选项的 `pipeline_override` 会同时覆盖 `__SeizeDeliveryJobsRecoOrigin`、仓储入口和地区筛选节点，所以新增区域不能只补一条 locale 文案。

聚合选项的终点范围由 `task-data.mjs` 按地图筛选 `candidatesRows` 生成：`AllUnlimited` 覆盖目录中的全部区域，`Unlimited` 覆盖 `map02`（武陵）的全部区域，`ValleyIVUnlimited` 覆盖 `map01`（四号谷地）的全部区域。新增区域会自动并入 `AllUnlimited`，另外两个聚合选项只在其所属地图新增区域时才变化。游戏内的「全部区域」筛选本身就跨两张地图，所以 `AllUnlimited` 只用一个仓储入口就够了；`Unlimited` 是为兼容历史用户配置而保留的武陵语义，键名保持不变。

来源选项的 `pipeline_override` 是字段级替换，`task-data.mjs` 会整体替换 `SeizeDeliveryJobsMain.next` 和 `SeizeDeliveryJobsReadyToSeize.next`。因此这两个列表必须完整列出需要保留的节点：`SeizeDeliveryJobsGuard`（风险知悉拦截）、`SeizeDeliveryJobsExistDueTask`（已有委托检查）和 `SeizeDeliveryJobsFailedChanceExhausted`（今日接单上限检查）。漏写任何一个，对应检查都会静默失效——运行时生效的是覆盖后的列表，默认列表已被整体替换。

### 不限制终点

当所有「指定送货点」开关都保持关闭时，任务启用 `SeizeDeliveryJobsFindTarget`：

```text
SeizeDeliveryJobsLoop
  └─ SeizeDeliveryJobsFindTarget
       └─ Go 识别整列调度券的多个 box
            └─ 每个 box 链式 OCR 奖励、出发地、接取和查看位置
                 ├─ 奖励 < 下限       → 跳过
                 ├─ OCR 不完整/解析失败 → 跳过
                 └─ 奖励达标            → 点击当前列表最上方的接取按钮
                      ├─ 接取成功 → 后处理或结束
                      └─ 接取动画未出现 → 刷新列表后重新扫描
```

`__SeizeDeliveryJobsRecoCommissionToken` 是抢单的**行锚点**：它用 `TemplateMatch` 识别每行的**调度券图标**（武陵调度券 / 四号谷地调度券），Go 再以这个 box 为锚点，按固定偏移推导奖励数字、出发地、接取按钮和查看位置的 roi。

该节点是**单节点 + 多模板**：`template` 列出每张地图的调度券图（`WulingToken.png` = 武陵调度券，`ValleyIVToken.png` = 四号谷地调度券），一次识别就能拿到整列 box，包含「全部地区」下混排的多地图委托；`order_by: Vertical` 保证自上而下。

不要改成 `Or` 组合每张地图的节点：`Or` 只返回首个命中子节点的结果，混排列表里会漏掉另一张地图的整列委托。新增地图也要自带调度券图，裁剪要求见「新增地图」第 4 步。

### 指定终点

当某个「指定送货点」开关为 Yes 时，任务通过 `pipeline_override` 关闭 `SeizeDeliveryJobsFindTarget`，启用 `SeizeDeliveryJobsScanTarget` 和 `SeizeDeliveryJobsEndpointFilter`。流程如下：

```text
SeizeDeliveryJobsScanTarget
  └─ Go 首次调用：缓存本轮所有奖励达标委托及其动态 box
       └─ SeizeDeliveryJobsFoundTargetViewLocationClick
            └─ 打开当前委托的「查看位置」地图
                 └─ SeizeDeliveryJobsEndpointFilter
                      ├─ 地图已打开 → OCR 左上角子区域名
                      │    └─ 路由到该区域的 MapFind 候选
                      │         ├─ 命中已启用终点 → 关闭地图 → 点击接取
                      │         └─ 未命中 → 关闭地图 → 检查下一张缓存委托
                      └─ 缓存委托全部检查完 → 清空状态 → 刷新委托列表
```

区域门控节点用 ROI `[16, 14, 214, 41]` 读取地图左上角的子区域名，以避免在不限来源时误用另一张地图的终点候选。每个区域的 `MapFind` 节点只使用一个 `zone`，其 `candidates` 的 `at` 坐标来自送货目录中的地图坐标；这些是地图世界坐标，不是 1280×720 屏幕坐标。

筛选循环节点 `SeizeDeliveryJobsEndpointFilter` 用 `__SeizeDeliveryJobsRecoAnyDepotNode`（任一地图仓储管理页面的特征标志，`InLocalDepotNode` 的封装）反向判断地图视图已经打开：仍停在仓储管理页面时该标志存在，节点不命中。它不绑定具体地图，新增区域时只需在它的 `next` 里追加新的区域门控节点。

每个终点的生成节点 `SeizeDeliveryJobsEndpointFilter<EndpointId>` 默认 `enabled: false`。任务选项只通过 Pipeline override 打开用户勾选的叶子节点。叶子节点不再重复识别图标，命中后统一跳转 `SeizeDeliveryJobsEndpointMatched`；这样一次 `MapFind` 就能在同一区域内判断所有已启用终点。

### 接单后的后处理

任务选项 `SeizeDeliveryJobsPostProcessing` 决定接单成功后或启动时发现已有委托时如何处理：

| case | 行为 |
| ---------------------- | ------------------------------------------------------------------------ |
| `Disable` | 接单成功后结束；如果手上已有委托，提示同时只能接一个单并结束 |
| `Tele` | 调用 `AutoDelivery`；需要取货时只快速传送到仓储附近 |
| `TeleWalk` | 快速传送后走到仓储节点；可选是否优先使用滑索 |
| `TeleWalkFetchDeliver` | 调用完整 `AutoDelivery`，自动判断取货/送货并完成委托；可配置风险知悉、滑索偏好和重复次数 |

`TeleWalkFetchDeliver` 的重复次数通过 `SeizeDeliveryJobsMain.max_hit` 设置为 1、2 或 3。风险知悉选项默认为 No，`SeizeDeliveryJobsGuard` 会拦截自动走路流程；只有用户明确选择 Yes 才会放行。`AutoDeliveryAdapter.json` 只提供入口和 continuation anchor，送货路线、导航和完成委托的逻辑仍由公共 `AutoDelivery` 负责。

## 数据和生成器

### 输入数据流

区域仓储节点和终点节点都使用与 `AutoDelivery` 相同的目录模型：

```text
zmdmap
  └─ tools/pipeline-generate/data/delivery_destinations.json
       └─ AutoDelivery/model.mjs
            ├─ destinations（地图、区域、MapFind zone、终点 u/v 坐标和收货人名称）
            │    └─ endpoint-filter-data.mjs → 终点行、区域 candidates 行和调度器行
            │         ├─ endpoint-labels.mjs → 维护 endpoint-labels.json 中的人工五语言名称
            │         ├─ endpoint-candidates-data.mjs / endpoint-dispatcher-data.mjs → 再导出上述行
            │         └─ task-data.mjs → 任务 case 与终点开关，并调用 sync-locales.mjs 同步五语言文案
            └─ depots（仓储及其所属地图）
                 └─ commission-data.mjs → 每张地图的仓储入口、文本确认和地区筛选行
```

`delivery_destinations.json` 是 zmdmap 的精简数据，不要手工编辑。运行 `pnpm fetch:zmdmap` 会更新它以及同目录的其他任务数据。`AutoDelivery/model.mjs` 还会读取 `tools/pipeline-generate/AutoDelivery/routes.json` 来校验路线覆盖；新增终点若要支持完整自动送货，还必须先让 `AutoDelivery` 生成对应路线和运行时目录。生成器只能看到 `depots` 中存在的仓储，因此没有仓储条目的地图不会出现在 `SeizeDeliveryJobsCommission.json` 里。

### 生成器配置和产物

`tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs` 会按文件名顺序扫描所有 `*config.json`，生成以下文件：

| 配置 | 模板 | 产物 | 是否手改 |
| ----------------------------- | ------------------------------------ | ------------------------------------------------------------ | -------- |
| `commission-config.json` | `commission-template.jsonc` | `SeizeDeliveryJobsCommission.json` | 否 |
| `endpoint-candidates-config.json` | `endpoint-candidates-template.jsonc` | `SeizeDeliveryJobsEndpointCandidates.json` | 否 |
| `endpoint-dispatcher-config.json` | `endpoint-dispatcher-template.jsonc` | `SeizeDeliveryJobsEndpointDispatcher.json` | 否 |
| `endpoint-filter-config.json` | `endpoint-filter-template.json` | `SeizeDeliveryJobsDestinations.json` | 否 |
| `task-config.json` | `task-template.jsonc` | `assets/tasks/SeizeDeliveryJobs.json` | 否 |

生成时 `task-data.mjs` 会调用 `syncSeizeDeliveryJobsLocales()`，向五个 `assets/locales/interface/*.json` 写入区域标签、终点标签和命中提示。区域标签只补缺失值，已有非空区域文案会保留；终点标签和终点命中提示则每次生成都按 `endpoint-labels.json` 同步，以保证人工名称变化立即生效。

`run-all.mjs` 在渲染 task 或 `merged` 配置前会删除对应的旧单文件产物，防止已删除的区域、终点或选项残留。因此生成前应确认当前数据和配置没有未保存的手工内容。

### 运行命令

推荐在仓库根目录执行：

```bash
# 更新 zmdmap 数据并重新生成 SeizeDeliveryJobs 全部生成产物
pnpm generate:SeizeDeliveryJobs

# 只使用已有本地数据重新渲染，不访问 zmdmap
node tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs
```

如果本次更新带来了新的仓储或终点，并且要使用 `TeleWalkFetchDeliver`，先生成公共自动送货资源，再生成抢单任务：

```bash
pnpm generate:AutoDelivery
pnpm generate:SeizeDeliveryJobs
```

`generate:SeizeDeliveryJobs` 本身不会生成 `assets/resource/pipeline/AutoDelivery/` 或 `assets/data/AutoDelivery/catalog.json`。只改已有终点的展示名时，不需要重新生成 AutoDelivery。

## 终点展示名称维护

### `endpoint-labels.json` 的规则

文件键是 `delivery_destinations.json` 中的原始终点 ID，值必须包含以下五种语言字段：

```jsonc
{
    // 矿脉源区
    // 莫莉
    "deliver_target_map01_lv006_03": {
        "zh_cn": "",
        "zh_tw": "",
        "en_us": "",
        "ja_jp": "",
        "ko_kr": ""
    }
}
```

规则如下：

- 非空字符串是人工维护的地标名，便于用户在任务选项和日志中定位终点。
- 空字符串或仅含空白字符时，该语言的显示名回退为 `delivery_destinations.json` 中该终点的收货人名称。
- 人工名称和数据源名称都为空时，生成器直接报错，不生成空白任务选项或空白命中提示。
- 生成器只会补缺少的终点和语言字段，不会用回退名称覆盖人工文件；每次生成会重建上面示例中的地图、区域和收货人注释。
- 注释不是程序输入，不要依赖手工修改注释来改变名称；数据源更新后重新生成即可刷新注释。

### ID 兼容和排序

已有终点的内部 `EndpointId` 通过 `endpoint-filter-data.mjs` 中的 `LEGACY_ENDPOINTS` 保留。当前兼容 ID 包括 `Owl`、`MaterialResearchInstitute`、`Observatory`、`TechProductionOffice`、`No1TypeCAnchorArea`、`No3TypeCAnchorArea` 和 `JingweiFieldArea`。不要为了让 ID 更符合新地标名而重命名它们，否则会同时影响：

- 已保存用户配置中的选项 case；
- `SeizeDeliveryJobsEndpointFilter<EndpointId>` 节点名；
- locale 键和命中提示键；
- `MapFind` candidate 的 `next` 引用。

不在兼容表中的新终点，会由原始 ID 自动转换为 PascalCase。例如：

```text
deliver_target_map01_lv006_03
  → DeliverTargetMap01Lv00603
  → SeizeDeliveryJobsEndpointFilterDeliverTargetMap01Lv00603
```

旧终点保持既有顺序，新终点按 `AutoDelivery/model.mjs` 输出的源 ID 顺序排在旧终点之后；删除源数据中的终点后，重新生成会删除相应产物，不会保留孤立选项。

区域 ID 不走兼容表，而是从数据自动派生：`area.en_us` 去掉非字母数字字符（`Origin Lodespring` → `OriginLodespring`，`Test District` → `TestDistrict`）。它同时决定「委托接收点」的来源 case 名、`SeizeDeliveryJobsDeliveryPoint<AreaId>` 选项名和区域门控节点名，因此改动数据源中的英文区域名会一并改名。

### 只修改已有终点名称

1. 在 `endpoint-labels.json` 中按原始终点 ID 填写或清空对应语言字段。
2. 执行 `pnpm generate:SeizeDeliveryJobs`，或在数据已更新的情况下执行 `node tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs`。
3. 检查 `assets/tasks/SeizeDeliveryJobs.json` 的终点 case、`SeizeDeliveryJobsDestinations.json` 的 `desc`，以及五个 locale 文件中对应的选项／命中提示文案。

不要直接修改生成后的 `assets/tasks`、Pipeline 或 locale。下一次生成会以 `endpoint-labels.json` 和 zmdmap 数据为准。

## 新增终点、区域或地图

### 新增当前区域内的终点

如果 zmdmap 已经提供新终点，并且它属于现有区域、使用现有 `MapFind zone`：

1. 运行 `pnpm fetch:zmdmap`，确认 `delivery_destinations.json` 中出现新终点及其 `u`/`v` 坐标。
2. 运行 `pnpm generate:AutoDelivery`，让 AutoDelivery 同步 `routes.json` metadata、生成导航节点和 `catalog.json`。
3. 在 `endpoint-labels.json` 中检查新 ID；生成器会自动补出五个空字段。需要地标名时填写字段，否则保留空值使用收货人名称回退。
4. 运行 `pnpm generate:SeizeDeliveryJobs`。
5. 检查新终点是否同时出现在：对应区域的 task checkbox、`SeizeDeliveryJobsDestinations.json`、该区域 `MapFind` 的 `candidates`，以及五个 locale 文件。
6. 在节点测试或实机上确认地图缩放后 `DeliveryPoint.png` 能在新 `at` 坐标命中，并确认命中后能返回委托列表继续接单。

终点候选识别使用 `assets/resource/image/SceneManager/MapIcons.json` 中登记的 `DeliveryPoint` 图标。所有终点共用 `assets/resource/image/SeizeDeliveryJobs/DeliveryPoint.png`；只有游戏视觉资源变化时才需要更新这张模板图及其测试集。

### 新增区域

新增区域先判断属于哪种情况：

**A. 与已有区域同属一张地图**

除上游数据外不需要任何手工改动。若新区域同时带来新终点，先执行 `pnpm generate:AutoDelivery`，再执行 `pnpm generate:SeizeDeliveryJobs`（见「运行命令」）。以下内容会自动生成：

- 「委托接收点」的来源 case：按所属地图归组，排在旧区域之后；`LEGACY_AREA_ORDER` 只用于保持旧区域的既有顺序；
- 区域送货点选项、该区域每个终点的 case 和「指定送货点」开关；
- 区域门控 OCR 的 `expected`（五语言区域名）和 `MapFind` 的 `at` 坐标；
- 「武陵-全部」「四号谷地-全部」「全部地区」三个聚合选项的 `expected` 自动包含新区域；
- 五个 `assets/locales/interface/*.json` 中的区域标签（只补缺失值）。

**不需要新增图片**：区域门控用 OCR，终点共用 `DeliveryPoint.png`，筛选按钮按地图（`Filter${MapName}.png`）。仍需人工判断的只有 `endpoint-labels.json`：自动回退的收货人名有歧义时填写区分名，见「终点展示名称维护」。

**B. 属于一张新地图**

新地图需要逐项接入，建议按以下顺序检查：

1. 确认 zmdmap 数据提供区域名称、所属仓储、地图 `u`/`v` 坐标和五种语言文本。
2. 在 `tools/pipeline-generate/SeizeDeliveryJobs/commission-data.mjs` 的 `mapNames`、`mapLabels` 和 `depotTextNodes` 中补充新地图映射；每行会导出 `MapId`、`MapName`、`AreaName`、`DepotTextNode` 和 `Labels`。
3. 在 `tools/pipeline-generate/SeizeDeliveryJobs/task-data.mjs` 的 `mapId → mapName` 逻辑中补充新地图；当前代码只区分 `map01 → ValleyIV`，其他值回退为 `Wuling`，这套映射逻辑不能直接复用到第三张地图。
4. 确认 `Filter${MapName}.png` 存在，并**为新地图单独截一张调度券图**，放到 `assets/resource/image/SeizeDeliveryJobs/DepotNodePage/${MapName}Token.png`（并按第 5 步追加到 `template`）。尺寸与现有两张对齐（47×33，从 720p 截图原样裁剪），另外：
   - **锚点与现有模板一致**：Go 以调度券 box 为锚点、按固定偏移推导后续 roi，所以新模板的匹配 y 必须落在相同的行内相对位置。可用调度券卡片底边那条黄色横线的中心标定——现有两张图在各自匹配行的偏移都是 41.5~42.5px，裁偏几像素该行的奖励 roi 就会跟着偏。
   - **不包含奖励数字和底部黄线**
5. 在手工 Pipeline 中接入新地图：
   - `SeizeDeliveryJobsCommon.json` 的 `__SeizeDeliveryJobsRecoCommissionToken.template` 追加新地图的调度券图；
   - `SeizeDeliveryJobs.json` 的默认 `Main.next` 和 `ReadyToSeize.next` 也补上新地图入口/筛选节点，让默认列表保持完整；运行时实际生效的是 `task-data.mjs` 按 case 覆盖后的 `next`；
   - 确认对应 `SceneManager` 仓储入口和 `DeliveryJobsCheckLocalDepotNode${DepotTextNode}`（如 `DeliveryJobsCheckLocalDepotNodeWulingCityText`）已存在；`DepotTextNode` 的值本身已带 `Text` 后缀。
6. 执行 `pnpm generate:AutoDelivery`，再执行 `pnpm generate:SeizeDeliveryJobs`。
7. 检查生成的区域门控、`MapFind zone` 和候选节点。如果同一区域的终点跨多个 `MapFind zone`，当前生成器会主动报错；需要先扩展区域分组模型和模板，不能把不同 zone 强行放进同一个候选（`candidates`）节点。

区域门控 `expected` 应使用完整的五语言区域名，并保持与地图左上角 OCR 的实际文本一致。区域名只用于门控路由，不能用终点收货人名称代替。

### 新增或修改路线

抢单任务的终点定位只依赖 `delivery_destinations.json` 的地图坐标；取货和送货路线由公共 `AutoDelivery` 维护。新增或修改路线时：

- 使用 `tools/pipeline-generate/AutoDelivery/routes.json` 保存实测路线覆盖，不要把完整导航路径写入 SeizeDeliveryJobs 的 `MapFind` candidates。
- 运行 `pnpm generate:AutoDelivery`，确认对应 `AutoDeliveryRoute...`、重试路线和 `assets/data/AutoDelivery/catalog.json` 已更新。
- 再运行 SeizeDeliveryJobs 生成器，确保终点 task 选项和地图候选仍引用相同的源 ID。

送货路线中 `NAVMESH` 的地图坐标不是屏幕坐标；只有 Pipeline 的 `roi`、固定 `target` 和模板图裁剪区域遵循 1280×720 基准。导航、MapFind 与界面识别中的坐标不要混用。

## Go Service 维护

### 注册的组件

`agent/go-service/seizedeliveryjobs/register.go` 注册四个组件：

| 注册名 | 类型 | 用途 |
| ------------------------------------------------ | -------- | ------------------------------------------------------------------- |
| `SeizeDeliveryJobsFindTargetRecognition` | Custom Recognition | 不限制终点时扫描并返回首个奖励达标委托的接取按钮 |
| `SeizeDeliveryJobsScanTargetRecognition` | Custom Recognition | 指定终点时首次扫描并缓存奖励达标委托 |
| `SeizeDeliveryJobsScanTargetAction` | Custom Action | 获取当前缓存委托的查看位置/接取动态 box，并推进索引 |
| `SeizeDeliveryJobsResetScanStateAction` | Custom Action | 终点命中或扫描耗尽后清空缓存和索引 |

若重命名或增删组件，必须同步修改：

- `agent/go-service/seizedeliveryjobs/register.go`；
- `agent/go-service/register.go` 中的 `seizedeliveryjobs.Register()`（只有新增/删除子包时才需要调整）；
- `tools/schema/custom.recognition.schema.json` 或 `tools/schema/custom.action.schema.json`；
- 全部 Pipeline 中的 `custom_recognition` / `custom_action` 用法。

### 奖励解析和动态 box

Go 从 `__SeizeDeliveryJobsMinReward.expected[0]` 读取任务输入，并把奖励统一换算为「万」：

- `万` / `萬`：直接使用数字；
- `K`：除以 10，例如 `119K = 11.9 万`；
- `M`：乘以 100，例如 `1.2M = 120 万`；
- 无单位：按已经是「万」处理。

委托卡片的后续 OCR ROI 都由前一个识别结果的 box 加偏移得到：先找调度券图标，再找奖励数字，最后找出发地、接取和查看位置。不要在 Go 中写死每张卡片的绝对屏幕坐标；界面布局变化时优先检查 Pipeline 的基础 ROI、OCR 命中框和偏移关系。

### 扫描状态边界

指定终点模式中的 `scannedJobItems` 和 `currentIndex` 是一次任务进程内的临时扫描状态，不落盘：

- 首次 `ScanTargetRecognition` 扫描并缓存所有奖励达标委托，后续识别复用缓存，不重复扫描整列。
- 终点命中后由 `ResetScanStateAction` 清空状态，再点击当前缓存委托的接取按钮。
- 终点未命中时关闭地图并检查下一张缓存委托；全部委托检查完后同样先清空状态，再刷新列表。
- 委托列表刷新后必须重新扫描，不能继续使用旧 box。

如果新增分支会离开委托列表或刷新委托数据，必须明确接入 reset 节点，否则下一轮可能使用已经失效的动态点击坐标。

## 常见问题与排查顺序

| 现象 | 优先检查 |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 生成器提示找不到数据文件 | 运行 `pnpm fetch:zmdmap`；不要创建一个不完整的本地 `delivery_destinations.json` 冒充数据源 |
| 终点选项为空或生成失败 | 检查终点 ID 是否存在、五个语言字段是否为字符串；至少保证人工名称或数据源收货人名称非空 |
| 新终点有选项但 `MapFind` 不命中 | 检查 `mapAt`/`u`/`v`、`MapFind zone`、`DeliveryPoint` 图标登记和地图缩放后的实际位置；`at` 不是屏幕 ROI |
| 终点筛选总是走错区域 | 检查区域门控 OCR ROI `[16, 14, 214, 41]`、五语言 `expected`，以及该区域的终点是否都归入同一个 `MapFind zone` |
| 指定终点模式没有检查下一张委托 | 检查未命中后的 ESC → `SeizeDeliveryJobsScanTarget` 链路，以及扫描耗尽后的 reset → refresh 链路 |
| 不限制终点模式完全识别不到委托 | 检查 `__SeizeDeliveryJobsRecoCommissionToken` 的 `template` 是否含当前地图的调度券图，以及调度券 ROI、奖励 OCR 和来源 `expected`；`maafw.log` 里的 `filtered_results_=[]` 且分数低于 `param_.thresholds` 只说明没过阈值，还要排查 roi 与当前画面 |
| 新加的调度券图分数偏低 | 给该节点加 `threshold` 数组按模板分别设阈值（长度与 `template` 一致，如 `[0.7, 0.6]`）；阈值调低后两图可能互相误匹配，同一行拿到两个 box，需要在 Go 侧按 y 去重 |
| 委托识别到了但奖励读不到/接取失败 | 多半是该地图调度券图的锚点裁偏：按偏移推导出的奖励/出发地/接取/查看位置 roi 会整体平移，按「新增地图」第 4 步用行底黄线重新标定 |
| 接单后自动送货找不到路线 | 先运行 `pnpm generate:AutoDelivery`，检查对应 `AutoDelivery/catalog.json` 和生成路线；`SeizeDeliveryJobs` 生成器不会代替 `AutoDelivery` 生成这些文件 |
| 修改名称后旧配置失效 | 检查是否改动了 `LEGACY_ENDPOINTS` 对应的内部 ID；展示名称应改 `endpoint-labels.json`，不要改 case/node ID |

定位问题时先看 `maafw.log`、`go-service.log` 和节点 focus 文案，确认失败发生在列表入口、委托 OCR、终点地图路由还是接单后的 AutoDelivery，再修改对应层。不要通过增加重试或固定延迟掩盖识别链路问题。

## 提交前检查

只修改已有终点名称时，至少运行：

```bash
node tools/pipeline-generate/run-all.mjs SeizeDeliveryJobs
node --test tools/pipeline-generate/DeliveryJobs/data.test.mjs
pnpm check
pnpm test
pnpm format:md:check
git diff --check
```

新增终点、区域或路线时，再确认：

```bash
pnpm generate:AutoDelivery
pnpm generate:SeizeDeliveryJobs
```

检查生成 diff 是否只包含预期的 task、Pipeline、locale 和 AutoDelivery 产物；确认没有直接编辑生成文件，也没有出现以下划线 `_` 开头的新资源目录。代码或 JSON 有修改时，按项目约定补跑 `pnpm format`、`pnpm format:go`、`pnpm check` 和 `pnpm test`。
