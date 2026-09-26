package bettersliding

import (
	"math"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
)

// betterSlidingParam 是 Pipeline custom_action_param 的原始解析载体。
//
// 识别参数类字段统一为 String | Object：
//   - String：节点引用，取该节点 recognition.param 作为识别参数补丁；
//   - Object：识别参数补丁，内容即 recognition.param 的键值。
//
// IncreaseButton / DecreaseButton 额外接受 int[2] | int[4] 坐标。
// 补丁只写 recognition.param，绝不替换 recognition 类型。
type betterSlidingParam struct {
	TargetQuantity                int                        `json:"TargetQuantity"`
	SliderQuantity                any                        `json:"SliderQuantity"`
	SliderQuantityFilter          any                        `json:"SliderQuantityFilter"`
	AvailableQuantity             any                        `json:"AvailableQuantity"`
	AvailableQuantityFilter       any                        `json:"AvailableQuantityFilter"`
	Direction                     string                     `json:"Direction"`
	IncreaseButton                any                        `json:"IncreaseButton"`
	DecreaseButton                any                        `json:"DecreaseButton"`
	SwipeButton                   any                        `json:"SwipeButton"`
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
	SliderQuantityFilter          bool
	AvailableQuantity             bool
	AvailableQuantityFilter       bool
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

// BetterSlidingAction handles slider-based quantity selection UIs.
// It recognizes slider endpoints, computes a proportional click position from
// the target quantity, and fine-tunes via increase/decrease buttons.
//
// Parameter fields:
//   - TargetQuantity: target quantity (overridden by attach.TargetQuantity when present)
//   - SliderQuantity: OCR recognition param patch for the current slider quantity,
//     applied to BetterSlidingGetSliderQuantity (node reference string or patch object).
//   - SliderQuantityFilter: ColorMatch param patch written to
//     BetterSlidingSliderQuantityFilter and linked into the quantity patch via color_filter.
//   - AvailableQuantity: OCR recognition param patch for the total available quantity,
//     applied to BetterSlidingGetAvailableQuantity and enabling that node.
//     When provided, its OCR result is used for ReverseTarget / TargetQuantityType calculation.
//     When AvailableQuantity is not provided, target resolution falls back to the
//     BetterSlidingGetSliderMaxQuantity runtime value (slider endpoint).
//   - AvailableQuantityFilter: ColorMatch param patch written to
//     BetterSlidingAvailableQuantityFilter and linked into the available quantity patch.
//   - Direction: swipe direction (left/right/up/down)
//   - IncreaseButton: increase button coordinates, node reference, or param patch
//   - DecreaseButton: decrease button coordinates, node reference, or param patch
//   - CenterPointOffset: click offset from slider handle center, default [-10, 0]
//   - ClampTargetToSliderMax: clamp target to sliderMaxQuantity instead of failing (default false)
//   - FineTuneQuantity: bool or int >= 1; true (default) always fine-tunes via
//     Increase/Decrease, false never, int N only when abs(current - target) <= N.
//     Integers above maxFineTuneThreshold saturate to "always fine-tune".
//   - FineTuneFallback: none (default) / more / less; only used when this run decides
//     not to fine-tune, more/less nudges the precise click by 1px steps on one axis.
//   - ResetBeforeFindStart: swipe toward the minimum before matching the slider start position,
//     so the recorded start position is the minimum value (default false)
//   - SwipeButton: slider template recognition param patch overriding BetterSlidingSwipeButton
//   - OutOfRangeOverrideEnable: Pipeline node name to enable when target is out of range
//   - TargetReachableOverrideEnable: Pipeline node name to enable when the resolved target can be
//     reached without clamping. The caller must still confirm that its outer operation succeeded.
//   - TargetQuantityType: TargetQuantityTypeValue (default) or TargetQuantityTypePercentage
//   - ReverseTarget: reverse target calculation
type BetterSlidingAction struct {
	TargetQuantity                int
	AvailableQuantityExplicit     bool
	Direction                     string
	IncreaseButton                buttonTarget
	DecreaseButton                buttonTarget
	CenterPointOffset             [2]int
	ClampTargetToSliderMax        bool
	FineTuneQuantity              fineTuneQuantity
	FineTuneFallback              string
	ResetBeforeFindStart          bool
	OutOfRangeOverrideEnable      string
	TargetReachableOverrideEnable string
	TargetQuantityType            string
	ReverseTarget                 bool
	SwipeOnlyMode                 bool
	OriginalTargetQuantity        int

	// 识别参数补丁。patch 为空表示该参数未配置；FilterNode 非空表示该 Filter 已配置，
	// 需要把内建 Filter 节点名写进对应 Quantity 补丁的 color_filter。
	swipeButtonPatch             map[string]any
	sliderQuantityPatch          map[string]any
	sliderQuantityFilterNode     string
	sliderQuantityFilterPatch    map[string]any
	availableQuantityPatch       map[string]any
	availableQuantityFilterNode  string
	availableQuantityFilterPatch map[string]any

	startBox []int
	endBox   []int
	// preciseClickBase 精确点击基准坐标；preciseClickNudges 为已偏移次数，仅作日志索引。
	preciseClickBase          [2]int
	preciseClickNudges        int
	sliderMaxQuantity         int
	availableQuantity         int
	availableQuantityResolved bool
	outOfRange                bool
	targetReachable           bool
	// minimumTargetShortCircuit 表示本次走「目标即最小值 1」的短路路径：
	// 跳过滑条端点识别与精确点击，ResetBeforeFindStart 时仅执行复位滑动后直接收尾。
	minimumTargetShortCircuit bool
	runtimeTargetResolved     bool
	logger                    zerolog.Logger
}

// buttonTarget 是 IncreaseButton / DecreaseButton 归一化后的载体：
// coordinates 与 patch 二选一。coordinates 非空表示直接点击坐标；
// patch 非空表示按钮模板识别补丁（经 And 包装成 TemplateMatch 后点击命中框）。
type buttonTarget struct {
	coordinates []int
	patch       map[string]any
}

func (b buttonTarget) logValue() any {
	if len(b.patch) > 0 {
		return b.patch
	}

	return append([]int(nil), b.coordinates...)
}

const maxClickRepeat = 30

// maxFineTuneThreshold 是 FineTuneQuantity 整数阈值的饱和边界。
//
// 取 math.MaxInt32：远大于任何真实数量差值，且能被 float64 精确表示。阈值超过该值时
// 不再执行 int(v)，而是饱和为「始终微调」（见 newThresholdFineTuneQuantity）——旧实现
// 钳到 float64(math.MaxInt)（= 2^63）再转换会回绕成 math.MinInt，把「超大阈值」
// 反转成「从不微调」。
const maxFineTuneThreshold = math.MaxInt32

// fineTuneQuantity 是 FineTuneQuantity 归一化后的载体（语义见 normalizeFineTuneQuantity）。
type fineTuneQuantity struct {
	thresholdMode bool
	enabled       bool
	threshold     int
}

// defaultFineTuneQuantity 对应「未提供 FineTuneQuantity」时的默认行为：始终微调。
var defaultFineTuneQuantity = fineTuneQuantity{enabled: true}

// FineTuneFallback 的规范取值（大小写不敏感地接受，归一化后统一为小写）。
const (
	// FineTuneFallbackNone 不偏移，复检后收尾。
	FineTuneFallbackNone = "none"
	// FineTuneFallbackMore 朝 End 方向做单轴 1px 累加偏移后复查。
	FineTuneFallbackMore = "more"
	// FineTuneFallbackLess 朝 Start 方向做单轴 1px 累加偏移后复查。
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
// BetterSliding 对 SwipeButton / IncreaseButton / DecreaseButton 的模板匹配默认开启绿色掩码，
// 但可被参数补丁里的 green_mask 显式覆盖。
const defaultGreenMask = true

var _ maa.CustomActionRunner = &BetterSlidingAction{}
