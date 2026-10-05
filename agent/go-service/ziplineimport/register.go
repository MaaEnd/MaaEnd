//go:build linux && !android

package ziplineimport

import maa "github.com/MaaXYZ/maa-framework-go/v4"

const (
	componentName = "ziplineimport"

	actionName = "ZiplineImport"
)

// Register 仅在 Linux 上注册 ZiplineImport 自定义动作。
//
// Windows 上的同名动作由 cpp-algo 的 WebView2 实现注册，这里必须保持「任何情况下只有一方
// 注册」：Windows 只有 cpp，Linux 只有 Go。因此本文件用 //go:build linux && !android 限定，
// 非 Linux 走 register_stub.go 的空实现。
//
// 标签必须排除 android：GOOS=android 同样满足 linux，而这套实现依赖桌面 Firefox 与 certutil，
// 设备上都没有。Android 走 register_android.go 的占位动作。
func Register() {
	maa.AgentServerRegisterCustomAction(actionName, &Action{})
}
