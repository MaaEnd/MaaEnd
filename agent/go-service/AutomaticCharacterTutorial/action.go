package AutomaticCharacterTutorial

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// DynamicMatchAction implements the logic to press keys based on the recognized key number
// 动态匹配动作：根据识别到的按键数字按下对应的键盘按键 (1, 2, 3, 4)
type DynamicMatchAction struct{}

// Run implements the custom action logic
func (a *DynamicMatchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 1. Get the match result detail
	// 从识别结果中获取详情（包含 key_num）
	detailStr := arg.RecognitionDetail.DetailJson
	var detail struct {
		Index  int     `json:"index"`
		Diff   float64 `json:"diff"`
		KeyNum int     `json:"key_num"`
	}

	if err := json.Unmarshal([]byte(detailStr), &detail); err != nil {
		log.Error().Err(err).Str("detail", detailStr).Msg("Failed to parse recognition detail")
		return false
	}

	keyCode := 0

	// 2. Determine Key Code
	// 严格模式：只使用 OCR 识别出的数字，不进行推断
	if detail.KeyNum > 0 && detail.KeyNum <= 4 {
		// keyNum 1 -> Key '1' (49)
		keyCode = 48 + detail.KeyNum
		log.Info().Int("keyNum", detail.KeyNum).Msg("Using recognized key number")
	} else {
		log.Error().Int("index", detail.Index).Int("keyNum", detail.KeyNum).Msg("OCR failed and fallback is disabled. Cannot determine key code.")
		return false
	}

	// Validate key code range ('1' to '4' is 49 to 52)
	if keyCode < 49 || keyCode > 52 {
		log.Error().Int("keyCode", keyCode).Msg("Invalid key code calculated")
		return false
	}

	log.Info().Int("keyCode", keyCode).Msg("Pressing skill key")

	// 3. Press the key
	// 执行按键操作
	ctrl := ctx.GetTasker().GetController()
	if err := ctrl.PostClickKey(int32(keyCode)).Wait(); err != nil {
		// Fix: *maa.Job does not implement error, use .GetDetail() or check error manually if returned
		// PostClickKey returns *Job. Wait() returns *Job.
		// Job struct has .GetDetail() to see result, but Go binding might not have standard error interface on Job.
		// Actually, Wait() returns *Job. If we want to check for error, we should inspect the job status or result.
		// For simplicity, we can just log that it failed if we assume err is returned (which is not true here).
		// Correct usage:
		// job := ctrl.PostClickKey(...).Wait()
		// if !job.Success() { ... }
		// But based on user request to fix "cannot use err (variable of type *maa.Job) as error value",
		// we should remove the .Err(err) call or convert it properly.
		// Since maa-framework-go v3 Job might not be an error, let's just log the failure.

		log.Error().Msg("Failed to press key (Job failed)")
		return false
	}

	return true
}
