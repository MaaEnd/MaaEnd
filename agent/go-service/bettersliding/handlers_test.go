package bettersliding

import "testing"

func TestResolveSliderQuantityOutcome(t *testing.T) {
	tests := []struct {
		name               string
		targetQuantity     int
		sliderMaxQuantity  int
		clamp              bool
		wantTargetQuantity int
		wantOutcome        sliderQuantityOutcome
	}{
		{
			name:               "positive target with zero slider max is out of range",
			targetQuantity:     17720,
			sliderMaxQuantity:  0,
			clamp:              true,
			wantTargetQuantity: 17720,
			wantOutcome:        sliderQuantityOutcomeOutOfRange,
		},
		{
			name:               "below-range outcome takes precedence when target and slider max are zero",
			targetQuantity:     0,
			sliderMaxQuantity:  0,
			clamp:              true,
			wantTargetQuantity: 0,
			wantOutcome:        sliderQuantityOutcomeOutOfRange,
		},
		{
			name:               "upper target clamps to positive slider max",
			targetQuantity:     17947,
			sliderMaxQuantity:  229,
			clamp:              true,
			wantTargetQuantity: 229,
			wantOutcome:        sliderQuantityOutcomeClamped,
		},
		{
			name:               "reachable target remains unchanged",
			targetQuantity:     229,
			sliderMaxQuantity:  229,
			clamp:              true,
			wantTargetQuantity: 229,
			wantOutcome:        sliderQuantityOutcomeTargetReachable,
		},
		{
			name:               "upper target without clamp is out of range",
			targetQuantity:     230,
			sliderMaxQuantity:  229,
			clamp:              false,
			wantTargetQuantity: 230,
			wantOutcome:        sliderQuantityOutcomeOutOfRange,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotTargetQuantity, gotOutcome := resolveSliderQuantityOutcome(
				test.targetQuantity,
				test.sliderMaxQuantity,
				test.clamp,
			)
			if gotTargetQuantity != test.wantTargetQuantity || gotOutcome != test.wantOutcome {
				t.Fatalf(
					"resolveSliderQuantityOutcome(%d, %d, %t) = (%d, %d), want (%d, %d)",
					test.targetQuantity,
					test.sliderMaxQuantity,
					test.clamp,
					gotTargetQuantity,
					gotOutcome,
					test.wantTargetQuantity,
					test.wantOutcome,
				)
			}
		})
	}
}
