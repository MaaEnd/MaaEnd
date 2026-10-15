package gamelaunch

import (
	"errors"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// selectWindows checks the failure dialog before accepting the Unity window:
// a rejected direct launch also creates an Endfield window.
func selectWindows(windows []*maa.DesktopWindow) (game, launcher *maa.DesktopWindow, err error) {
	for _, window := range windows {
		switch {
		case window.WindowName == "游戏启动失败":
			return nil, nil, errors.New("game launch failed; close the error dialog and retry through the official launcher")
		case window.WindowName == "Endfield" && window.ClassName == "UnityWndClass":
			game = window
		case window.WindowName == "鹰角启动器":
			launcher = window
		}
	}
	return game, launcher, nil
}
