package everynthhit

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const recognitionName = "EveryNthHit"

var _ maa.CustomRecognitionRunner = &EveryNthHit{}

// recognitionParam 是 EveryNthHit 的 custom_recognition_param。
type recognitionParam struct {
	// Key 是计数器名字，不同 key 之间互不影响。
	Key string `json:"key"`
	// Every 是每评估多少次命中一次，必须是正整数。
	Every int `json:"every"`
}

// EveryNthHit 每被评估 every 次命中一次，用于「第 N 次走到该节点才继续」的场景。
type EveryNthHit struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *EveryNthHit) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().
			Str("component", recognitionName).
			Msg("got nil custom recognition arg")
		return nil, false
	}

	var param recognitionParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", recognitionName).
			Str("param", arg.CustomRecognitionParam).
			Msg("failed to parse custom_recognition_param")
		return nil, false
	}
	if param.Key == "" {
		log.Error().
			Str("component", recognitionName).
			Str("param", arg.CustomRecognitionParam).
			Msg("custom_recognition_param.key is required")
		return nil, false
	}
	if param.Every <= 0 {
		log.Warn().
			Str("component", recognitionName).
			Str("key", param.Key).
			Int("every", param.Every).
			Msg("custom_recognition_param.every must be a positive integer")
		return nil, false
	}

	if !Advance(param.Key, param.Every) {
		return nil, false
	}

	log.Debug().
		Str("component", recognitionName).
		Str("key", param.Key).
		Int("every", param.Every).
		Msg("every-nth hit reached")
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}
