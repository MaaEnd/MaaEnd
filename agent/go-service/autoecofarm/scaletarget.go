package autoecofarm

import (
	_ "embed"
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 定义获取参数的字段

type autoEcoFarmCalculateSwipeTargetParams struct {
	//NodeName   string  `json:"node_name"`
	XStepRatio float64 `json:"xStepRatio"`
	YStepRatio float64 `json:"yStepRatio"`
}

type autoEcoFarmCalculateSwipeTarget struct{}

// 根据目标的坐标区域和设定的拉近比例，计算出swipe用的end坐标，用来实现将视角拉近目标区域一定比例
func (m *autoEcoFarmCalculateSwipeTarget) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	//计算常用参数,计算过程中同一用float64，最后转回int

	screenCenterX := float64(arg.Img.Bounds().Dx()) / 2
	screenCenterY := float64(arg.Img.Bounds().Dy()) / 2

	log.Info().Msgf("截图中心坐标：%f, %f", screenCenterX, screenCenterY)
	//读取参数
	var params = autoEcoFarmCalculateSwipeTargetParams{
		//NodeName:   string(""),
		XStepRatio: 0.5,
		YStepRatio: 0.5,
	}

	//解析 JSON 参数到结构体中
	if arg.CustomRecognitionParam != "" {
		err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params)
		if err != nil {
			log.Error().Err(err).Msg("CustomRecognitionParam参数解析失败")
			return nil, false
		}
	}

	oTragetX := float64(arg.Roi.X())      // 传入矩形左上角X
	oTragetY := float64(arg.Roi.Y())      // 传入矩形左上角Y
	oTragetW := float64(arg.Roi.Width())  // 传入矩形宽度（X轴方向）
	oTragetH := float64(arg.Roi.Height()) // 传入矩形高度（Y轴方向）

	log.Info().Msgf(
		"Roi矩形参数：左上角X=%.2f, 左上角Y=%.2f, 宽度=%.2f, 高度=%.2f",
		oTragetX, oTragetY, oTragetW, oTragetH,
	)

	// 计算传入矩形的中点坐标
	oTragetCenterX := oTragetX + oTragetW/2 // 中点X = 左上角X + 宽度/2
	oTragetCenterY := oTragetY + oTragetH/2 // 中点Y = 左上角Y + 高度/2

	//  计算屏幕中心坐标

	//计算距离
	dx := oTragetCenterX - screenCenterX
	dy := oTragetCenterY - screenCenterY

	//计算传出坐标,将目标向屏幕中心平移一段距离

	targetX := int(screenCenterX + dx*params.XStepRatio)
	targetY := int(screenCenterY + dy*params.YStepRatio)
	targetW := 1
	targetH := 1

	targetbox := maa.Rect{targetX, targetY, targetW, targetH}

	results := &maa.CustomRecognitionResult{
		Box:    targetbox,
		Detail: "",
	}

	return results, true

}
