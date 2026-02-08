package itemTransfer

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner      = &ItemTransferCustomTarget{}
)

// Register registers all custom recognition and action components for item-transfer package
func Register() {
	maa.AgentServerRegisterCustomAction("ItemTransferCustomTarget", &ItemTransferCustomTarget{})
}
