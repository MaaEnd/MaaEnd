//go:build windows

package launchcloudweb

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	// 与 interface.json 中 CloudWeb 控制器的 class_regex 保持一致
	shellClassName     = "MaaEnd.CloudShell"
	shellWindowTimeout = 30 * time.Second
	pollInterval       = 200 * time.Millisecond
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW = user32.NewProc("FindWindowW")
)

func run() bool {
	if shellWindowExists() {
		log.Info().
			Str("component", component).
			Msg("cloud shell is already running")
		return true
	}

	exePath := fsutil.OutputPath("cloud-shell", "cloud-shell.exe")
	if _, err := os.Stat(exePath); err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Str("path", exePath).
			Msg("cloud shell executable not found")
		return false
	}

	process, err := startDetached(exePath)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Str("path", exePath).
			Msg("failed to start cloud shell")
		return false
	}
	log.Info().
		Str("component", component).
		Str("path", exePath).
		Int("pid", process.Pid).
		Msg("cloud shell started")

	// WebView2 Runtime 缺失等初始化失败会让外壳在窗口出现前退出，不必等到超时
	exited := make(chan *os.ProcessState, 1)
	go func() {
		state, _ := process.Wait()
		exited <- state
	}()

	deadline := time.After(shellWindowTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case state := <-exited:
			exitCode := -1
			if state != nil {
				exitCode = state.ExitCode()
			}
			log.Error().
				Str("component", component).
				Int("exit_code", exitCode).
				Msg("cloud shell exited before its window appeared, see debug/cloud-shell.log")
			return false
		case <-deadline:
			log.Error().
				Str("component", component).
				Dur("timeout", shellWindowTimeout).
				Msg("cloud shell window not found")
			return false
		case <-ticker.C:
			if shellWindowExists() {
				log.Info().
					Str("component", component).
					Msg("cloud shell window is ready")
				return true
			}
		}
	}
}

// startDetached 启动外壳并让它活过 pretask：脱离控制台与进程组，不继承 MXU 读取 pretask 输出的管道。
// 工作目录设为安装目录，外壳的日志与登录数据落在 debug/、cache/ 下。
func startDetached(exePath string) (*os.Process, error) {
	start := func(flags uint32) (*os.Process, error) {
		cmd := exec.Command(exePath)
		cmd.Dir = fsutil.OutputPath()
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd.Process, nil
	}

	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	// 宿主若把 pretask 放进关闭即结束的作业对象，外壳会随 pretask 一起被结束，先尝试脱离作业
	process, err := start(flags | windows.CREATE_BREAKAWAY_FROM_JOB)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		process, err = start(flags)
	}
	return process, err
}

func shellWindowExists() bool {
	className, err := windows.UTF16PtrFromString(shellClassName)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
	return hwnd != 0
}
