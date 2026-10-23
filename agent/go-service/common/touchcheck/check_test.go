package touchcheck

import "testing"

func TestAllowsCombat(t *testing.T) {
	for _, tc := range []struct {
		name, kind           string
		release, want, probe bool
	}{
		{"adb touch ready", "adb", true, true, true},
		{"adb shell rejected", "adb", false, false, true},
		{"unknown rejected", "", true, false, false},
		{"desktop unchanged", "win32", false, true, false},
		{"playcover unchanged", "playcover", false, true, false},
		{"native android unchanged", "native_android", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			got := allowsCombat(tc.kind, func() bool { called = true; return tc.release })
			if got != tc.want || called != tc.probe {
				t.Fatalf("allowed=%v, probe=%v; want %v, %v", got, called, tc.want, tc.probe)
			}
		})
	}
}
