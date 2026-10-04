package ims

import (
	"fmt"
	"testing"
)

func TestSelectSupplyPlanTarget(t *testing.T) {
	tests := []struct {
		name                string
		candidates          []supplyPlanCandidate
		items               map[string]int
		want                string
		allowSurplus        bool
		continueAfterTarget bool
	}{
		{
			name: "lowest ratio rather than first target or lowest absolute count",
			candidates: []supplyPlanCandidate{
				{"gold", "{gold}>=10000"},
				{"skill", "{skill}>=100"},
			},
			items: map[string]int{"gold": 1000, "skill": 50},
			want:  "gold",
		},
		{
			name: "later target with lower progress",
			candidates: []supplyPlanCandidate{
				{"gold", "{gold}>=1000"},
				{"skill", "{skill}>=100"},
			},
			items: map[string]int{"gold": 900, "skill": 10},
			want:  "skill",
		},
		{
			name: "missing items are zero and ties keep dispatch order",
			candidates: []supplyPlanCandidate{
				{"gold", "{gold}>=1000"},
				{"skill", "{skill}>=100"},
			},
			want: "gold",
		},
		{
			name: "equal nonzero ratios keep dispatch order",
			candidates: []supplyPlanCandidate{
				{"gold", "{gold}>=1000"},
				{"skill", "{skill}>=100"},
			},
			items: map[string]int{"gold": 200, "skill": 20},
			want:  "gold",
		},
		{
			name: "last unmet target continues after meeting the plan",
			candidates: []supplyPlanCandidate{
				{"disabled", "{disabled}>=0"},
				{"met", "{met}>=10"},
				{"exceeded", "{exceeded}>=10"},
				{"needed", "{needed}>=100"},
			},
			items:               map[string]int{"met": 10, "exceeded": 11, "needed": 50},
			want:                "needed",
			allowSurplus:        true,
			continueAfterTarget: true,
		},
		{
			name: "weighted experience uses experience rather than card count",
			candidates: []supplyPlanCandidate{
				{"experience", "({high}*10000+{mid}*1000+{low}*200)>=20000"},
				{"gold", "{gold}>=1000"},
			},
			items: map[string]int{"high": 1, "mid": 2, "low": 5, "gold": 500},
			want:  "gold",
		},
		{
			name:                "all targets exactly met keep dispatch order and continue",
			candidates:          []supplyPlanCandidate{{"gold", "{gold}>=1000"}, {"skill", "{skill}>=100"}},
			items:               map[string]int{"gold": 1000, "skill": 100},
			want:                "gold",
			allowSurplus:        true,
			continueAfterTarget: true,
		},
		{
			name:                "101 percent is preferred to 120 percent",
			candidates:          []supplyPlanCandidate{{"gold", "{gold}>=1000"}, {"skill", "{skill}>=100"}},
			items:               map[string]int{"gold": 1200, "skill": 101},
			want:                "skill",
			allowSurplus:        true,
			continueAfterTarget: true,
		},
		{
			name: "above-target experience remains weighted and zero targets excluded",
			candidates: []supplyPlanCandidate{
				{"disabled", "{disabled}>=0"},
				{"gold", "{gold}>=1000"},
				{"experience", "({high}*10000+{mid}*1000+{low}*200)>=20000"},
			},
			items:               map[string]int{"gold": 1200, "high": 1, "mid": 10, "low": 1},
			want:                "experience",
			allowSurplus:        true,
			continueAfterTarget: true,
		},
		{
			name: "multiple unmet targets still stop at the selected target",
			candidates: []supplyPlanCandidate{
				{"gold", "{gold}>=1000"},
				{"skill", "{skill}>=100"},
				{"met", "{met}>=10"},
			},
			items:        map[string]int{"gold": 900, "skill": 10, "met": 12},
			want:         "skill",
			allowSurplus: true,
		},
		{
			name:       "surplus disabled ends when all targets are exactly met",
			candidates: []supplyPlanCandidate{{"gold", "{gold}>=1000"}, {"skill", "{skill}>=100"}},
			items:      map[string]int{"gold": 1000, "skill": 100},
		},
		{
			name:       "surplus disabled ends when all targets are exceeded",
			candidates: []supplyPlanCandidate{{"gold", "{gold}>=1000"}, {"skill", "{skill}>=100"}},
			items:      map[string]int{"gold": 1200, "skill": 101},
		},
		{
			name:       "surplus disabled stops the last unmet material at its target",
			candidates: []supplyPlanCandidate{{"gold", "{gold}>=1000"}, {"skill", "{skill}>=100"}},
			items:      map[string]int{"gold": 1200, "skill": 90},
			want:       "skill",
		},
		{
			name:       "all targets disabled",
			candidates: []supplyPlanCandidate{{"gold", "{gold}>=0"}},
		},
		{
			name:         "surplus enabled still excludes zero targets",
			candidates:   []supplyPlanCandidate{{"gold", "{gold}>=0"}},
			allowSurplus: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectSupplyPlanTarget(tt.candidates, tt.items, tt.allowSurplus)
			if err != nil || got.name != tt.want || got.continueAfterTarget != tt.continueAfterTarget {
				t.Fatalf("selection = %+v, err = %v; want %q, continueAfterTarget = %v",
					got, err, tt.want, tt.continueAfterTarget)
			}
		})
	}
}

func TestSelectSupplyPlanTargetLargeRatios(t *testing.T) {
	// These fractions differ beyond float64 precision and their products
	// overflow int64. The second fraction is smaller, despite being later.
	target := int64(1 << 60)
	if int64(int(target)) != target {
		t.Skip("requires 64-bit ints")
	}
	candidates := []supplyPlanCandidate{
		{"first", fmt.Sprintf("{first}>=%d", target)},
		{"second", fmt.Sprintf("{second}>=%d", target)},
	}
	selection, err := selectSupplyPlanTarget(candidates, map[string]int{
		"first": int(target - 1), "second": int(target - 2),
	}, false)
	if err != nil || selection.name != "second" {
		t.Fatalf("selection = %+v, err = %v; want second", selection, err)
	}
}

func TestSelectSupplyPlanTargetInvalidExpression(t *testing.T) {
	for _, expression := range []string{"", "{gold}>10", "{gold}>=true", "{gold}>=1/0", "{gold}>=not_a_number"} {
		t.Run(expression, func(t *testing.T) {
			_, err := selectSupplyPlanTarget([]supplyPlanCandidate{{"gold", expression}}, nil, false)
			if err == nil {
				t.Fatal("invalid target expression must fail instead of choosing an arbitrary material")
			}
		})
	}
}

func TestParseSupplyPlanSelectLowestRatioParam(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"{}", false},
		{`{"continue_after_target":false}`, false},
		{`{"continue_after_target":true}`, true},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			params, err := parseSupplyPlanSelectLowestRatioParam(tt.raw)
			if err != nil || params.ContinueAfterTarget != tt.want {
				t.Fatalf("params = %+v, err = %v; want continue_after_target = %v", params, err, tt.want)
			}
		})
	}
	for _, raw := range []string{"{", `{"continue_after_target":"true"}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseSupplyPlanSelectLowestRatioParam(raw); err == nil {
				t.Fatal("invalid switch parameter must fail")
			}
		})
	}
}
