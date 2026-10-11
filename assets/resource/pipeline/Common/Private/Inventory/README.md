# Inventory 公共 Pipeline

| 入口 | 桌面操作 | ADB 操作 |
| --- | --- | --- |
| `InventoryTransferAll` | Ctrl + 点击 | 长按源格，第二触点点击全部转移图标 |
| `InventoryTransferStack` | Shift + 点击 | 长按源格，第二触点点击转移一组图标 |
| `InventoryTransferHalf` | Alt + 点击 | 长按源格，第二触点点击转移一半图标 |
| `InventoryMoveRepoItemToBag` | 拖动到目标格 | 长按确认菜单后，保持源触点移到目标格 |

三个数量转移入口共用 `InventoryTransferSource` 锚点。调用方应先执行一个 `DirectHit` 节点，以 `roi` 引用已找到的物品格、用 `roi_offset` 收缩可点击区域，并将锚点绑定到该节点，再通过 `SubTask` 调用公共入口。锚点在动作执行后更新，因此不要在执行 `SubTask` 的同一个节点才设置它。

拖动入口使用 `InventoryMoveSource` 与 `InventoryMoveDestination` 两个锚点。调用方应在同一帧 `And` 识别源格和目标格，并把两个锚点指向对应子节点；当前框架缓存 `And` 子识别结果，可直接用于 `Swipe`／`TouchMove` 的目标。公共节点负责输入，不负责选格、分页或仓库导航。

桌面端沿用修饰键点击的输入时序。ADB 源触点持续按住，左右菜单使用同一轮截图；第二触点保持 100ms 后释放，再保持源触点 100ms 后释放。拖动到目标格后保持 200ms。上述等待是原触屏输入时序，菜单出现仍由模板识别驱动。

正常动作失败、菜单等待超时会走显式清理；清理后调用已有 `FalseAction` 返回失败，防止把输入失败当成搬运成功。`KeyDown`／`TouchDown` 设置 `auto_up: true`，在支持该字段的框架中，任务取消或结束时也会释放未配对输入。

`InventoryBagScrollUpward`、`InventoryBagScrollDownward`、`InventoryRepoScrollUpward`、`InventoryRepoScrollDownward` 提供一次滚动。桌面使用滚轮，ADB 纵向滑动后横移并保持以消除惯性；调用方负责画面静止和滚动条边界判断。

ADB 菜单模板使用左 ROI `[150,175,100,370]`、右 ROI `[1030,175,100,370]`，阈值 0.85。macOS 键码由 `pnpm generate:MacOS` 生成。
