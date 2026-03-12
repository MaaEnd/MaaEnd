package autoecofarm

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &autoEcoFarmCalculateSwipeTarget{}
	_ maa.CustomRecognitionRunner = &autoEcoFarmFindNearestMaaResult{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	maa.AgentServerRegisterCustomRecognition("autoEcoFarmCalculateSwipeTarget", &autoEcoFarmCalculateSwipeTarget{})
	maa.AgentServerRegisterCustomRecognition("autoEcoFarmFindNearestMaaResult", &autoEcoFarmFindNearestMaaResult{})
}
