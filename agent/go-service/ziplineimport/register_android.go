//go:build android

package ziplineimport

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
)

const (
	componentName = "ziplineimport"

	actionName = "ZiplineImport"
)

var _ maa.CustomActionRunner = &unsupportedAction{}

// unsupportedAction 是 ZiplineImport 在 Android 上的占位实现，只提示暂不支持。
//
// 导入要开一个浏览器让用户自己登录，而 agent 在设备上是没有界面的子进程，桌面端的两套实现
// （cpp-algo 的内嵌浏览器、Linux 的 Firefox + 代理）都搬不过来。任务入口在 PI 里与桌面 ADB
// 共用同一个控制器、藏不掉，所以这里注册一个同名动作把原因说清楚，而不是让它报找不到动作。
type unsupportedAction struct{}

func (a *unsupportedAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	log.Error().Str("component", componentName).Msg("zipline import: not supported on Android")
	maafocus.Print(ctx, i18n.T("ziplineimport.android_unsupported"))
	return false
}

// Register 在 Android 上注册占位动作；cpp-algo 在 Android 上不注册同名动作。
func Register() {
	maa.AgentServerRegisterCustomAction(actionName, &unsupportedAction{})
}
