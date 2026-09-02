package aerosalvage

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const aeroSalvageBalloonStateRecognitionName = "AeroSalvageBalloonStateRecognition"

var _ maa.CustomRecognitionRunner = &BalloonStateRecognition{}

// BalloonStateRecognition reads the remaining count of every balloon slot involved in
// the placement plan. It hits only when all count digits are readable; the observed
// tuple is cached for ConfigureSwipeAction.
type BalloonStateRecognition struct{}

// Run recognizes the balloon state from the count digits of all plan slots.
func (r *BalloonStateRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", aeroSalvageBalloonStateRecognitionName).Msg("custom recognition arg or image is nil")
		return nil, false
	}
	if len(balloonPlanSlots) == 0 {
		log.Warn().Str("component", aeroSalvageBalloonStateRecognitionName).Msg("no balloon plan slots to recognize")
		return nil, false
	}
	counts := make([]int, len(balloonPlanSlots))
	for i, node := range balloonPlanSlots {
		count, found, err := recognizeBalloonNumber(ctx, arg.Img, node)
		if err != nil {
			log.Warn().Err(err).Str("component", aeroSalvageBalloonStateRecognitionName).Str("step", "recognize balloon state").Msg("recognition failed")
			return nil, false
		}
		if !found {
			// 本识别的失败定义：OCR 读不到数字。
			log.Warn().Str("component", aeroSalvageBalloonStateRecognitionName).Str("count_node", node).Msg("balloon count digit is unreadable")
			return nil, false
		}
		counts[i] = count
	}
	balloonObservedState = counts
	log.Debug().
		Str("component", aeroSalvageBalloonStateRecognitionName).
		Ints("balloon_state", counts).
		Msg("balloon state recognized")
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}
