package itemtransfer

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func Register() {
	maa.AgentServerRegisterCustomAction(
		"ItemTransferOCRFallbackAction",
		&ItemTransferOCRFallbackAction{},
	)
	maa.AgentServerRegisterCustomAction(
		"ItemTransferOCRAction",
		&ItemTransferOCRAction{},
	)
	maa.AgentServerRegisterCustomRecognition(
		"ItemTransferCompetitiveMatch",
		&CompetitiveRecognition{},
	)
}
