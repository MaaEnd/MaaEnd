package pullcount

import "testing"

func TestCalculatePullCount(t *testing.T) {
	tests := []struct {
		name             string
		stock            resourceStock
		reserveOriginium int
		reserveOroberyl  int
		want             calculationResult
	}{
		{
			name: "issue resource example",
			// 旧顶栏「换算嵌晶玉 2925」= 39 颗源石；保留 29 → 可用 10*75=750
			stock: resourceStock{
				Originium:        39,
				Oroberyl:         20770,
				CarryToNextPulls: 3,
			},
			reserveOriginium: 29,
			reserveOroberyl:  0,
			want: calculationResult{
				UsableOriginiumOroberyl: 750,
				UsableOroberyl:          20770,
				OroberylPulls:           41,
				UsableOriginiumPulls:    1,
				ResourcePulls:           43,
				CurrentPoolTotal:        46,
				NextPoolTotal:           56,
			},
		},
		{
			name: "reserved originium clamps to zero",
			stock: resourceStock{
				Originium: 26,
				Oroberyl:  499,
			},
			reserveOriginium: 29,
			reserveOroberyl:  0,
			want: calculationResult{
				UsableOriginiumOroberyl: 0,
				UsableOroberyl:          499,
				ResourcePulls:           0,
				NextPoolTotal:           10,
			},
		},
		{
			name: "custom oroberyl reserve",
			stock: resourceStock{
				Originium:        29,
				Oroberyl:         1500,
				CarryToNextPulls: 1,
			},
			reserveOriginium: 29,
			reserveOroberyl:  500,
			want: calculationResult{
				UsableOriginiumOroberyl: 0,
				UsableOroberyl:          1000,
				OroberylPulls:           2,
				ResourcePulls:           2,
				CurrentPoolTotal:        3,
				NextPoolTotal:           13,
			},
		},
	}

	for _, tt := range tests {
		got := calculatePullCount(tt.stock, tt.reserveOriginium, tt.reserveOroberyl)
		if got.UsableOriginiumOroberyl != tt.want.UsableOriginiumOroberyl ||
			got.UsableOroberyl != tt.want.UsableOroberyl ||
			got.OroberylPulls != tt.want.OroberylPulls ||
			got.UsableOriginiumPulls != tt.want.UsableOriginiumPulls ||
			got.ResourcePulls != tt.want.ResourcePulls ||
			got.CurrentPoolTotal != tt.want.CurrentPoolTotal ||
			got.NextPoolTotal != tt.want.NextPoolTotal {
			t.Fatalf("%s: calculatePullCount() = %+v, want key fields %+v", tt.name, got, tt.want)
		}
	}
}
