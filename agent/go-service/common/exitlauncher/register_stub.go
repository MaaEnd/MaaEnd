//go:build !linux

package exitlauncher

// Register 在非 Linux 平台为空实现：ExitGameLauncherAction 依赖 Wine/Proton 进程树，
// 仅在 Linux（resource_linux）注册。
func Register() {}
