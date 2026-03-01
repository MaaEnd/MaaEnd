package resell

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// quotaRecoResult 配额识别结果，通过 CustomRecognitionResult.Detail 传给 Action
type quotaRecoResult struct {
	X int `json:"x"`
	Y int `json:"y"`
	B int `json:"b"`
}

// ResellCheckQuotaRecognition 执行配额 OCR，将解析结果通过 Detail 传给后续 Action（使用 pipeline 传入的 arg.Img）
var _ maa.CustomRecognitionRunner = &ResellCheckQuotaRecognition{}

type ResellCheckQuotaRecognition struct{}

func (r *ResellCheckQuotaRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	log.Info().Msg("[Resell]检查配额溢出状态…")
	if arg.Img == nil {
		log.Error().Msg("[Resell]pipeline 传入的截图为空")
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: `{"x":-1,"y":-1,"b":-1}`,
		}, true
	}

	x, y, _, b := ocrAndParseQuota(ctx, arg.Img)
	if x < 0 || y <= 0 || b < 0 {
		log.Info().Msg("[Resell]未能解析配额或未找到，按正常流程继续")
	}
	result := quotaRecoResult{X: x, Y: y, B: b}
	detailJSON, _ := json.Marshal(result)
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}
