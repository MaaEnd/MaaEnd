package control

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
)

func TestControlTypeFromInfo(t *testing.T) {
	cases := []struct {
		name string
		info string
		want string
	}{
		{"win32", `{"type":"win32","hwnd":1}`, pienv.ControllerKindWin32},
		{"adb", `{"type":"adb","adb_serial":"127.0.0.1:5555"}`, pienv.ControllerKindAdb},
		// MaaFwApp 在手机上用的 Android 原生控制器：触控 + 移动端 UI，是否与 ADB 合并由需求决定
		{"native android", `{"type":"native_android","display_id":0,"touch_resolution":{"width":1280,"height":720}}`, pienv.ControllerKindNativeAndroid},
		{"native android fallback", `type=native_android`, pienv.ControllerKindNativeAndroid},
		{"playcover", `{"type":"PlayCover"}`, pienv.ControllerKindPlayCover},
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
