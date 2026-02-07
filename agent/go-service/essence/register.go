package essence

import "github.com/MaaXYZ/maa-framework-go/v4"

// Ensure interface compliance at compile time.
// 编译期保证接口实现。
var (
	_ maa.CustomRecognitionRunner = &EssenceJudgeRecognition{}
	_ maa.CustomRecognitionRunner = &EssenceTooltipJudgeRecognition{}
	_ maa.CustomActionRunner      = &EssenceScanGridAction{}
)

// Register is called from main.go to register custom components.
// Register 在 main.go 中调用，用于注册组件。
func Register() {
	if err := EnsureDataReady(); err != nil {
		// 只记录日志，不阻止注册；避免因为数据缺失直接崩溃。
		// Log only, do not block registration; avoid crash on missing data.
		essLog.Warn().Err(err).Msg("data not ready during Register (will retry lazily)")
	}

	maa.AgentServerRegisterCustomRecognition("EssenceJudge", &EssenceJudgeRecognition{})
	maa.AgentServerRegisterCustomRecognition("EssenceTooltipJudge", &EssenceTooltipJudgeRecognition{})
	maa.AgentServerRegisterCustomAction("EssenceScanGridAction", &EssenceScanGridAction{})
	essLog.Info().Msg("registered custom recognition/actions")
}
