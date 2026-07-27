package bettersliding

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
)

type betterSlidingParam struct {
	TargetQuantity                int                        `json:"TargetQuantity"`
	SliderQuantity                quantityParam              `json:"SliderQuantity"`
	AvailableQuantity             quantityParam              `json:"AvailableQuantity"`
	GreenMask                     bool                       `json:"GreenMask"`
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
	FinishAfterPreciseClick       bool                       `json:"FinishAfterPreciseClick"`
	presence                      betterSlidingParamPresence `json:"-"`
}

type betterSlidingParamPresence struct {
	TargetQuantity                bool
	SliderQuantity                bool
	AvailableQuantity             bool
	GreenMask                     bool
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
	FinishAfterPreciseClick       bool
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
//   - GreenMask: map to green_mask in TemplateMatch for slider/button templates
//   - Direction: swipe direction (left/right/up/down)
//   - IncreaseButton: increase button template path or coordinates
//   - DecreaseButton: decrease button template path or coordinates
//   - CenterPointOffset: click offset from slider handle center, default [-10, 0]
//   - ClampTargetToSliderMax: clamp target to sliderMaxQuantity instead of failing (default false)
//   - FinishAfterPreciseClick: skip fine-tuning and return success after precise click (default false)
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
	GreenMask                     bool
	Direction                     string
	IncreaseButton                buttonTarget
	DecreaseButton                buttonTarget
	CenterPointOffset             [2]int
	ClampTargetToSliderMax        bool
	FinishAfterPreciseClick       bool
	SwipeButton                   string
	OutOfRangeOverrideEnable      string
	TargetReachableOverrideEnable string
	TargetQuantityType            string
	ReverseTarget                 bool
	SwipeOnlyMode                 bool
	OriginalTargetQuantity        int

	startBox                  []int
	endBox                    []int
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

// TargetQuantityType constants for canonical target quantity type values.
const (
	TargetQuantityTypeValue      = "Value"
	TargetQuantityTypePercentage = "Percentage"
)

var defaultCenterPointOffset = [2]int{-10, 0}

var _ maa.CustomActionRunner = &BetterSlidingAction{}
