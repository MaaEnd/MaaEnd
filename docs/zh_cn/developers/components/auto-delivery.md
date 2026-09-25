# 开发手册 - AutoDelivery 送货组件

AutoDelivery 是任务无关的自动送货组件。调用方打开正确的当前送货任务详情后，只需进入公共节点 `AutoDelivery`；组件会自行判断当前需要取货还是送货，并完成对应流程。

## 调用方式

### 前置条件与入口

调用方只负责触发打开**正确的当前送货任务详情**。组件会等待详情页加载完成，但不负责首次从任务列表或仓储节点打开详情，因为不同任务进入详情页的方式不同。

```json
{
    "MyTaskOpenCurrentJobDetail": {
        "next": [
            "AutoDelivery"
        ]
    }
}
```

`AutoDelivery` 是唯一公共执行入口。其余 `AutoDelivery...` 节点属于组件实现或配置契约，不应作为独立步骤写入调用方的 `next`。

### 默认流程

```text
当前送货任务详情
  -> AutoDelivery
       -> 已取货：识别终点 -> 取消追踪 -> 返回大世界 -> 前往终点 -> 提交货物
       -> 未取货：识别仓储 -> 快速传送 -> 取消追踪 -> 返回大世界
            -> 前往仓储 -> 取货 -> 重新打开送货任务详情
            -> 识别终点 -> 取消追踪 -> 返回大世界 -> 前往终点 -> 提交货物
```

组件按任务详情中的区域、任务条件和操作按钮自行判断阶段。快速传送后、资源回收站地图判定后以及取货后需要重新打开任务详情时，组件会通过 `SubTask` 依次执行 `SceneEnterMenuMission` 和送货任务选择流程；只有同时确认任务界面与右侧“送货任务”详情后，才会取消追踪或继续识别目的地。调用方无需为详情切换配置额外入口或 anchor。

页面信息无法识别、路线无法解析或操作失败时，组件不转发额外的 `on_error`，而是按 Pipeline 默认行为结束任务。调用方若需要在某个正常阶段返回自身流程，应使用下节的 anchor，不要直接引用内部节点。

## 阶段出口 anchor

四个阶段出口均采用“anchor 优先、默认节点兜底”的形式。未设置时继续默认流程；调用方只在需要截断流程或在提交后继续自身任务时配置：

| anchor | 触发位置 | 未设置时的默认行为 | 典型用途 |
| --- | --- | --- | --- |
| `AutoDeliveryAfterRecognizeDestination` | 已识别到送货终点后 | 取消追踪并前往终点 | 已取货时禁止仅传送、仅走到仓储等模式继续移动 |
| `AutoDeliveryAfterQuickTeleport` | 快速传送到仓储附近后 | 取消追踪并前往仓储 | 仅快速传送 |
| `AutoDeliveryAfterNavigateDepot` | 到达仓储后 | 取货 | 仅走到仓储节点 |
| `AutoDeliveryAfterSubmitGoods` | 关闭送货奖励界面后 | 正常结束组件 | 返回调用方主循环或完成节点 |

完整送货通常只需配置提交后的出口：

```json
{
    "MyTaskFullDelivery": {
        "recognition": "DirectHit",
        "action": "DoNothing",
        "anchor": {
            "AutoDeliveryAfterSubmitGoods": "MyTaskDeliveryDone"
        },
        "next": [
            "MyTaskOpenCurrentJobDetail"
        ]
    }
}
```

## 滑索配置

仓储和终点导航默认都以 `zip: false` 运行。需要允许滑索时，通过任务选项覆写两个固定识别节点的完整 `custom_action_param`，不修改 `AutoDelivery.next`：

```json
{
    "pipeline_override": {
        "AutoDeliveryRecognizeDepot": {
            "custom_action_param": {
                "zip": true
            }
        },
        "AutoDeliveryRecognizeDestination": {
            "custom_action_param": {
                "zip": true
            }
        }
    }
}
```

`pipeline_override` 对 `custom_action_param` 使用字段级替换，因此必须提供节点需要的完整参数。`AutoDeliveryRecognizeDepot` 与 `AutoDeliveryRecognizeDestination` 不是执行入口，但节点名属于任务选项使用的配置契约。允许滑索只表示 MapNavigator 可以在合适时选择滑索，不保证实际路线一定使用。

## 组件维护

本节面向 AutoDelivery 数据和实现维护者，普通调用方无需依赖其中的文件、路线字段或识别细节。

### 数据位置

| 路径 | 内容 |
| --- | --- |
| `tools/pipeline-generate/data/delivery_destinations.json` | zmdmap 数据 CI 自动生成并发布的仓储、终点、五语言文本、坐标和归属关系 |
| `tools/pipeline-generate/AutoDelivery/routes.json` | 自动路线无法适用时的仓储、终点和站位修正路线覆盖 |
| `assets/resource/pipeline/AutoDelivery/Routes/{RouteFileId}.json` | 由上述数据按仓储节点分组生成、可独立试跑的仓储与终点寻路节点 |
| `assets/data/AutoDelivery/catalog.json` | 运行时 OCR 匹配目录，仅保留文本、归属关系和生成节点名 |
| `assets/resource/pipeline/AutoDelivery/Common.json` | 公共入口和任务详情识别 |
| `assets/resource/pipeline/AutoDelivery/Pickup.json` | 快速传送、仓储导航和取货 |
| `assets/resource/pipeline/AutoDelivery/Delivery.json` | 取消追踪、终点导航和提交货物 |
| `agent/go-service/autodelivery/` | OCR 匹配、运行时目录校验和生成路线节点分发 |

普通仓储和资源回收站由 `tools/pipeline-generate/data/scripts/delivery_destinations_data.py` 提供游戏数据坐标与 yaw。路线生成器先在目标 yaw 正方向 8 米处生成一个 `required: true` 的 `NAVMESH` 必经点，再前往原始坐标，从而保证从交互正面接近（朝向不合适时用覆盖条目中的 `yaw` 修正，见下节）；普通收货 NPC 仍只生成原始坐标的单个 `NAVMESH` 目标。自动生成的点都带 `target_deck_y`，取值是该实体在数据源中的世界高度，接近点与终点同层、共用这个高度，用来在重叠可走面里选中目标层；人工覆盖路径的层声明按实测结果原样保留。只有断网格、分层或需要特殊站位时，才在 `routes.json` 中维护覆盖。

运行 `pnpm generate:AutoDelivery` 后，每条主路线会生成普通与允许滑索两个 `AutoDeliveryRoute...` 节点。所有仓储和送货目标还会用同一条两点接近路线生成一个不启用滑索的 retry 节点；显式 `retry_path` 可以覆盖该路线。固定的 `AutoDeliveryNavigateDepot`、`AutoDeliveryRetryNavigateDepot`、`AutoDeliveryNavigateDestination` 和 `AutoDeliveryRetryNavigateDestination` 是 `SubTask` 分发器：Go Service 只根据 OCR 结果选择生成节点名，不再把坐标或完整 `path` 注入 Pipeline。生成节点是公开的单路线测试入口，`desc` 会注明路线对应的仓储节点；它们不替代完整送货业务的唯一入口 `AutoDelivery`。

生成路线的首点由生成器统一插入 `ZONE` 区域声明（`ValleyIV_Base` / `Wuling_Base`，取自仓储 `map` 的 BaseNav 地区），作为定位器起步时的期望区域；因此 `path` / `retry_path` / `departure_path` 都不需要书写首点区域声明，路径中段为跨区路线保留的 `ZONE` 照常保留。

当前只支持**起点位于 base 图**：`delivery_destinations.json` 的 `maps[map].zone` 仅有 `map01base` / `map02base`。若将来出现位于层内或室内的仓储，需要先支持层级起点区域与层级坐标（`target_tier`）才能生成可用路线；否则按 base 地区生成的首点声明会让定位器在层内因区域不符直接失败。

覆盖文件只有顶层 `depots` 和 `destinations`：

| 数组 | 字段 | 含义 |
| --- | --- | --- |
| `depots` | `path` | 从快速传送落点前往仓储的完整 MapNavigator 路线；未配置时使用自动生成的仓储坐标 |
| `depots` | `retry_path` | 覆盖首次未识别到取货按钮时执行的自动两点站位修正路线 |
| `depots` | `departure_path` | 拼接到该仓储所属终点路线前的公共离场路线 |
| `depots` | `yaw` | 覆盖主路线接近点与自动重试路线使用的朝向角；仓储朝向墙体时使用 |
| `depots` | `offset` | 微调自动生成的仓储导航落点（底图像素偏移） |
| `depots` | `walk_only` / `zipline_only` | 覆盖该仓储主路线的滑索策略，见下节；二者互斥 |
| `destinations` | `path` | 包含最终航点的完整终点路线；未配置时使用自动生成的终点坐标 |
| `destinations` | `retry_path` | 覆盖首次未识别到提交按钮时执行的自动两点站位修正路线 |
| `destinations` | `yaw` | 覆盖回收站主路线接近点与自动重试路线使用的朝向角；NPC 主路线仍为单个终点 |
| `destinations` | `offset` | 微调自动生成的终点导航落点（底图像素偏移） |
| `destinations` | `walk_only` / `zipline_only` | 覆盖该终点主路线的滑索策略，见下节；二者互斥 |

终点目录中的 `area` 取自 `LevelDescTable.showName`，对应任务详情页实际显示的关卡名称，而不是地区建设中的仓储节点名称。普通收货任务按 `buyerName` 匹配终点；`kind` 为 `recycle_bin` 的回收站任务不显示买家名，改为匹配完整 `mission`。同一区域存在多个相同回收站文案时保持歧义失败，不静默选择可能错误的终点。

### `retry_path`

`retry_path` 使用与 `MapNavigateAction.custom_action_param.path` 相同的格式。它从主路线结束后的实际站位开始执行，只需维护路径点；首点区域声明由生成器按仓储所在 `map` 统一注入，同区声明会被归一化后重新生成，跨区声明直接报错。所有仓储和送货目标未配置时都使用自动生成的“8 米必经接近点 → 原始终点”路线，无需手工复制坐标；自动路线与主路线同源，从接近点到终点一律带 `target_deck_y`，共用实体的世界高度。接近点方位不合适时优先用下节的 `yaw` 修正，而不是改写整条 `retry_path`。这不改变 NPC 主路线仍为单个终点的规则。

只在已经确认自动两点修正不适用时配置 `retry_path`，例如断网格、分层或目标附近需要绕行。不要用它掩盖模板不稳定、页面未加载或主路线错误。

执行边界如下：

```text
前往仓储或终点
  -> 首次识别到交互按钮：继续取货或提交
  -> 首次未识别到，且目录中存在 retry 节点：修正站位一次 -> 再识别一次
  -> 没有 retry 节点，或修正后仍未识别到：结束任务
```

retry 节点不继承主路线的 `zip`，也不形成 anchor 或循环重试。一次取货或交付流程最多执行一次站位修正。

### `yaw`

`yaw` 覆盖生成自动接近点使用的朝向角（度），用于收货 NPC 实际面向墙体、或数据源缺失朝向（`yaw` 缺省为 0）导致 8 米接近点落在不可达处、retry 站位修正必然失败的场景。它只改变接近点的方位，不改变路线结构：

- 仓储：同时作用于主路线接近点与自动重试路线；
- 终点：作用于资源回收站主路线的接近点与自动重试路线。NPC 主路线按既定规则仍只生成原始坐标的单个 `NAVMESH` 点，因此 `yaw` 只影响其 retry 路线。

取值与数据源 `yaw` 同义：接近点位于实体该朝向的正方向 8 米处，0 度指向底图上方，数值增大顺时针旋转（90 度向右、180 度向下、270 度向左）；写负数或超过 360 的值会先归一化到 `[0, 360)`。

`yaw` 只在存在自动接近点时生效：条目同时覆盖 `path` 与 `retry_path` 时生成器直接报错，避免配置静默失效。已经按实测路径覆盖接近段、或需要调整接近距离时，仍应使用 `retry_path`。

### `offset`

`offset` 用底图像素偏移 `[du, dv]` 微调自动生成的导航落点（右为正 `u`、下为正 `v`），用于数据源投影出来的坐标与实际可交互位置差几米的情况：落点被高架步道等上层结构挡住、停在另一张可走面上、或交互区域在相邻几米处，导致走到了坐标却触发不了交互按钮。相比整条重写 `path` / `retry_path`，它只改坐标，路线分段与区域声明仍由生成器维护。

- 作用范围：仓储与终点的主路线终点、自动接近点、自动重试路线一起平移；接近点仍按 8 米与 `yaw` 重新计算，`target_deck_y` 不变。
- 不影响回收站的大地图图标判定坐标：`AutoDeliveryFindRecycleBin...` 在 `assets/resource/pipeline/AutoDelivery/RecycleBinCandidates.json` 中用的 `at` 仍取自数据源。
- 偏移后的落点必须仍在底图范围内，越界、非两个数值、全零偏移都会让生成器直接报错；条目同时覆盖 `path` 与 `retry_path` 时同样报错，避免配置静默失效。
- 它只能修正水平落点。落点本身正确、只是停在了错误的可走层（高度差）时，`offset` 与 `yaw` 都无效，应按实测路径覆盖 `path` / `retry_path`。

### `walk_only` / `zipline_only`

这两个字段覆盖一条主路线的滑索策略，二者互斥，同时声明时生成器直接报错：

- `walk_only: true`：完整保留录制路径，禁止全局滑索规划跳过作者路点。生成器仍保留普通节点和 `WithZipline` 节点名，但两个节点都写 `"zip": false`，即用户全局启用滑索时仍严格按作者路径步行执行。
- `zipline_only: true`：该目标只有坐滑索才到得了（如终点裴令容），没有可用的步行路线。两个节点都写 `"zip": true`，避免留下一条已知走不通的步行路线；运行时若用户选择步行送货（「送货时优先使用滑索」为关），Go 侧在 `AutoDeliveryResolveDepotAction` / `AutoDeliveryResolveDestinationAction` 分发路线前直接输出红色提示说明原因并让动作失败，不会静默退化成步行走到不可达处再超时。

注意 `zip: true` 只表示允许 MapNavigator 在合适时使用滑索；未导入滑索坐标或滑索成本不占优时，导航仍可能选择步行。`zipline_only` 拦截的是「用户明确选择步行」这种配置错误，不保证导航规划一定采用滑索。

### 验证

修改数据、识别或流程后，重新生成路线与运行时目录：

```powershell
pnpm generate:AutoDelivery
```

`pnpm check` / `pnpm test` 按需执行：改动包含 `tests/**` 时本地跑 `pnpm test`，其余情况交给 PR 的 CI 校验即可，详见[编码规范](../coding-standards.md#提交前检查)。

静态检查和节点测试不能代替游戏内验证。新增地区或修改交互界面后，仍需分别验证未取货恢复、已取货恢复、取货站位修正、NPC 交货和非 NPC 交货链路。
