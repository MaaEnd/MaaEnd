package goods

import (
	"testing"
)

func TestBuildReserveSlidingOverrideAidQuota(t *testing.T) {
	const node = "Sliding"
	tests := []struct {
		name       string
		quantity   int
		configured bool
		aidQuota   aidQuotaDecision
		wantNext   []string
		wantTarget int
		wantRev    bool
	}{
		{
			name:       "non-activity item sells all",
			configured: false,
			aidQuota:   aidQuotaDecision{Applied: false},
			wantNext:   []string{"OutpostTradingSell"},
			wantTarget: 999999,
			wantRev:    false,
		},
		{
			name:       "non-activity item with reserve rule",
			quantity:   10,
			configured: true,
			aidQuota:   aidQuotaDecision{Applied: false},
			wantNext:   []string{"OutpostTradingReserveAlreadySatisfied", "OutpostTradingSellThenLoop"},
			wantTarget: 10,
			wantRev:    true,
		},
		{
			name:       "activity item without reserve limited by quota",
			configured: false,
			aidQuota:   aidQuotaDecision{Applied: true, Limit: 3, Target: 3},
			wantNext:   []string{"OutpostTradingAidQuotaExhausted", "OutpostTradingSellThenLoop"},
			wantTarget: 3,
			wantRev:    false,
		},
		{
			name:       "activity item with reserve keeps min of excess and quota",
			quantity:   10,
			configured: true,
			aidQuota:   aidQuotaDecision{Applied: true, Limit: 5, Stock: 30, Target: 15},
			wantNext:   []string{"OutpostTradingAidQuotaExhausted", "OutpostTradingSellThenLoop"},
			wantTarget: 15,
			wantRev:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildReserveSlidingOverride(node, tt.quantity, tt.configured, tt.aidQuota)
			entry, ok := got[node].(map[string]any)
			if !ok {
				t.Fatalf("override entry missing for node %s", node)
			}
			next, ok := entry["next"].([]string)
			if !ok {
				t.Fatalf("next has unexpected type %T", entry["next"])
			}
			if len(next) != len(tt.wantNext) {
				t.Fatalf("next = %v, want %v", next, tt.wantNext)
			}
			for i := range tt.wantNext {
				if next[i] != tt.wantNext[i] {
					t.Errorf("next[%d] = %q, want %q", i, next[i], tt.wantNext[i])
				}
			}
			attach, ok := entry["attach"].(map[string]any)
			if !ok {
				t.Fatalf("attach missing")
			}
			if target, _ := attach["TargetQuantity"].(int); target != tt.wantTarget {
				t.Errorf("TargetQuantity = %v, want %v", attach["TargetQuantity"], tt.wantTarget)
			}
			if rev, _ := attach["ReverseTarget"].(bool); rev != tt.wantRev {
				t.Errorf("ReverseTarget = %v, want %v", attach["ReverseTarget"], tt.wantRev)
			}
		})
	}
}

func TestClampInt(t *testing.T) {
	if got := clampInt(5, 1, 10); got != 5 {
		t.Errorf("clampInt(5,1,10) = %d, want 5", got)
	}
	if got := clampInt(0, 1, 10); got != 1 {
		t.Errorf("clampInt(0,1,10) = %d, want 1", got)
	}
	if got := clampInt(20, 1, 10); got != 10 {
		t.Errorf("clampInt(20,1,10) = %d, want 10", got)
	}
}

func TestMarkSelectedReserveSkipped(t *testing.T) {
	resetReserveSession()
	defer resetReserveSession()

	if _, marked := markSelectedReserveSkipped(); marked {
		t.Error("skip without selected item should not mark")
	}

	setSelectedReserveItem("item_activity_test")
	if _, marked := markSelectedReserveSkipped(); !marked {
		t.Error("first skip should mark")
	}
	if _, marked := markSelectedReserveSkipped(); marked {
		t.Error("second skip should not mark again")
	}

	snapshot := reserveSatisfiedItemsSnapshot()
	if _, ok := snapshot["item_activity_test"]; !ok {
		t.Error("skipped item should appear in satisfied snapshot for selection exclusion")
	}
}

func TestParseAidQuotaNumber(t *testing.T) {
	cases := map[string]int{
		"123":  123,
		" 45 ": 45,
	}
	for raw, want := range cases {
		got, err := parseAidQuotaNumber(raw)
		if err != nil {
			t.Errorf("parseAidQuotaNumber(%q) error: %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("parseAidQuotaNumber(%q) = %d, want %d", raw, got, want)
		}
	}
	if _, err := parseAidQuotaNumber("abc"); err == nil {
		t.Error("parseAidQuotaNumber(\"abc\") should error")
	}
}
