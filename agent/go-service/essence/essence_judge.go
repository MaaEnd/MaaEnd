package essence

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

// EssenceJudgeRecognition 是一个简单的自定义识别器：
// - 输入：CustomRecognitionArg.CustomRecognitionParam 中包含 s1/s2/s3
// - 输出：Detail 为 JudgeResult JSON
//
// 建议在 Pipeline 中通过 agent 节点传入参数，例如：
//
//	{
//	  "s1": "主能力提升",
//	  "s2": "攻击提升",
//	  "s3": "夜幕"
//	}
type EssenceJudgeRecognition struct{}

type essenceJudgeInput struct {
	S1 string `json:"s1"`
	S2 string `json:"s2"`
	S3 string `json:"s3"`
}

// Run 实现 CustomRecognitionRunner 接口。
// Run implements CustomRecognitionRunner.
func (r *EssenceJudgeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	essLog.Info().
		Str("recognition", arg.CustomRecognitionName).
		Msg("starting EssenceJudge recognition")

	var input essenceJudgeInput
	if arg.CustomRecognitionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &input); err != nil {
			essLog.Error().Err(err).Str("detail", arg.CustomRecognitionParam).Msg("failed to parse input detail json")
			return &maa.CustomRecognitionResult{
				Box:    arg.Roi,
				Detail: `{}`,
			}, false
		}
	}

	if input.S1 == "" && input.S2 == "" && input.S3 == "" {
		essLog.Warn().Msg("empty s1/s2/s3, treat as invalid")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	result := JudgeEssence(input.S1, input.S2, input.S3)

	data, err := json.Marshal(result)
	if err != nil {
		essLog.Error().Err(err).Interface("result", result).Msg("failed to marshal JudgeResult")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{}`,
		}, false
	}

	essLog.Info().
		Str("decision", result.Decision).
		Int("matchedWeaponCount", len(result.MatchedWeaponNames)).
		Msg("finished EssenceJudge recognition")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(data),
	}, true
}
