# 开发手册 - Inventory 物品转移与滚动组件

Inventory 提供仓库与背包界面中的公共转移和滚动节点。每个入口执行一次操作；调用方负责进入正确界面、识别物品以及验证操作结果。

## 公共入口

| 名称 | 类型 | 作用 |
| --- | --- | --- |
| `InventoryTransferAll` | Custom 动作 | 转移识别框中物品的全部同类物品 |
| `InventoryTransferStack` | Custom 动作 | 转移识别框中物品的一组 |
| `InventoryTransferHalf` | Custom 动作 | 转移识别框中物品的一半 |
| `InventoryBagScrollUpward` / `InventoryBagScrollDownward` | 节点 | 背包向上 / 向下滚动一次 |
| `InventoryRepoScrollUpward` / `InventoryRepoScrollDownward` | 节点 | 仓库向上 / 向下滚动一次 |

## 调用转移动作

调用前，先进入仓库与背包并排的界面。在识别到源物品的节点上直接使用转移动作，动作会对该节点的识别框执行转移；`ExampleVerifyInventoryTransfer` 是调用方提供的转移结果验证节点：

```json
{
    "ExampleFindInventoryItem": {
        "recognition": "TemplateMatch",
        "template": "Example/Item.png",
        "action": "Custom",
        "custom_action": "InventoryTransferAll",
        "next": [
            "ExampleVerifyInventoryTransfer"
        ]
    }
}
```

转移动作由 go-service 提供，使用前需要启动项目的 Agent。

## 平台与失败处理

- Win32 按住 Ctrl、Shift 或 Alt 后点击物品；macOS 使用对应的平台按键映射。
- ADB 用第一触点按住物品，转移按钮出现后用第二触点点击，点击期间第一触点保持按下；按钮在物品左右两侧出现时均可识别。
- 操作失败时，动作尝试释放已按下的按键或触点并返回失败，调用方节点进入 `on_error`。
- 动作执行成功不代表物品已转移到目标位置；调用方应重新识别并验证结果。

## 调用滚动节点

滚动节点面向仓库与背包并排的界面，使用 1280×720 基准坐标。桌面端使用滚轮，ADB 使用滑动手势。调用方负责等待画面稳定、判断是否到达边界，以及决定是否继续滚动。
