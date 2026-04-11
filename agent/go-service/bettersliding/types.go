package bettersliding

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
)

type betterSlidingParam struct {
	Target                  int                        `json:"Target"`
	Quantity                quantityParam              `json:"Quantity"`
	QuantityFilter          *quantityFilterParam       `json:"QuantityFilter"`
	GreenMask               bool                       `json:"GreenMask"`
	Direction               string                     `json:"Direction"`
	IncreaseButton          any                        `json:"IncreaseButton"`
	DecreaseButton          any                        `json:"DecreaseButton"`
	SwipeButton             string                     `json:"SwipeButton"`
	ExceedingOverrideEnable string                     `json:"ExceedingOverrideEnable"`
	TargetType              string                     `json:"TargetType"`
	TargetReverse           bool                       `json:"TargetReverse"`
	CenterPointOffset       any                        `json:"CenterPointOffset"`
	ClampTargetToMax        bool                       `json:"ClampTargetToMax"`
	FinishAfterPreciseClick bool                       `json:"FinishAfterPreciseClick"`
	presence                betterSlidingParamPresence `json:"-"`
}

type betterSlidingParamPresence struct {
	Target                  bool
	Quantity                bool
	QuantityFilter          bool
	GreenMask               bool
	Direction               bool
	IncreaseButton          bool
	DecreaseButton          bool
	SwipeButton             bool
	ExceedingOverrideEnable bool
	TargetType              bool
	TargetReverse           bool
	CenterPointOffset       bool
	ClampTargetToMax        bool
	FinishAfterPreciseClick bool
}

type quantityParam struct {
	Box     []int `json:"Box"`
	OnlyRec *bool `json:"OnlyRec"`
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
//   - Target: target quantity (overridden by attach.Target when present)
//   - Quantity.Box: OCR ROI [x,y,w,h] for reading the quantity
//   - QuantityFilter: optional color filter for quantity OCR
//   - Quantity.OnlyRec: enable only_rec for the quantity OCR node
//   - GreenMask: map to green_mask in TemplateMatch for slider/button templates
//   - Direction: swipe direction (left/right/up/down)
//   - IncreaseButton: increase button template path or coordinates
//   - DecreaseButton: decrease button template path or coordinates
//   - CenterPointOffset: click offset from slider handle center, default [-10, 0]
//   - ClampTargetToMax: clamp target to maxQuantity instead of failing (default false)
//   - FinishAfterPreciseClick: skip fine-tuning and return success after precise click (default false)
//   - SwipeButton: custom slider template path overriding BetterSlidingSwipeButton
//   - ExceedingOverrideEnable: Pipeline node name to enable when target is out of range
//   - TargetType: TargetTypeValue (default) or TargetTypePercentage
//   - TargetReverse: reverse target calculation
type BetterSlidingAction struct {
	Target                  int
	QuantityBox             []int
	QuantityFilter          *quantityFilterParam
	QuantityOnlyRec         bool
	GreenMask               bool
	Direction               string
	IncreaseButton          buttonTarget
	DecreaseButton          buttonTarget
	CenterPointOffset       [2]int
	ClampTargetToMax        bool
	FinishAfterPreciseClick bool
	SwipeButton             string
	ExceedingOverrideEnable string
	TargetType              string
	TargetReverse           bool
	SwipeOnlyMode           bool
	OriginalTarget          int

	startBox              []int
	endBox                []int
	maxQuantity           int
	exceeded              bool
	runtimeTargetResolved bool
	logger                zerolog.Logger
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

// TargetType constants for canonical target type values.
const (
	TargetTypeValue      = "Value"
	TargetTypePercentage = "Percentage"
)

var defaultCenterPointOffset = [2]int{-10, 0}

var _ maa.CustomActionRunner = &BetterSlidingAction{}
