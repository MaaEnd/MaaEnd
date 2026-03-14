package autoecofarm

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &autoEcoFarmCalculateSwipeTarget{}
	_ maa.CustomRecognitionRunner = &autoEcoFarmFindNearestRecognitionResult{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	maa.AgentServerRegisterCustomRecognition("autoEcoFarmCalculateSwipeTarget", &autoEcoFarmCalculateSwipeTarget{})
	maa.AgentServerRegisterCustomRecognition("autoEcoFarmFindNearestRecognitionResult", &autoEcoFarmFindNearestRecognitionResult{})
}
