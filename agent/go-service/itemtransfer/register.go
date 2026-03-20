package itemtransfer

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	_ maa.CustomActionRunner = &ItemTransferFallbackAction{}
)

func Register() {
	maa.AgentServerRegisterCustomAction(
		"ItemTransferFallbackAction",
		&ItemTransferFallbackAction{},
	)
}
