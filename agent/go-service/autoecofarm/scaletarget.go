package autoecofarm

import (
	_ "embed"
	"encoding/json"
	"math"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	autoEcoFarmStepRatioDecay = 0.9
	autoEcoFarmStepRatioMin   = 0.1
)

type autoEcoFarmCalculateSwipeTargetParams struct {
	XStepRatio float64 `json:"xStepRatio"`
	YStepRatio float64 `json:"yStepRatio"`
}

type autoEcoFarmCalculateSwipeTarget struct{}

// 根据目标的坐标区域和设定的拉近比例，计算出swipe用的end坐标，用来实现将视角拉近目标区域一定比例
func (m *autoEcoFarmCalculateSwipeTarget) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {

	screenCenterX := float64(arg.Img.Bounds().Dx()) / 2
	screenCenterY := float64(arg.Img.Bounds().Dy()) / 2

	log.Debug().
		Str("component", "AutoEcoFarm").
		Float64("screen_center_x", screenCenterX).
		Float64("screen_center_y", screenCenterY).
		Msg("screenshot center")

	var params = autoEcoFarmCalculateSwipeTargetParams{
		XStepRatio: 0.5,
		YStepRatio: 0.5,
	}

	if arg.CustomRecognitionParam != "" {
		err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params)
		if err != nil {
			log.Error().Err(err).Str("component", "AutoEcoFarm").Msg("CustomRecognitionParam parse failed")
			return nil, false
		}
	}

	oTargetX := float64(arg.Roi.X())
	oTargetY := float64(arg.Roi.Y())
	oTargetW := float64(arg.Roi.Width())
	oTargetH := float64(arg.Roi.Height())
	oTargetCenterX := oTargetX + oTargetW/2
	oTargetCenterY := oTargetY + oTargetH/2

	if lastState := getLastState(); lastState != nil {
		params.XStepRatio = lastState.xStepRatio
		params.YStepRatio = lastState.yStepRatio
		crossedCenter := false

		lastTargetCenterX := float64(lastState.lastRoi.X()) + float64(lastState.lastRoi.Width())/2
		lastTargetCenterY := float64(lastState.lastRoi.Y()) + float64(lastState.lastRoi.Height())/2

		lastDx := lastTargetCenterX - screenCenterX
		lastDy := lastTargetCenterY - screenCenterY
		currDx := oTargetCenterX - screenCenterX
		currDy := oTargetCenterY - screenCenterY

		if lastDx != 0 && currDx != 0 && lastDx*currDx < 0 {
			params.XStepRatio = math.Max(params.XStepRatio*autoEcoFarmStepRatioDecay, autoEcoFarmStepRatioMin)
			log.Debug().
				Str("component", "AutoEcoFarm").
				Str("axis", "X").
				Float64("step_ratio", params.XStepRatio).
				Msg("crossed center, ratio decayed")
			crossedCenter = true
		}
		if lastDy != 0 && currDy != 0 && lastDy*currDy < 0 {
			params.YStepRatio = math.Max(params.YStepRatio*autoEcoFarmStepRatioDecay, autoEcoFarmStepRatioMin)
			log.Debug().
				Str("component", "AutoEcoFarm").
				Str("axis", "Y").
				Float64("step_ratio", params.YStepRatio).
				Msg("crossed center, ratio decayed")
			crossedCenter = true
		}

		if crossedCenter {
			maafocus.Print(ctx, i18n.T("autoecofarm.crossed_center"))
		}
	}

	log.Debug().
		Str("component", "AutoEcoFarm").
		Float64("x", oTargetX).Float64("y", oTargetY).
		Float64("w", oTargetW).Float64("h", oTargetH).
		Msg("ROI rect")

	params.XStepRatio = math.Max(autoEcoFarmStepRatioMin, math.Min(params.XStepRatio, 1.0))
	params.YStepRatio = math.Max(autoEcoFarmStepRatioMin, math.Min(params.YStepRatio, 1.0))

	dx := oTargetCenterX - screenCenterX
	dy := oTargetCenterY - screenCenterY

	targetX := int(screenCenterX + dx*params.XStepRatio)
	targetY := int(screenCenterY + dy*params.YStepRatio)
	targetW := 1
	targetH := 1

	targetbox := maa.Rect{targetX, targetY, targetW, targetH}

	results := &maa.CustomRecognitionResult{
		Box:    targetbox,
		Detail: "",
	}
	setLastState(arg.Roi, params.XStepRatio, params.YStepRatio)
	return results, true
}
