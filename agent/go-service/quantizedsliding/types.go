package quantizedsliding

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
)

type quantizedSlidingParam struct {
	Target            int                  `json:"Target"`
	QuantityBox       []int                `json:"QuantityBox"`
	QuantityFilter    *quantityFilterParam `json:"QuantityFilter"`
	Direction         string               `json:"Direction"`
	IncreaseButton    any                  `json:"IncreaseButton"`
	DecreaseButton    any                  `json:"DecreaseButton"`
	CenterPointOffset any                  `json:"CenterPointOffset"`
}

// quantityFilterParam 定义数量 OCR 预处理使用的单组颜色阈值。
type quantityFilterParam struct {
	Lower  []int `json:"lower"`
	Upper  []int `json:"upper"`
	Method int   `json:"method"`
}

// QuantizedSlidingAction 实现量化滑动选择功能,用于处理游戏中需要通过滑动选择数量的 UI 场景。
// 该动作会自动识别滑动条的起点和终点位置,根据目标数量精确计算点击位置,
// 并通过微调按钮进行最终调整以达到目标值。
//
// 参数说明:
//   - Target: 目标数量
//   - QuantityBox: OCR 识别数量的 ROI 区域 [x,y,w,h]
//   - QuantityFilter: 可选的数量 OCR 颜色过滤参数
//   - Direction: 滑动方向 (left/right/up/down)
//   - IncreaseButton: 增加数量按钮的坐标
//   - DecreaseButton: 减少数量按钮的坐标
type QuantizedSlidingAction struct {
	Target            int
	QuantityBox       []int
	QuantityFilter    *quantityFilterParam
	Direction         string
	IncreaseButton    buttonTarget
	DecreaseButton    buttonTarget
	CenterPointOffset [2]int

	startBox    []int
	endBox      []int
	maxQuantity int
	logger      zerolog.Logger
}

type buttonTarget struct {
	coordinates []int
	template    string
}

func (b buttonTarget) logValue() any {
	if b.template != "" {
		return b.template
	}

	return append([]int(nil), b.coordinates...)
}

var quantizedSlidingActionNodes = []string{
	"QuantizedSlidingMain",
	"QuantizedSlidingFindStart",
	"QuantizedSlidingGetMaxQuantity",
	"QuantizedSlidingFindEnd",
	"QuantizedSlidingCheckQuantity",
	"QuantizedSlidingDone",
}

const maxClickRepeat = 30

var defaultCenterPointOffset = [2]int{-10, 0}

var _ maa.CustomActionRunner = &QuantizedSlidingAction{}
