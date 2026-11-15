package autoalt

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("AutoAltClickAction", &AutoAltClickAction{})
	maa.AgentServerRegisterCustomAction("AutoAltSwipeAction", &AutoAltSwipeAction{})
	maa.AgentServerRegisterCustomAction("AutoCtrlClickAction", &AutoCtrlClickAction{})
	maa.AgentServerRegisterCustomAction("AutoSwipeClickAction", &AutoSwipeClickAction{})
	maa.AgentServerRegisterCustomAction("InventoryTransferAll", newInventoryTransferAction("InventoryTransferAll", "All"))
	maa.AgentServerRegisterCustomAction("InventoryTransferStack", newInventoryTransferAction("InventoryTransferStack", "Stack"))
	maa.AgentServerRegisterCustomAction("InventoryTransferHalf", newInventoryTransferAction("InventoryTransferHalf", "Half"))
}
