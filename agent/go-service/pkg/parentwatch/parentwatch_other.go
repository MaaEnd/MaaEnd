//go:build !windows

package parentwatch

import (
	"errors"
	"syscall"
)

// posixWatcher 在 POSIX 下基于 kill(pid, 0) 探活：
// 进程不存在时 errno 返回 ESRCH。注意 PID 复用风险：
// 若父进程退出后系统把同一个 PID 复用给另一个进程，
// 探活会误判为存活；但子进程在 reparent 后 getppid() 会变成 1，
// 启动时的初值已经记录在 pid 字段里，可以保持稳定语义。
type posixWatcher struct {
	pid int
}

func newWatcher(pid int) (watcher, error) {
	return &posixWatcher{pid: pid}, nil
}

func (w *posixWatcher) IsAlive() (bool, error) {
	err := syscall.Kill(w.pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	// EPERM 表示目标进程存在但无权访问，依然视为存活。
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, err
}

func (w *posixWatcher) Close() {}
