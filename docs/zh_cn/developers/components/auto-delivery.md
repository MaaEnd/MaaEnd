# AutoDelivery 通用送货组件

AutoDelivery 封装运送委托接取后的公共流程：打开任务位置、快速传送、前往仓储节点取货、识别送货终点、导航至交付点并提交货物。

组件只处理送货业务，不负责抢单、装箱或决定从哪个界面打开当前任务详情。调用方应先完成自己的前置业务，并提供可重复调用的“打开当前任务详情”子任务。

## 目录

| 路径 | 职责 |
| ----------------------------------------------- | ----------------------------------------------- |
| `assets/resource/pipeline/AutoDelivery/` | 取货、目的地识别、导航、提交和失败处理流程 |
| `assets/data/AutoDelivery/` | 送货点目录及人工校准后的 MapNavigator 路线 |
| `assets/resource/image/AutoDelivery/` | 携带状态、取货、提交货物和任务地图模板 |
| `agent/go-service/autodelivery/` | OCR 文本匹配、目的地解析和导航参数注入 |

Pipeline 负责界面流程；Go Action `AutoDeliveryResolveDestinationAction` 只负责把 OCR 结果解析成唯一送货点，并完整覆写 `AutoDeliveryNavigate` 与 `AutoDeliveryRetryNavigate` 的 `MapNavigateAction` 参数。

## 调用契约

完整送货入口是 `AutoDeliveryResolveDestinationBeforeTeleportEntry`，调用前必须已经打开当前任务详情。

流程中需要多次重新打开任务详情。调用方必须覆写 `AutoDeliveryOpenCurrentJobDetail.custom_action_param`，提供一个成功时停在任务详情界面、失败时返回失败的子任务：

```json
{
    "AutoDeliveryOpenCurrentJobDetail": {
        "custom_action_param": {
            "sub": [
                "MyTaskOpenCurrentDeliveryJob"
            ],
            "continue": false,
            "strict": true
        }
    }
}
```

调用入口可设置以下锚点：

| 锚点 | 触发时机 |
| ----------------------------------------- | -------------------------------- |
| `AutoDeliveryAfterQuickTeleport` | 快速传送落地后；留空则继续判断取货区域 |
| `AutoDeliveryAfterWalkToDepot` | 走到取货仓储节点后；留空则继续接取货物 |
| `AutoDeliveryAfterDelivery` | 成功提交并关闭奖励界面后 |
| `AutoDeliveryAfterDeliveryFallback` | 送达回调因 `max_hit` 等原因不可执行时 |

只需要完整送货时，前两个锚点留空即可。抢委托的“仅传送”和“传送并走到仓储点”模式分别利用前两个锚点提前结束。

建议使用严格 `SubTask` 调用完整流程，并在 `on_error` 中处理失败：

```json
{
    "MyTaskRunAutoDelivery": {
        "recognition": "DirectHit",
        "action": "Custom",
        "custom_action": "SubTask",
        "custom_action_param": {
            "sub": [
                "AutoDeliveryResolveDestinationBeforeTeleportEntry"
            ],
            "continue": false,
            "strict": true
        },
        "next": [
            "MyTaskDeliveryDone"
        ],
        "on_error": [
            "MyTaskDeliveryFailed"
        ]
    }
}
```

AutoDelivery 的致命错误节点统一通过 `FalseAction` 返回失败，调用方不应通过覆写内部错误节点来改变流程。

## 维护目的地

- `delivery_destinations.json` 由 `tools/pipeline-generate/data/scripts/delivery_destinations_data.py` 从游戏数据与 BaseNav 元数据生成。
- `destinations.json` 保存业务 ID、区域公共前缀、送货点独有接续路线、坐标覆盖和 `target_deck_y`。
- Go 解析器先按地区缩小候选，再对目的地文本做容错匹配；匹配不唯一或低于阈值时直接失败，不猜测终点。
- 导航仍由 `MapNavigateAction` 执行。纯移动优先使用 `NAVMESH`，复杂路段按 [MapNavigator 文档](./map-navigator.md) 配置必要航点。

## 调用方边界

- `SeizeDeliveryJobs` 只负责筛选和接取委托，并从运送委托列表打开任务详情。
- `DeliveryJobs` 只负责仓储装箱、接单，并从对应本地仓储节点点击“查看任务”。
- 新调用方不得引用 `__AutoDelivery*` 私有节点，也不应覆写 `AutoDeliveryNavigate` 等内部流程节点。
