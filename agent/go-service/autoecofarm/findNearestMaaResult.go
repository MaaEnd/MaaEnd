package autoecofarm

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const debugmode = false

type autoEcoFarmFindNearestRecognitionResultParams struct {
	RecognitionNodeName string  `json:"recognitionNodeName"`
	XRatio              float64 `json:"xRatio"`
	YRatio              float64 `json:"yRatio"`
}

type autoEcoFarmFindNearestRecognitionResult struct{}

// 这个函数提供一个模板识别函数名，然后返回所有识别结果中离某一百分比位置最近的那个，比如XRatio=0.5，YRatio=0.5，就是离中心最近
func (m *autoEcoFarmFindNearestRecognitionResult) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {

	if debugmode {
		maafocus.NodeActionStarting(ctx, "函数正常开始")
	}

	var params = autoEcoFarmFindNearestRecognitionResultParams{
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

	if params.RecognitionNodeName == "" || params.XRatio < 0 || params.XRatio > 1 || params.YRatio < 0 || params.YRatio > 1 {
		maafocus.NodeActionStarting(ctx, "非法参数")
		return nil, false
	}

	if debugmode {
		msg1 := fmt.Sprintf("传入参数确认,节点名:“%s”,x比例:%.2f,y比例:%.2f", params.RecognitionNodeName, params.XRatio, params.YRatio)

		maafocus.NodeActionStarting(ctx, msg1)
	}
	//调用外部识别函数并提取识别结果
	detail, _ := ctx.RunRecognition(params.RecognitionNodeName, arg.Img, nil)

	recdetails := detail.DetailJson
	if debugmode {
		maafocus.NodeActionStarting(ctx, "下面是json详细内容：")
		maafocus.NodeActionStarting(ctx, recdetails)
	}
	results := detail.Results.Filtered

	if len(results) == 0 {
		maafocus.NodeActionStarting(ctx, "未识别到结果")
		return nil, false
	}

	var minX int
	var maxX int
	var minY int
	var maxY int

	//读取第一个结果为默认值
	result1, _ := results[0].AsTemplateMatch()

	minX = result1.Box.X()
	maxX = result1.Box.X()
	minY = result1.Box.Y()
	maxY = result1.Box.Y()
	if debugmode {
		msg2 := fmt.Sprintf("初始边界为（%d,%d）,（%d，%d）", minX, minY, maxX, maxY)
		maafocus.NodeActionStarting(ctx, msg2)
	}
	//先循环算出边界
	for idx, res := range results {
		// 这里可以访问每个识别结果的字段（X、Y、Confidence 等）
		resultn, _ := res.AsTemplateMatch()
		Xn := resultn.Box.X()
		Yn := resultn.Box.Y()
		if debugmode {
			msgn := fmt.Sprintf("第%d个点（%d,%d）", idx+1, Xn, Yn)
			maafocus.NodeActionStarting(ctx, msgn)
		}
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
			maxY = Yn
		}
	}
	if debugmode {
		msg3 := fmt.Sprintf("边界为（%d,%d）,（%d，%d）", minX, minY, maxX, maxY)
		maafocus.NodeActionStarting(ctx, msg3)
	}

	//计算目标点

	fminX := float64(minX)
	fminY := float64(minY)
	fmaxX := float64(maxX)
	fmaxY := float64(maxY)

	targetX := fminX + (fmaxX-fminX)*params.XRatio
	targetY := fminY + (fmaxY-fminY)*params.YRatio

	if debugmode {
		msg4 := fmt.Sprintf("目标点为（%.2f,%.2f）", targetX, targetY)
		maafocus.NodeActionStarting(ctx, msg4)
	}

	//初始化最小值为第一个值
	realutX := result1.Box.X()
	realutY := result1.Box.Y()
	realutW := result1.Box.Width()
	realutH := result1.Box.Height()

	realutcXn := realutX + realutW/2
	realutcYn := realutY + realutH/2

	mindistance2 := (float64(realutcXn)-targetX)*(float64(realutcXn)-targetX) + (float64(realutcYn)-targetY)*(float64(realutcYn)-targetY)

	//遍历所有结果，返回中心里目标欧几里得距离平方最小的结果
	for idx, res := range results {
		// 这里可以访问每个识别结果的字段（X、Y、Confidence 等）
		resultn, _ := res.AsTemplateMatch()
		fXn := float64(resultn.Box.X())
		fYn := float64(resultn.Box.Y())
		fWn := float64(resultn.Box.Width())
		fHn := float64(resultn.Box.Height())

		fcXn := fXn + fWn/2
		fcYn := fYn + fHn/2

		distance2 := (fcXn-targetX)*(fcXn-targetX) + (fcYn-targetY)*(fcYn-targetY)

		if distance2 < mindistance2 {
			mindistance2 = distance2
			realutX = int(fXn)
			realutY = int(fYn)
			realutW = int(fWn)
			realutH = int(fHn)
		}

		if debugmode {
			msgn2 := fmt.Sprintf("第%d个点[%.2f,%.2f,%.2f,%.2f],欧几里得距离平方是%.2f,目前最小距离是%.2f,对应坐标[%d,%d,%d,%d]", idx+1, fXn, fYn, fWn, fHn, distance2, mindistance2, realutX, realutY, realutW, realutH)
			maafocus.NodeActionStarting(ctx, msgn2)
		}
	}

	if debugmode {
		msg3 := fmt.Sprintf("最终的最小距离是%.2f,对应坐标[%d,%d,%d,%d]", mindistance2, realutX, realutY, realutW, realutH)
		maafocus.NodeActionStarting(ctx, msg3)
	}

	targetbox := maa.Rect{realutX, realutY, realutW, realutH}
	finalresult := &maa.CustomRecognitionResult{
		Box:    targetbox,
		Detail: "",
	}

	return finalresult, true

}
