package AutomaticCharacterTutorial

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// UltimateSkillAction implements logic to long press the ultimate key
// 终结技动作：根据识别到的按键数字长按对应的键盘按键
type UltimateSkillAction struct{}

// Run implements the custom action logic
func (a *UltimateSkillAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 1. Get detail
	detailStr := arg.RecognitionDetail.DetailJson
	var detail struct {
		Index  int `json:"index"`
		KeyNum int `json:"key_num"`
	}

	if err := json.Unmarshal([]byte(detailStr), &detail); err != nil {
		log.Error().Err(err).Msg("Failed to parse ult action detail")
		return false
	}

	keyCode := 0

	// 2. Determine Key Code
	if detail.KeyNum > 0 && detail.KeyNum <= 4 {
		keyCode = 48 + detail.KeyNum
	} else {
		// Fallback to index
		keyCode = 49 + detail.Index
		log.Warn().Int("index", detail.Index).Msg("OCR failed for ult, using index fallback")
	}

	if keyCode < 49 || keyCode > 52 {
		log.Error().Int("keyCode", keyCode).Msg("Invalid key code for ult")
		return false
	}

	log.Info().Int("keyCode", keyCode).Msg("Long pressing ultimate skill key")

	// 3. Long Press the key (approx 300ms)
	ctrl := ctx.GetTasker().GetController()

	// Press Down
	if err := ctrl.PostKeyDown(int32(keyCode)).Wait(); err != nil {
		log.Error().Msg("Failed to press down key (Job failed)")
		return false
	}

	// Hold
	time.Sleep(300 * time.Millisecond)

	// Release
	if err := ctrl.PostKeyUp(int32(keyCode)).Wait(); err != nil {
		log.Error().Msg("Failed to release key (Job failed)")
		return false
	}

	return true
}
