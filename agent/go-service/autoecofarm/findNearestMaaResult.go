package autoecofarm

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type autoEcoFarmFindNearestMaaResultParams struct {
	RecognitionNodeName string  `json:"recognitionNodeName"`
	XRatio              float64 `json:"xRatio"`
	YRatio              float64 `json:"yRatio"`
}

type autoEcoFarmFindNearestMaaResult struct{}

func (m *autoEcoFarmFindNearestMaaResult) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {

	maafocus.NodeActionStarting(ctx, "11111")

	var params = autoEcoFarmFindNearestMaaResultParams{
		RecognitionNodeName: "",
		XRatio:              0.5,
		YRatio:              0.5,
	}

	//解析 JSON 参数到结构体中
	if arg.CustomRecognitionParam != "" {
		err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params)
		if err != nil {
			log.Error().Err(err).Msg("CustomRecognitionParam参数解析失败")
			return nil, false
		}
	}

	msg1 := fmt.Sprintf("%s,%f,%f", params.RecognitionNodeName, params.XRatio, params.YRatio)

	maafocus.NodeActionStarting(ctx, msg1)

	//调用外部识别函数并提取识别结果
	detail, _ := ctx.RunRecognition(params.RecognitionNodeName, arg.Img, nil)

	results := detail.Results.All

	//读取第一个结果为默认值
	result1, _ := results[0].AsCustom()

	minX := result1.Box.X()
	maxX := result1.Box.X()
	minY := result1.Box.Y()
	maxY := result1.Box.Y()

	//先循环算出边界
	for _, res := range results {
		// 这里可以访问每个识别结果的字段（X、Y、Confidence 等）
		resultn, _ := res.AsTemplateMatch()
		Xn := resultn.Box.X()
		Yn := resultn.Box.Y()

		if Xn < minX {
			minX = Xn
		}
		if Xn > maxX {
			maxX = Xn
		}
		if Yn < minY {
			minY = Yn
		}
		if Yn > maxY {
			minY = Yn
		}
	}

	msgn := fmt.Sprintf("边界为（%d,%d）,（%d，%d）", minX, minY, maxX, maxY)
	maafocus.NodeActionStarting(ctx, msgn)

	return nil, true

}
