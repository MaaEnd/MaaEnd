package gamelaunch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

func run(args []string) error {
	// Windows mutex 归属 OS 线程；等待过程中 goroutine 不能迁移到其他线程。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// 多个 GUI 实例也不能在检查进程与创建进程之间同时拉起游戏。
	name, _ := windows.UTF16PtrFromString(`Local\MaaEnd.GameLaunch`)
	mutex, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	defer windows.CloseHandle(mutex)
	state, err := windows.WaitForSingleObject(mutex, 0)
	if err != nil {
		return err
	}
	if state != windows.WAIT_OBJECT_0 && state != windows.WAIT_ABANDONED {
		return errors.New("another GameLaunch pretask is running")
	}
	defer windows.ReleaseMutex(mutex)
	ids, err := processIDs("Endfield.exe")
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		log.Info().Uints32("game_pids", ids).Msg("game is already running; skipping launch")
		return nil
	}
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	game, err := executablePath(opts.GamePath, "Endfield.exe")
	if err != nil {
		return err
	}
	if info, err := os.Stat(game); err != nil {
		return fmt.Errorf("inspect game executable: %w", err)
	} else if info.IsDir() {
		return errors.New("game executable path is a directory")
	}
	parent, err := prepareLauncher(opts.LauncherPath)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	// 启动器准备期间用户可能已手动启动游戏，创建前再次检查。
	if ids, err := processIDs("Endfield.exe"); err != nil {
		return err
	} else if len(ids) > 0 {
		log.Info().Uints32("game_pids", ids).Msg("game started while preparing launcher; skipping launch")
		return nil
	}
	process, err := createGame(game, parent)
	if err != nil {
		return fmt.Errorf("CreateProcessW: %w", err)
	}
	defer windows.CloseHandle(process.Process)
	windows.CloseHandle(process.Thread)
	log.Info().Uint32("game_pid", process.ProcessId).Str("game_path", game).
		Msg("created game with official launcher parent")
	if err := waitForGame(process); err != nil {
		return err
	}
	log.Info().Uint32("game_pid", process.ProcessId).Msg("game window is ready")
	return nil
}

func prepareLauncher(path string) (windows.Handle, error) {
	parent, err := officialParent()
	if parent != 0 || err != nil {
		return parent, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path, err = installedLauncher()
		if err != nil {
			return 0, err
		}
	} else if !filepath.IsAbs(path) || (!strings.EqualFold(filepath.Base(path), "Launcher.exe") && !strings.EqualFold(filepath.Base(path), "Games.exe")) {
		return 0, errors.New("configure the absolute path to the official Launcher.exe (or Games.exe), or leave it empty for auto-discovery")
	}
	fingerprint, err := verifyLauncher(path)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start official launcher: %w", err)
	}
	_ = cmd.Process.Release()
	log.Info().Str("launcher_path", path).Str("signer_sha1", fingerprint).Msg("started official launcher")
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		parent, err := officialParent()
		if parent != 0 || err != nil {
			return parent, err
		}
		select {
		case <-deadline.C:
			return 0, errors.New("timed out waiting for official Games.exe")
		case <-ticker.C:
		}
	}
}

func waitForGame(process windows.ProcessInformation) error {
	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := windows.WaitForSingleObject(process.Process, 0)
		if err != nil {
			return err
		}
		if state != uint32(windows.WAIT_TIMEOUT) {
			var code uint32
			if err := windows.GetExitCodeProcess(process.Process, &code); err != nil {
				return err
			}
			return fmt.Errorf("game exited before window became ready: exit code %d", code)
		}
		ready, err := gameWindowReady(process.ProcessId)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-deadline.C:
			return errors.New("timed out waiting for a visible UnityWndClass game window")
		case <-ticker.C:
		}
	}
}
