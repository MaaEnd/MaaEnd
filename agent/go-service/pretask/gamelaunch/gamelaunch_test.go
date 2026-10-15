package gamelaunch

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseLauncherPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		fail bool
	}{
		{name: "disabled"},
		{name: "empty options", args: []string{`{}`}},
		{name: "PI input object", args: []string{`{"GameLaunchLauncher":{"Path":" D:\\鹰角网络\\Launcher.exe "}}`}, want: `D:\鹰角网络\Launcher.exe`},
		{name: "malformed JSON", args: []string{`{"GameLaunchLauncher":`}, fail: true},
		{name: "wrong input shape", args: []string{`{"GameLaunchLauncher":"Launcher.exe"}`}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLauncherPath(tc.args)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("parseLauncherPath() = %q, %v; want %q, error=%t", got, err, tc.want, tc.fail)
			}
		})
	}
}

func TestSelectWindows(t *testing.T) {
	game := &maa.DesktopWindow{ClassName: "UnityWndClass", WindowName: "Endfield"}
	launcher := &maa.DesktopWindow{ClassName: "Qt5152QWindowIcon", WindowName: "鹰角启动器"}
	failed := &maa.DesktopWindow{ClassName: "#32770", WindowName: "游戏启动失败"}
	for _, tc := range []struct {
		name    string
		windows []*maa.DesktopWindow
		game    *maa.DesktopWindow
		fail    bool
	}{
		{name: "launcher only", windows: []*maa.DesktopWindow{launcher}},
		{name: "existing game", windows: []*maa.DesktopWindow{launcher, game}, game: game},
		{name: "wrong window class", windows: []*maa.DesktopWindow{{ClassName: "Other", WindowName: "Endfield"}}},
		{name: "error before game", windows: []*maa.DesktopWindow{failed, game}, fail: true},
		{name: "error after game", windows: []*maa.DesktopWindow{game, failed}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := selectWindows(tc.windows)
			if (err != nil) != tc.fail || got != tc.game {
				t.Fatalf("selectWindows() game=%v, error=%v", got, err)
			}
		})
	}
}
