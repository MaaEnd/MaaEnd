package bettersliding

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
)

type betterSlidingParam struct {
	TargetQuantity                int                        `json:"TargetQuantity"`
	SliderQuantity                quantityParam              `json:"SliderQuantity"`
	AvailableQuantity             quantityParam              `json:"AvailableQuantity"`
	Direction                     string                     `json:"Direction"`
	IncreaseButton                any                        `json:"IncreaseButton"`
	DecreaseButton                any                        `json:"DecreaseButton"`
	SwipeButton                   string                     `json:"SwipeButton"`
	OutOfRangeOverrideEnable      string                     `json:"OutOfRangeOverrideEnable"`
	TargetReachableOverrideEnable string                     `json:"TargetReachableOverrideEnable"`
	TargetQuantityType            string                     `json:"TargetQuantityType"`
	ReverseTarget                 bool                       `json:"ReverseTarget"`
	CenterPointOffset             any                        `json:"CenterPointOffset"`
	ClampTargetToSliderMax        bool                       `json:"ClampTargetToSliderMax"`
	FineTuneQuantity              any                        `json:"FineTuneQuantity"`
	FineTuneFallback              string                     `json:"FineTuneFallback"`
	ResetBeforeFindStart          bool                       `json:"ResetBeforeFindStart"`
	presence                      betterSlidingParamPresence `json:"-"`
}

type betterSlidingParamPresence struct {
	TargetQuantity                bool
	SliderQuantity                bool
	AvailableQuantity             bool
	Direction                     bool
	IncreaseButton                bool
	DecreaseButton                bool
	SwipeButton                   bool
	OutOfRangeOverrideEnable      bool
	TargetReachableOverrideEnable bool
	TargetQuantityType            bool
	ReverseTarget                 bool
	CenterPointOffset             bool
	ClampTargetToSliderMax        bool
	FineTuneQuantity              bool
	FineTuneFallback              bool
	ResetBeforeFindStart          bool
}

type quantityParam struct {
	Box     []int                `json:"Box"`
	Filter  *quantityFilterParam `json:"Filter"`
	OnlyRec *bool                `json:"OnlyRec"`
}

// quantityFilterParam 定义数量 OCR 预处理使用的单组颜色阈值。
type quantityFilterParam struct {
	Lower  []int `json:"lower"`
	Upper  []int `json:"upper"`
	Method int   `json:"method"`
}

// BetterSlidingAction handles slider-based quantity selection UIs.
// It recognizes slider endpoints, computes a proportional click position from
// the target quantity, and fine-tunes via increase/decrease buttons.
//
// Parameter fields:
//   - TargetQuantity: target quantity (overridden by attach.TargetQuantity when present)
//   - SliderQuantity.Box: OCR ROI [x,y,w,h] for reading the current slider quantity.
//   - AvailableQuantity.Box: OCR ROI [x,y,w,h] for reading the total available quantity.
//     When provided, BetterSlidingGetAvailableQuantity runs after SwipeToMax and its OCR result is
//     used for ReverseTarget / TargetQuantityType calculation.
//     When AvailableQuantity is not provided, target resolution falls back to the
//     BetterSlidingGetSliderMaxQuantity runtime value (slider endpoint).
//   - SliderQuantity.Filter: optional color filter for slider quantity OCR
//   - SliderQuantity.OnlyRec: enable only_rec for the slider quantity OCR node
//   - AvailableQuantity.Filter: optional color filter for available quantity OCR
//   - AvailableQuantity.OnlyRec: enable only_rec for available quantity OCR
//   - Direction: swipe direction (left/right/up/down)
//   - IncreaseButton: increase button template path or coordinates
//   - DecreaseButton: decrease button template path or coordinates
//   - CenterPointOffset: click offset from slider handle center, default [-10, 0]
//   - ClampTargetToSliderMax: clamp target to sliderMaxQuantity instead of failing (default false)
//   - FineTuneQuantity: bool or int. true (default) always fine-tunes via Increase/Decrease after
//     BetterSlidingCheckQuantity; false never fine-tunes; int N (>= 1) fine-tunes only when
//     abs(current - target) <= N. See "不微调语义" in docs/**/better-sliding.md.
//   - FineTuneFallback: none (default) / more / less. Only takes effect when this run decides not
//     to fine-tune: none finishes via the quantity re-check; more/less nudges the precise click
//     target by 1px steps along a single axis and re-checks. See "不微调语义" in the docs.
//   - ResetBeforeFindStart: swipe toward the minimum before matching the slider start position,
//     so the recorded start position is the minimum value (default false)
//   - SwipeButton: custom slider template path overriding BetterSlidingSwipeButton
//   - OutOfRangeOverrideEnable: Pipeline node name to enable when target is out of range
//   - TargetReachableOverrideEnable: Pipeline node name to enable when the resolved target can be
//     reached without clamping. The caller must still confirm that its outer operation succeeded.
//   - TargetQuantityType: TargetQuantityTypeValue (default) or TargetQuantityTypePercentage
//   - ReverseTarget: reverse target calculation
type BetterSlidingAction struct {
	TargetQuantity                int
	SliderQuantityBox             []int
	AvailableQuantityBox          []int
	AvailableQuantityExplicit     bool
	SliderQuantityFilter          *quantityFilterParam
	AvailableQuantityFilter       *quantityFilterParam
	SliderQuantityOnlyRec         bool
	AvailableQuantityOnlyRec      bool
	Direction                     string
	IncreaseButton                buttonTarget
	DecreaseButton                buttonTarget
	CenterPointOffset             [2]int
	ClampTargetToSliderMax        bool
	FineTuneQuantity              fineTuneQuantity
	FineTuneFallback              string
	ResetBeforeFindStart          bool
	SwipeButton                   string
	OutOfRangeOverrideEnable      string
	TargetReachableOverrideEnable string
	TargetQuantityType            string
	ReverseTarget                 bool
	SwipeOnlyMode                 bool
	OriginalTargetQuantity        int

	startBox                  []int
	endBox                    []int
	preciseClickBase          [2]int
	preciseClickNudges        int
	sliderMaxQuantity         int
	availableQuantity         int
	availableQuantityResolved bool
	outOfRange                bool
	targetReachable           bool
	runtimeTargetResolved     bool
	logger                    zerolog.Logger
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

// fineTuneQuantity 是 FineTuneQuantity 归一化后的载体。
// thresholdMode 为 true 时表示 int 阈值语义（threshold >= 1），否则为布尔语义（enabled）。
type fineTuneQuantity struct {
	thresholdMode bool
	enabled       bool
	threshold     int
}

// defaultFineTuneQuantity 对应「未提供 FineTuneQuantity」时的默认行为：始终微调。
var defaultFineTuneQuantity = fineTuneQuantity{enabled: true}

// FineTuneFallback 的规范取值（大小写不敏感地接受，归一化后统一为小写）。
const (
	// FineTuneFallbackNone 表示不微调时不偏移，复检后直接收尾。
	FineTuneFallbackNone = "none"
	// FineTuneFallbackMore 表示不微调时朝 End 方向做单轴 1px 累加偏移后复查。
	FineTuneFallbackMore = "more"
	// FineTuneFallbackLess 表示不微调时朝 Start 方向做单轴 1px 累加偏移后复查。
	FineTuneFallbackLess = "less"
)

// nudgeAxis 表示不微调时单轴 1px 累加偏移所选的轴。
type nudgeAxis uint8

const (
	// nudgeAxisX 表示偏移作用在 x 分量上。
	nudgeAxisX nudgeAxis = iota
	// nudgeAxisY 表示偏移作用在 y 分量上（也是平局与重合时的兜底选择）。
	nudgeAxisY
)

// String 返回轴的日志标签（"x" / "y"）。
func (a nudgeAxis) String() string {
	if a == nudgeAxisX {
		return "x"
	}

	return "y"
}

// reset2Side 表示 BetterSlidingReset2 的复位方向，由精确点击基准坐标在
// Start → End 轴上的相对位置决定：靠近 Start 时朝 End 滑动，靠近 End 时朝 Start 滑动。
type reset2Side uint8

const (
	// reset2SideTowardStart 表示向最小侧（Start）滑动复位，终点取 buildResetSwipeEnd。
	reset2SideTowardStart reset2Side = iota
	// reset2SideTowardEnd 表示向最大侧（End）滑动复位，终点取 buildSwipeEnd。
	reset2SideTowardEnd
)

// String 返回复位方向的日志标签（"start" / "end"）。
func (s reset2Side) String() string {
	if s == reset2SideTowardEnd {
		return "end"
	}

	return "start"
}

// modeLabel 返回 fineTuneQuantity 语义标签，仅用于日志。
func (q fineTuneQuantity) modeLabel() string {
	if q.thresholdMode {
		return "threshold"
	}

	return "bool"
}

// TargetQuantityType constants for canonical target quantity type values.
const (
	TargetQuantityTypeValue      = "Value"
	TargetQuantityTypePercentage = "Percentage"
)

var defaultCenterPointOffset = [2]int{-10, 0}

// defaultGreenMask 是 BetterSliding 按钮模板匹配默认启用的绿色掩码开关。
// BetterSliding 对 SwipeButton / IncreaseButton / DecreaseButton 的模板匹配固定开启绿色掩码。
const defaultGreenMask = true

var _ maa.CustomActionRunner = &BetterSlidingAction{}
