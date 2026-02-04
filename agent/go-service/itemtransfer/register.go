package itemtransfer

import "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {

	maa.AgentServerRegisterCustomRecognition("LocateItemInRepository", &RepoLocate{})
	maa.AgentServerRegisterCustomRecognition("LocateItemInBackpack", &BackpackLocate{})
	maa.AgentServerRegisterCustomRecognition("CheckTransferLimit", &TransferLimitChecker{})
	maa.AgentServerRegisterCustomRecognition("ConfigLoader", &ConfigLoader{})
	maa.AgentServerRegisterCustomAction("SequenceAction", &SequenceAction{})

	// maa.AgentServerRegisterCustomRecognition("LocateItemFromBackpack")
	// maa.AgentServerRegisterCustomAction("TransferItemToRepository")
}
