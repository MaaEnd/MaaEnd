package itemtransfer

import "github.com/MaaXYZ/maa-framework-go/v3"

func Register() {

	maa.AgentServerRegisterCustomRecognition("LocateItem", &ItemLocate{})
	maa.AgentServerRegisterCustomAction("LeftClickWithCtrlDown", &LeftClickWithCtrlDown{})
	// maa.AgentServerRegisterCustomRecognition("LocateItemFromBackpack")
	// maa.AgentServerRegisterCustomAction("TransferItemToRepository")
}
