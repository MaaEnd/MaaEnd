# 开发手册 - AutoDelivery 送货组件

AutoDelivery 是任务无关的自动送货组件。它为调用方提供一个统一执行入口：`AutoDelivery`。

调用方触发打开正确的当前送货任务详情后，直接把 `AutoDelivery` 放入 `next`。组件会先等待区域文本和任务操作按钮出现，确认详情页加载完成，再识别黄色送货目标；识别到时说明已经取货，若任务仍在追踪则先取消追踪，随后返回大世界并前往终点。没有黄色目标时则识别当前仓储节点，快速传送后进入任务界面取消追踪，再返回大世界前往仓储取货。取货成功后，组件会重新进入任务界面，在左侧列表中选择“送货任务”；当前页未找到时最多向下滑动三次，确认右侧详情已经切换，再识别送货终点。

```text
当前送货任务详情
  -> AutoDelivery
       -> 有黄色送货目标：确认取消追踪 -> 返回大世界 -> 前往终点 -> 提交货物
       -> 无黄色送货目标：快速传送 -> 进入任务界面取消追踪 -> 返回大世界
            -> 前往仓储 -> 取货 -> 进入任务界面 -> 选择并确认“送货任务”
            -> AutoDelivery -> 识别并前往终点 -> 提交货物
```

其余 `AutoDelivery...` 节点组成组件的内部流程，但不使用额外的 `__` 前缀区分。调用方不应把识别、传送、寻路、取货或提交节点直接写入 `next`。

## 调用入口

```json
{
    "MyTaskOpenCurrentJobDetail": {
        "next": [
            "AutoDelivery"
        ]
    }
}
```

`AutoDelivery` 首次调用的前提是调用方已经触发打开正确任务的详情页。组件会自行等待详情页加载完成，但不负责从任务列表或本地仓储节点打开首次详情，因为不同任务进入详情页的方式不同。取货后的第二次详情切换由组件统一处理：通过 `SceneEnterMenuMission` 进入任务界面，OCR 左侧列表中的完整任务名“送货任务”，当前页未找到时最多向下滑动三次，点击后再用右侧详情标题独立确认切换成功。每次取货后开始查找前都会清空滑动节点的命中计数，因此连续多单各自拥有三次滑动额度。

入口按以下顺序识别：

1. 区域文本和已追踪/开始追踪按钮同时出现，确认当前任务详情已加载；
2. 任务条件区域存在黄色文本时，OCR 送货目标并注入终点路线；
3. 否则 OCR 区域名称，解析所属仓储节点并注入仓储路线；
4. 页面或两项业务信息无法识别时按 Pipeline 默认行为结束任务。

识别送货目标优先于仓储节点，因此调用方不需要使用 `CarryingGoods.png` 判断是否携带货物。
入口的识别顺序属于固定流程，不通过 anchor 配置：

```json
{
    "next": [
        "AutoDeliveryRecognizeDestination",
        "AutoDeliveryRecognizeDepot"
    ]
}
```

## 默认流程与回调 anchor

AutoDelivery 对有明确通用下一步的阶段提供默认节点。写法统一为 anchor 优先、默认实现节点兜底：

```json
{
    "next": [
        "[Anchor]AutoDeliveryAfterRecognizeDestination",
        "AutoDeliveryCancelCurrentJobTracking",
        "AutoDeliveryCurrentJobTrackingAlreadyOff"
    ]
}
```

调用方未设置 anchor 时会继续默认流程；设置后则可以在该阶段结束任务或插入调用方自己的处理。

| anchor | 默认行为 | 常见用途 |
| ---------------------------------------- | -------------------- | ------------------------------ |
| `AutoDeliveryAfterQuickTeleport` | 进入任务界面取消追踪后前往仓储节点 | “仅快速传送”在此结束 |
| `AutoDeliveryAfterNavigateDepot` | 取货 | “走到仓储节点”在此结束 |
| `AutoDeliveryAfterRecognizeDestination` | 确认取消追踪后返回大世界并前往终点 | 禁止送货阶段继续移动 |
| `AutoDeliveryAfterSubmitGoods` | 正常结束组件 | 进入调用方的完成节点或下一轮任务 |

所有 `AutoDeliveryAfter*` 回调都有默认值，不需要额外处理时无需设置。AutoDelivery 不提供额外的 `on_error` 转发；识别或操作失败时按 Pipeline 默认行为结束任务。

完整链路只需要任务特有的提交后出口；取货后重新选择送货任务由组件内部完成：

```json
{
    "MyTaskFullDelivery": {
        "recognition": "DirectHit",
        "action": "DoNothing",
        "anchor": {
            "AutoDeliveryAfterSubmitGoods": "MyTaskDeliveryDone"
        },
        "next": [
            "MyTaskOpenCurrentJob"
        ]
    }
}
```

## 滑索配置

`AutoDelivery` 固定调用两个识别节点，默认都以 `zip: false` 注入寻路参数。滑索选项只覆写这两个节点的动作参数，不改变入口 `next`：

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

`pipeline_override` 对 `custom_action_param` 是字段级替换，因此选项需要提供节点所需的完整参数。这两个节点不作为独立执行入口，但节点名属于任务选项使用的配置契约。允许滑索只会把 `zip: true` 传给首次仓储寻路和终点寻路；仓储附近的 `retry_path` 是局部站位修正，不继承该参数。

## SeizeDeliveryJobs 的三种模式

SeizeDeliveryJobs 的三个后处理模式都在打开任务详情后进入同一个 `AutoDelivery`，区别只由回调 anchor 表达：

- 仅快速传送：`AutoDeliveryAfterQuickTeleport` 进入成功节点；若已经取货，则通过 `AutoDeliveryAfterRecognizeDestination` 提示送货阶段无法传送。
- 走到仓储节点：仓储分支在 `AutoDeliveryAfterNavigateDepot` 结束；若已经取货，则通过 `AutoDeliveryAfterRecognizeDestination` 提示送货阶段无法传送，不会改为前往送货终点。
- 全自动送货：只配置 `AutoDeliveryAfterSubmitGoods` 返回抢单主循环；取货后由 AutoDelivery 进入任务界面、选择“送货任务”并继续终点识别。

## DeliveryJobs 接入

DeliveryJobs 在单击“查看任务”后进入仓储适配节点，再由 `AutoDelivery` 统一等待详情页加载。每个仓储节点的适配器只配置：

- `AutoDeliveryAfterSubmitGoods`：直接返回对应地区循环。

接取新任务后直接使用当前详情页。只有恢复此前已有任务时，才返回对应本地仓储节点重新打开详情；取货后则直接进入任务界面，选择“送货任务”并确认右侧详情，再继续识别终点，不经过地区建设中的运送委托列表。

普通转交不使用 AutoDelivery 的整页就绪判断；单击“查看任务”后直接进入 `DeliveryJobsClickTransferJob`，由该节点等待并识别“转交委托”按钮。

## `retry_path`

仓储主路线和取货重试路线都保存在 AutoDelivery 数据中。首次到达仓储后若没有识别到取货按钮，组件会在存在 `retry_path` 时执行一次内部局部修正路线，再识别一次按钮。

```text
前往仓储节点
  -> 找到取货按钮：取货并确认携带状态
  -> 未找到且存在 retry_path：局部修正一次 -> 再识别取货按钮
  -> 仍未找到：结束任务
```

`retry_path` 不暴露为独立调用入口，不使用 anchor，也不使用 `max_hit` 循环。没有 `retry_path` 的仓储不会执行伪造的默认重试路线；重试后仍无法取货时直接结束任务。

## 数据位置

| 路径 | 内容 |
| --------------------------------------------------------- | -------------------------------------------- |
| `assets/data/AutoDelivery/delivery_destinations.json` | 自动生成的仓储、终点、五语言文本、坐标和归属关系 |
| `assets/data/AutoDelivery/overrides.json` | 特殊主路线、`retry_path`、终点分段路线和楼层覆盖 |
| `assets/resource/pipeline/AutoDelivery/Common.json` | 统一调用入口和任务详情识别 |
| `assets/resource/pipeline/AutoDelivery/Pickup.json` | 快速传送、仓储寻路和取货 |
| `assets/resource/pipeline/AutoDelivery/Delivery.json` | 返回大世界、终点寻路和提交货物 |
| `agent/go-service/autodelivery/` | OCR 匹配、数据校验和寻路节点参数注入 |

普通仓储和终点直接使用自动目录中的坐标生成单个 `NAVMESH` 航点。只有断网格、分层、需要分段靠近或需要取货位置修正时，才在 `overrides.json` 中维护覆盖。

终点目录中的 `area` 取自 `LevelDescTable.showName`，对应任务详情页实际显示的关卡名称，而不是地区建设中的仓储节点名称。普通收货任务从完整目标文案中匹配 `buyerName`；`kind` 为 `recycle_bin` 的回收站任务不显示 `buyerName`，改为匹配完整 `mission`。同一区域存在多个相同回收站文案时保持歧义失败，不任意选择终点。

覆盖文件只有顶层 `depots` 和 `destinations`：

| 数组 | 可覆盖字段 |
| -------------- | -------------------------------------------------------------------- |
| `depots` | `path`、`retry_path`、`destination_path_prefix` |
| `destinations` | `path`、`target_override`、`target_deck_y` |

## 接入检查

1. 确认调用方能触发打开正确的当前任务详情，随后直接进入 `AutoDelivery`，页面加载由组件自行等待。
2. 需要提交后继续调用方任务时绑定 `AutoDeliveryAfterSubmitGoods`；取货后继续送货不需要额外 anchor。
3. 仅在需要截断默认流程或继续调用方流程时配置阶段回调。
4. 调用方只把 `AutoDelivery` 作为执行入口，不在 `next` 中直接引用其他 `AutoDelivery...` 中间节点。
5. 滑索选项只覆写两个固定识别节点的完整 `custom_action_param`，不修改 `AutoDelivery.next`。
6. 不在调用方保存仓储或终点坐标，不使用 `CarryingGoods.png` 判断流程阶段。
7. 新地区先更新自动目录数据，特殊路线再补 `overrides.json`。
8. 修改任务列表选择时同时验证 Win32/ADB 的“送货任务”列表项和右侧详情标题；修改 Go 后运行 `go test ./autodelivery`，修改 DeliveryJobs 后重新生成并运行生成器测试。
9. 最后运行 `pnpm format`、`pnpm format:go`、`pnpm check` 和 `pnpm test`。

静态检查和节点测试不能代替游戏内实机验证。新增地区或修改交互界面后，仍需分别验证未取货恢复、已取货恢复、取货重试、NPC 交货和非 NPC 交货链路。
