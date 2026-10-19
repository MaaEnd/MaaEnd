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

// controllerKindFromInfo 是 PI_CONTROLLER 缺失时的兜底路径，覆盖 MaaFramework 的 GetInfo 输出。
func TestControllerKindFromInfo(t *testing.T) {
	cases := []struct {
		name string
		info string
		want string
	}{
		{"win32", `{"type":"win32","hwnd":1}`, ControllerKindWin32},
		{"adb", `{"type":"adb","adb_serial":"127.0.0.1:5555"}`, ControllerKindAdb},
		{"native android", `{"type":"native_android","display_id":0}`, ControllerKindNativeAndroid},
		{"playcover", `{"type":"PlayCover"}`, ControllerKindPlayCover},
		{"playcover alias", `{"type":"play_cover"}`, ControllerKindPlayCover},
		{"linux", `{"type":"linux"}`, ControllerKindLinux},
		// 非法 JSON 时退化到子串探测
		{"non-json native android", `type=native_android`, ControllerKindNativeAndroid},
		{"non-json win32", `type=win32 hwnd=1`, ControllerKindWin32},
		// 空串与无法识别的输入都返回 unknown
		{"empty", "", ControllerKindUnknown},
		{"unknown type", `{"type":"dbg"}`, ControllerKindUnknown},
		{"garbage", `not json at all`, ControllerKindUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := controllerKindFromInfo(c.info); got != c.want {
				t.Fatalf("controllerKindFromInfo(%q) = %q, want %q", c.info, got, c.want)
			}
		})
	}
}
