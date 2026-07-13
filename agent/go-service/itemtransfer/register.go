package itemtransfer

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func Register() {
	maa.AgentServerRegisterCustomRecognition(
		"ItemTransferCacheLowConfidenceRecognition",
		&ItemTransferCacheLowConfidenceRecognition{},
	)
	maa.AgentServerRegisterCustomAction(
		"ItemTransferFallbackAction",
		&ItemTransferFallbackAction{},
	)
	maa.AgentServerRegisterCustomAction(
		"ItemTransferOCRAction",
		&ItemTransferOCRAction{},
	)
	maa.AgentServerRegisterCustomAction(
		"ItemTransferCacheVerifiedAction",
		&ItemTransferCacheVerifiedAction{},
	)
}
