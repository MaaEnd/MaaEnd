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

组件按任务详情中的区域、任务条件和操作按钮自行判断阶段。取货后重新打开详情、在任务列表中找到送货任务以及确认详情切换均由组件完成，调用方无需为第二次详情切换配置额外入口或 anchor。

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
| `tools/pipeline-generate/AutoDelivery/routes.json` | 特殊仓储路线、取货站位修正和终点完整路线 |
| `assets/resource/pipeline/AutoDelivery/Routes.json` | 由上述数据生成、可独立试跑的仓储与终点寻路节点 |
| `assets/data/AutoDelivery/catalog.json` | 运行时 OCR 匹配目录，仅保留文本、归属关系和生成节点名 |
| `assets/resource/pipeline/AutoDelivery/Common.json` | 公共入口和任务详情识别 |
| `assets/resource/pipeline/AutoDelivery/Pickup.json` | 快速传送、仓储导航和取货 |
| `assets/resource/pipeline/AutoDelivery/Delivery.json` | 取消追踪、终点导航和提交货物 |
| `agent/go-service/autodelivery/` | OCR 匹配、运行时目录校验和生成路线节点分发 |

普通仓储和终点由 `tools/pipeline-generate/data/scripts/delivery_destinations_data.py` 从游戏数据生成单个 `NAVMESH` 目标。只有断网格、分层、需要分段靠近或需要修正取货站位时，才在 `routes.json` 中维护覆盖。

运行 `pnpm generate:AutoDelivery` 后，每条主路线会生成普通与允许滑索两个 `AutoDeliveryRoute...` 节点，`retry_path` 则生成一个不继承滑索选项的站位修正节点。固定的 `AutoDeliveryNavigateDepot`、`AutoDeliveryRetryNavigateDepot` 和 `AutoDeliveryNavigateDestination` 是 `SubTask` 分发器：Go Service 只根据 OCR 结果选择生成节点名，不再把坐标或完整 `path` 注入 Pipeline。生成节点是公开的单路线测试入口，`desc` 会注明路线对应的仓储节点；它们不替代完整送货业务的唯一入口 `AutoDelivery`。

覆盖文件只有顶层 `depots` 和 `destinations`：

| 数组 | 字段 | 含义 |
| --- | --- | --- |
| `depots` | `path` | 从快速传送落点前往仓储的完整 MapNavigator 路线；未配置时使用自动生成的仓储坐标 |
| `depots` | `retry_path` | 首次未识别到取货按钮时执行一次的局部站位修正路线 |
| `depots` | `departure_path` | 拼接到该仓储所属终点路线前的公共离场路线 |
| `destinations` | `path` | 包含最终航点的完整终点路线；未配置时使用自动生成的终点坐标 |

终点目录中的 `area` 取自 `LevelDescTable.showName`，对应任务详情页实际显示的关卡名称，而不是地区建设中的仓储节点名称。普通收货任务按 `buyerName` 匹配终点；`kind` 为 `recycle_bin` 的回收站任务不显示买家名，改为匹配完整 `mission`。同一区域存在多个相同回收站文案时保持歧义失败，不静默选择可能错误的终点。

### `retry_path`

`retry_path` 使用与 `MapNavigateAction.custom_action_param.path` 相同的格式。它从仓储主路线结束后的实际站位开始执行，应包含独立运行所需的区域声明和路径点。

只在已经确认以下根因时配置 `retry_path`：仓储主路线可以正确到达，但 MapNavigator 的默认到达范围可能让角色停在取货交互范围外。不要用它掩盖模板不稳定、页面未加载或主路线错误。

执行边界如下：

```text
前往仓储
  -> 首次识别到取货按钮：直接取货
  -> 首次未识别到，且配置了 retry_path：修正站位一次 -> 再识别一次
  -> 没有 retry_path，或修正后仍未识别到：结束任务
```

`retry_path` 不继承仓储主路线的 `zip`，也不暴露为执行入口、anchor 或循环重试节点。一次 AutoDelivery 取货流程最多执行一次站位修正。

### 验证

修改数据、识别或流程后，先重新生成路线与运行时目录，再按改动范围运行：

```powershell
pnpm generate:AutoDelivery

cd agent/go-service
go test ./autodelivery

cd ../..
node --test tools/pipeline-generate/AutoDelivery/*.test.mjs tools/pipeline-generate/DeliveryJobs/*.test.mjs
pnpm check
pnpm test
```

静态检查和节点测试不能代替游戏内验证。新增地区或修改交互界面后，仍需分别验证未取货恢复、已取货恢复、取货站位修正、NPC 交货和非 NPC 交货链路。
