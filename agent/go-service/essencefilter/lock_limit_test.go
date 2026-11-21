package essencefilter

import (
	"math"
	"testing"
)

func TestExpectedEnhanceAttempts(t *testing.T) {
	cases := []struct {
		name   string
		levels [3]int
		want   float64
		wantOK bool
	}{
		{"maxed", [3]int{6, 6, 3}, 0, true},
		{"fresh", [3]int{1, 1, 1}, 177.073, true},
		{"slot3_target_only", [3]int{6, 6, 2}, 23.810, true},
		{"slots_1_2_to_6_slot3_to_3", [3]int{5, 5, 2}, 97.884, true},
		{"slot1_out_of_range_low", [3]int{0, 1, 1}, 0, false},
		{"slot2_out_of_range_high", [3]int{1, 7, 1}, 0, false},
		{"slot3_out_of_range_high", [3]int{1, 1, 4}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExpectedEnhanceAttempts(tc.levels)
			if ok != tc.wantOK {
				t.Fatalf("ExpectedEnhanceAttempts(%v) ok = %v, want %v", tc.levels, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if math.Abs(got-tc.want) > 1e-3 {
				t.Fatalf("ExpectedEnhanceAttempts(%v) = %.4f, want %.3f", tc.levels, got, tc.want)
			}
		})
	}
}
