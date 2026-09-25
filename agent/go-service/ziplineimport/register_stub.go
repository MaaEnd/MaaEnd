//go:build !linux

package ziplineimport

// Register 为非 Linux 平台的空实现：ZiplineImport 仅在 Linux 由 Go 注册，
// Windows 由 cpp-algo 注册，macOS 暂不提供。保证任意时刻只有一方注册同名动作。
func Register() {}
