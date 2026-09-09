package inventory

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册背包与仓库共用的语义化转移动作。
func Register() {
	maa.AgentServerRegisterCustomAction("InventoryTransferAllAction", &TransferAllAction{})
	maa.AgentServerRegisterCustomAction("InventoryTransferStackAction", &TransferStackAction{})
	maa.AgentServerRegisterCustomAction("InventoryTransferHalfAction", &TransferHalfAction{})
	maa.AgentServerRegisterCustomAction("InventoryTransferTouchAction", &TouchTransferAction{})
	maa.AgentServerRegisterCustomAction("InventoryDragTouchAction", &DragTouchAction{})
}
