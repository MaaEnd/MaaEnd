package ims

import (
	"fmt"
	"testing"
)

func TestSelectSupplyPlanTarget(t *testing.T) {
	tests := []struct {
		name       string
		candidates []supplyPlanCandidate
		items      map[string]int
		want       string
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
			name: "zero targets and satisfied targets are skipped",
			candidates: []supplyPlanCandidate{
				{"disabled", "{disabled}>=0"},
				{"met", "{met}>=10"},
				{"exceeded", "{exceeded}>=10"},
				{"needed", "{needed}>=100"},
			},
			items: map[string]int{"met": 10, "exceeded": 11, "needed": 50},
			want:  "needed",
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
			name:       "all targets met",
			candidates: []supplyPlanCandidate{{"gold", "{gold}>=100"}},
			items:      map[string]int{"gold": 100},
		},
		{
			name:       "all targets disabled",
			candidates: []supplyPlanCandidate{{"gold", "{gold}>=0"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectSupplyPlanTarget(tt.candidates, tt.items)
			if err != nil || got.name != tt.want {
				t.Fatalf("selection = %+v, err = %v; want %q", got, err, tt.want)
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
	})
	if err != nil || selection.name != "second" {
		t.Fatalf("selection = %+v, err = %v; want second", selection, err)
	}
}

func TestSelectSupplyPlanTargetInvalidExpression(t *testing.T) {
	for _, expression := range []string{"", "{gold}>10", "{gold}>=true", "{gold}>=1/0", "{gold}>=not_a_number"} {
		t.Run(expression, func(t *testing.T) {
			_, err := selectSupplyPlanTarget([]supplyPlanCandidate{{"gold", expression}}, nil)
			if err == nil {
				t.Fatal("invalid target expression must fail instead of choosing an arbitrary material")
			}
		})
	}
}
