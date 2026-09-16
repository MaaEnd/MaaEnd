package approachocr

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers ApproachOCRTargetAction.
func Register() {
	maa.AgentServerRegisterCustomAction("ApproachOCRTargetAction", &ApproachOCRTargetAction{})
}
