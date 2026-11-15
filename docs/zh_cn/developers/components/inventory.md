# 开发手册 - Inventory 物品转移与滚动组件

Inventory 提供仓库与背包界面中的公共转移和滚动节点。每个入口执行一次操作；调用方负责进入正确界面、识别物品以及验证操作结果。

## 公共入口

| 节点 | 作用 |
| --- | --- |
| `InventoryTransferAll` | 转移源物品的全部同类物品 |
| `InventoryTransferStack` | 转移源物品的一组 |
| `InventoryTransferHalf` | 转移源物品的一半 |
| `InventoryBagScrollUpward` / `InventoryBagScrollDownward` | 背包向上 / 向下滚动一次 |
| `InventoryRepoScrollUpward` / `InventoryRepoScrollDownward` | 仓库向上 / 向下滚动一次 |

## 调用转移节点

调用前，先进入仓库与背包并排的界面并识别源物品。如果识别结果框适合点击，可以在识别节点上直接将 `InventoryTransferSource` 锚点绑定到该节点，再从后续节点调用所需的转移入口。

下面的片段省略了 `ExampleFindInventoryItem` 已有的识别配置；`ExampleVerifyInventoryTransfer` 是调用方提供的转移结果验证节点：

```json
{
    "ExampleFindInventoryItem": {
        "anchor": {
            "InventoryTransferSource": "ExampleFindInventoryItem"
        },
        "next": [
            "ExampleRunInventoryTransfer"
        ]
    },
    "ExampleRunInventoryTransfer": {
        "pre_delay": 0,
        "action": "Custom",
        "custom_action": "SubTask",
        "custom_action_param": {
            "sub": [
                "InventoryTransferAll"
            ]
        },
        "post_delay": 0,
        "rate_limit": 0,
        "next": [
            "ExampleVerifyInventoryTransfer"
        ]
    }
}
```

如果需要用 `roi_offset` 调整识别框，可以在识别节点后增加 `DirectHit` 节点，将其 `roi` 指向识别节点并将锚点绑定到它；不需要调整时无需增加此节点。锚点在节点动作执行后更新，因此不能在设置锚点的同一个动作中调用转移入口。也可以使用 `JumpBack` 调用，但调用方需要控制重复进入并在返回后验证结果。

示例中的 `SubTask` 和组件失败路径中的 `FalseAction` 由 go-service 提供，使用前需要启动项目的 Agent。

## 平台与失败处理

- Win32 使用 Ctrl、Shift 或 Alt 配合点击；macOS 使用对应的平台按键映射。
- ADB 按住源物品，再用第二触点点击对应的转移菜单按钮；按钮在物品左右两侧出现时均可识别。
- 操作失败时，组件尝试释放已按下的按键或触点并返回失败。`auto_up` 也会在框架支持时释放未配对的输入。
- 动作执行成功不代表物品已转移到目标位置；调用方应重新识别并验证结果。

## 调用滚动节点

滚动节点面向仓库与背包并排的界面，使用 1280×720 基准坐标。桌面端使用滚轮，ADB 使用滑动手势。调用方负责等待画面稳定、判断是否到达边界，以及决定是否继续滚动。
