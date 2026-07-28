package bettersliding

import (
	"reflect"
	"testing"
)

func TestParseBetterSlidingParamCanonicalQuantityFields(t *testing.T) {
	raw := `{
		"TargetQuantity": 88,
		"SliderQuantity": {"Box": [1, 2, 3, 4]},
		"AvailableQuantity": {"Box": [5, 6, 7, 8]},
		"OutOfRangeOverrideEnable": "OutOfRange",
		"TargetReachableOverrideEnable": "TargetReachable",
		"TargetQuantityType": "Percentage",
		"ReverseTarget": true,
		"ClampTargetToSliderMax": true
	}`

	got, err := parseBetterSlidingParam(raw)
	if err != nil {
		t.Fatalf("parseBetterSlidingParam returned error: %v", err)
	}

	if got.TargetQuantity != 88 {
		t.Fatalf("unexpected TargetQuantity: got %d, want 88", got.TargetQuantity)
	}
	if !reflect.DeepEqual(got.SliderQuantity.Box, []int{1, 2, 3, 4}) {
		t.Fatalf("unexpected SliderQuantity.Box: %#v", got.SliderQuantity.Box)
	}
	if !reflect.DeepEqual(got.AvailableQuantity.Box, []int{5, 6, 7, 8}) {
		t.Fatalf("unexpected AvailableQuantity.Box: %#v", got.AvailableQuantity.Box)
	}
	if got.OutOfRangeOverrideEnable != "OutOfRange" {
		t.Fatalf("unexpected OutOfRangeOverrideEnable: %q", got.OutOfRangeOverrideEnable)
	}
	if got.TargetReachableOverrideEnable != "TargetReachable" {
		t.Fatalf("unexpected TargetReachableOverrideEnable: %q", got.TargetReachableOverrideEnable)
	}
	if got.TargetQuantityType != TargetQuantityTypePercentage {
		t.Fatalf("unexpected TargetQuantityType: %q", got.TargetQuantityType)
	}
	if !got.ReverseTarget {
		t.Fatal("ReverseTarget was not parsed")
	}
	if !got.ClampTargetToSliderMax {
		t.Fatal("ClampTargetToSliderMax was not parsed")
	}

	wantPresence := betterSlidingParamPresence{
		TargetQuantity:                true,
		SliderQuantity:                true,
		AvailableQuantity:             true,
		OutOfRangeOverrideEnable:      true,
		TargetReachableOverrideEnable: true,
		TargetQuantityType:            true,
		ReverseTarget:                 true,
		ClampTargetToSliderMax:        true,
	}
	if got.presence != wantPresence {
		t.Fatalf("unexpected parameter presence: got %#v, want %#v", got.presence, wantPresence)
	}
}
