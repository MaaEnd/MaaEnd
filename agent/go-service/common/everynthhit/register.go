package everynthhit

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the EveryNthHit recognition.
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognitionName, &EveryNthHit{})
}
