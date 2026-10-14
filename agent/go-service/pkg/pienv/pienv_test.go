package pienv

import "testing"

func TestNormalizeControllerKind(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"adb", "  AdB  ", ControllerKindAdb},
		{"playcover", "PlayCover", ControllerKindPlayCover},
		{"play_cover alias", "play_cover", ControllerKindPlayCover},
		{"native android", "NATIVE_ANDROID", ControllerKindNativeAndroid},
		{"win32", " Win32 ", ControllerKindWin32},
		{"macos", "MacOS", ControllerKindMacOS},
		{"linux", "LINUX", ControllerKindLinux},
		{"gamepad", "Gamepad", ControllerKindGamepad},
		{"empty", "", ControllerKindUnknown},
		{"unknown", "unknown", ControllerKindUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeControllerKind(c.raw); got != c.want {
				t.Fatalf("NormalizeControllerKind(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestIsAdbLikeControllerType(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"Adb", true},
		{"PlayCover", true},
		{"play_cover", true},
		{"native_android", true},
		{"Win32", false},
		{"MacOS", false},
		{"Linux", false},
		{"Gamepad", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			if got := IsAdbLikeControllerType(c.raw); got != c.want {
				t.Fatalf("IsAdbLikeControllerType(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}
