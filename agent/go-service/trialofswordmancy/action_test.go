package trialofswordmancy

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

func TestParseAbandCountExhaustedWithNumber(t *testing.T) {
	text := "本日放弃次数已用完，继续放弃将会扣除1次奖励演算次数，是否确认放弃？"
	if got := parseAbandCount(text); got != 0 {
		t.Fatalf("parseAbandCount() = %d, want 0", got)
	}
}

func TestParseFirstInt(t *testing.T) {
	n, ok := parseFirstInt("abc12de34")
	if !ok || n != 12 {
		t.Fatalf("parseFirstInt() = %d, %v; want 12, true", n, ok)
	}

	if n, ok := parseFirstInt("无数字"); ok || n != 0 {
		t.Fatalf("parseFirstInt(no digits) = %d, %v; want 0, false", n, ok)
	}
}

func TestLoadOverflowModeInvalidFallback(t *testing.T) {
	got := loadOverflowMode(`{"overflowMode":"OverflowTypo"}`)
	if got != solver.OverflowNone {
		t.Fatalf("loadOverflowMode(invalid) = %v, want %v", got, solver.OverflowNone)
	}
}

func TestPickDecisionPrefersAbandonWhenCloseToDraw(t *testing.T) {
	got := pickDecision([]solver.Outcome{
		{Action: solver.DrawCard, Total: 10},
		{Action: solver.Abandon, Total: 9.5},
	})
	if got != solver.Abandon {
		t.Fatalf("pickDecision() = %v, want %v", got, solver.Abandon)
	}
}
