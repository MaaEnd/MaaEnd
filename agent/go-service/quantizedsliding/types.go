package quantizedsliding

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
)

type quantizedSlidingParam struct {
	Quantity          quantityParam        `json:"Quantity"`
	QuantityFilter    *quantityFilterParam `json:"QuantityFilter"`
	GreenMask         bool                 `json:"GreenMask"`
	Direction         string               `json:"Direction"`
	IncreaseButton    any                  `json:"IncreaseButton"`
	DecreaseButton    any                  `json:"DecreaseButton"`
	CenterPointOffset any                  `json:"CenterPointOffset"`
	ClampTargetToMax  bool                 `json:"ClampTargetToMax"`
}

type quantityParam struct {
	Target  int   `json:"Target"`
	Box     []int `json:"Box"`
	OnlyRec *bool `json:"OnlyRec"`
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
//   - Quantity.Target: 目标数量
//   - Quantity.Box: OCR 识别数量的 ROI 区域 [x,y,w,h]
//   - QuantityFilter: 可选的数量 OCR 颜色过滤参数
//   - Quantity.OnlyRec: 是否为数量 OCR 节点启用 only_rec
//   - GreenMask: 顶层参数，默认值为 false，进入 TemplateMatch 协议层后映射为 green_mask，用于滑块模板和加减按钮模板匹配
//   - Direction: 滑动方向 (left/right/up/down)
//   - IncreaseButton: 增加数量按钮的模板路径或坐标
//   - DecreaseButton: 减少数量按钮的模板路径或坐标
//   - CenterPointOffset: 滑动条中心点坐标偏移量
//   - ClampTargetToMax: 为 true 时，若 Quantity.Target 超过 maxQuantity，自动将目标值钳制为 maxQuantity 并继续（默认 false 时直接失败）
type QuantizedSlidingAction struct {
	Target            int
	QuantityBox       []int
	QuantityFilter    *quantityFilterParam
	QuantityOnlyRec   bool
	GreenMask         bool
	Direction         string
	IncreaseButton    buttonTarget
	DecreaseButton    buttonTarget
	CenterPointOffset [2]int
	ClampTargetToMax  bool

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

const maxClickRepeat = 30

var defaultCenterPointOffset = [2]int{-10, 0}

var _ maa.CustomActionRunner = &QuantizedSlidingAction{}
