package gamesetting

import "testing"

func TestParsePerAppAutoHDRFlags(t *testing.T) {
	if override, on := parsePerAppAutoHDRFlags("2096"); !override || on {
		t.Errorf("2096: got (override=%t, on=%t), want (true, false)", override, on)
	}
	if override, on := parsePerAppAutoHDRFlags("2097"); override || !on {
		t.Errorf("2097: got (override=%t, on=%t), want (false, true)", override, on)
	}
	if override, on := parsePerAppAutoHDRFlags("6193"); !override || !on {
		t.Errorf("6193: got (override=%t, on=%t), want (true, true)", override, on)
	}
}
