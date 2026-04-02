package autoecofarm

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &autoEcoFarmCalculateSwipeTarget{}
	_ maa.CustomRecognitionRunner = &autoEcoFarmFindNearestRecognitionResult{}
	_ maa.CustomActionRunner      = &autoEcoFarmResetSwipeState{}
)

func Register() {
	maa.AgentServerRegisterCustomRecognition("autoEcoFarmCalculateSwipeTarget", &autoEcoFarmCalculateSwipeTarget{})
	maa.AgentServerRegisterCustomRecognition("autoEcoFarmFindNearestRecognitionResult", &autoEcoFarmFindNearestRecognitionResult{})
	maa.AgentServerRegisterCustomAction("autoEcoFarmResetSwipeState", &autoEcoFarmResetSwipeState{})
}

type autoEcoFarmResetSwipeState struct{}

func (a *autoEcoFarmResetSwipeState) Run(_ *maa.Context, _ *maa.CustomActionArg) bool {
	ResetSwipeTargetState()
	return true
}
