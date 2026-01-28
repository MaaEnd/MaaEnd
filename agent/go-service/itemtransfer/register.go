package itemtransfer

import "github.com/MaaXYZ/maa-framework-go/v3"

func Register() {

	maa.AgentServerRegisterCustomRecognition("LocateItemFromRepository", &RepoLocate{})
	maa.AgentServerRegisterCustomAction("LeftClickWithCtrlDown", &LeftClickWithCtrlDown{})
	// maa.AgentServerRegisterCustomRecognition("LocateItemFromBackpack")
	// maa.AgentServerRegisterCustomAction("TransferItemToRepository")
}
