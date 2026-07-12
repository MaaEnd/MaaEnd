package autostockstaple

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers AutoStockStaple custom actions.
func Register() {
	maa.AgentServerRegisterCustomAction(autoStockStapleQuantityActionName, &QuantityControlAction{})
}
