package control

import "testing"

func TestControlTypeFromPIType(t *testing.T) {
	cases := []struct {
		name string
		pi   string
		want string
	}{
		{"adb normalized", "  AdB  ", CONTROL_TYPE_ADB},
		{"native android", "NATIVE_ANDROID", CONTROL_TYPE_ADB},
		{"playcover", "PlayCover", CONTROL_TYPE_ADB},
		{"win32", " Win32 ", CONTROL_TYPE_WIN32},
		{"macos", "MacOS", CONTROL_TYPE_MACOS},
		{"linux", "LINUX", CONTROL_TYPE_LINUX},
		{"unknown", "unknown", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := controlTypeFromPIType(c.pi); got != c.want {
				t.Fatalf("controlTypeFromPIType(%q) = %q, want %q", c.pi, got, c.want)
			}
		})
	}
}

func TestControlTypeFromInfo(t *testing.T) {
	cases := []struct {
		name string
		info string
		want string
	}{
		{"win32", `{"type":"win32","hwnd":1}`, CONTROL_TYPE_WIN32},
		{"adb", `{"type":"adb","adb_serial":"127.0.0.1:5555"}`, CONTROL_TYPE_ADB},
		// MaaFwApp 在手机上用的 Android 原生控制器：触控 + 移动端 UI，按 ADB 处理
		{"native android", `{"type":"native_android","display_id":0,"touch_resolution":{"width":1280,"height":720}}`, CONTROL_TYPE_ADB},
		{"native android fallback", `type=native_android`, CONTROL_TYPE_ADB},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := controlTypeFromInfo(c.info)
			if err != nil {
				t.Fatalf("controlTypeFromInfo(%q) error: %v", c.info, err)
			}
			if got != c.want {
				t.Fatalf("controlTypeFromInfo(%q) = %q, want %q", c.info, got, c.want)
			}
		})
	}
}

func TestControlTypeFromInfoRejectsUnknown(t *testing.T) {
	for _, info := range []string{"", `{"type":"dbg"}`, `{}`} {
		if got, err := controlTypeFromInfo(info); err == nil {
			t.Fatalf("controlTypeFromInfo(%q) = %q, want error", info, got)
		}
	}
}
