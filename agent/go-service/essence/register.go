package essence

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 保证实现接口
var (
	_ maa.CustomRecognitionRunner = &EssenceJudgeRecognition{}
	_ maa.CustomRecognitionRunner = &EssenceTooltipJudgeRecognition{}
	_ maa.CustomActionRunner      = &EssenceApplyLockAction{}
)

// EssenceJudgeRecognition 是一个简单的自定义识别器：
// - 输入：CustomRecognitionArg.CustomRecognitionParam 中包含 s1/s2/s3
// - 输出：Detail 为 JudgeResult JSON
//
// 建议在 Pipeline 中通过 agent 节点传入参数，例如：
// {
//   "s1": "主能力提升",
//   "s2": "攻击提升",
//   "s3": "夜幕"
// }
type EssenceJudgeRecognition struct{}

type essenceJudgeInput struct {
	S1 string `json:"s1"`
	S2 string `json:"s2"`
	S3 string `json:"s3"`
}

// Register 由 main.go 调用，用于注册到 Maa AgentServer。
func Register() {
	if err := EnsureDataReady(); err != nil {
		// 这里只记录日志，不阻止注册；避免因为数据缺失直接崩溃。
		log.Warn().Err(err).Msg("essence: data not ready during Register (will retry lazily)")
	}

	maa.AgentServerRegisterCustomRecognition("EssenceJudge", &EssenceJudgeRecognition{})
	maa.AgentServerRegisterCustomRecognition("EssenceTooltipJudge", &EssenceTooltipJudgeRecognition{})
	maa.AgentServerRegisterCustomAction("EssenceApplyLockAction", &EssenceApplyLockAction{})
	log.Info().Msg("essence: registered essence custom recognition/actions")
}

// Run 实现 CustomRecognitionRunner 接口。
func (r *EssenceJudgeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	log.Info().
		Str("recognition", arg.CustomRecognitionName).
		Msg("essence: starting EssenceJudge recognition")

	var input essenceJudgeInput
	if arg.CustomRecognitionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &input); err != nil {
			log.Error().Err(err).Str("detail", arg.CustomRecognitionParam).Msg("essence: failed to parse input detail json")
			return &maa.CustomRecognitionResult{
				Box:    arg.Roi,
				Detail: `{}`,
			}, false
		}
	}

	if input.S1 == "" && input.S2 == "" && input.S3 == "" {
		log.Warn().Msg("essence: empty s1/s2/s3, treat as invalid")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	result := JudgeEssence(input.S1, input.S2, input.S3)

	data, err := json.Marshal(result)
	if err != nil {
		log.Error().Err(err).Interface("result", result).Msg("essence: failed to marshal JudgeResult")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	log.Info().
		Str("decision", result.Decision).
		Int("matchedWeaponCount", len(result.MatchedWeaponNames)).
		Int("bestDungeonCount", len(result.BestDungeonIDs)).
		Msg("essence: finished EssenceJudge recognition")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(data),
	}, true
}

