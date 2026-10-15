//go:build windows

package gamelaunch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/maa-framework-go/v4/controller/win32"
	"github.com/rs/zerolog/log"
)

func launch(path string) error {
	libDir := os.Getenv("MAAFW_BINARY_PATH")
	if libDir == "" {
		libDir = fsutil.OutputPath("maafw")
	}
	if err := maa.Init(
		maa.WithLibDir(libDir),
		maa.WithLogDir(fsutil.OutputPath("debug", "game-launch")),
		maa.WithSaveDraw(true),
	); err != nil {
		return fmt.Errorf("initialize MaaFramework: %w", err)
	}
	defer closeHandle("MaaFramework", maa.Release)

	windows, err := maa.FindDesktopWindows()
	if err != nil {
		return err
	}
	game, _, err := selectWindows(windows)
	if err != nil {
		return err
	}
	if game != nil {
		log.Info().Str("component", "gamelaunch").Msg("Game is already running")
		return nil
	}

	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), "Launcher.exe") {
		return errors.New("configure the absolute path to the official Launcher.exe")
	}
	if info, err := os.Stat(path); err != nil {
		return fmt.Errorf("open official launcher: %w", err)
	} else if info.IsDir() {
		return errors.New("official launcher path must be a file")
	}
	cmd := exec.Command(path, "--game=endfield", "--reason=4")
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start official launcher: %w", err)
	}
	// The launcher owns the game process and must remain alive during game startup.
	// Release only our Go process handle; do not terminate the launcher on return.
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release launcher process handle: %w", err)
	}

	window, isGame, err := waitForWindow(false, 30*time.Second)
	if err != nil || isGame {
		return err
	}
	if err := clickStart(window); err != nil {
		return err
	}
	_, _, err = waitForWindow(true, 90*time.Second)
	return err
}

// waitForWindow waits for external process/window creation, not a fixed UI delay.
// UI recognition and Start Game clicks are handled by Pipeline.
func waitForWindow(gameOnly bool, timeout time.Duration) (*maa.DesktopWindow, bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		windows, err := maa.FindDesktopWindows()
		if err != nil {
			return nil, false, err
		}
		game, launcher, err := selectWindows(windows)
		if err != nil {
			return nil, false, err
		}
		if game != nil {
			return game, true, nil
		}
		if !gameOnly && launcher != nil {
			return launcher, false, nil
		}
		select {
		case <-timer.C:
			return nil, false, fmt.Errorf("timed out waiting for launcher/game window (gameOnly=%t)", gameOnly)
		case <-ticker.C:
		}
	}
}

func clickStart(window *maa.DesktopWindow) error {
	controller, err := maa.NewWin32Controller(window.Handle, win32.ScreencapBackground, win32.InputSeize, win32.InputSeize)
	if err != nil {
		return err
	}
	defer closeHandle("controller", controller.Destroy)
	if !controller.PostConnect().Wait().Success() {
		return errors.New("connect official launcher window failed")
	}
	resource, err := maa.NewResource()
	if err != nil {
		return err
	}
	defer closeHandle("resource", resource.Destroy)
	if err := resource.UseCPU(); err != nil {
		return err
	}
	if !resource.PostOcrModel(fsutil.OutputPath("resource", "model", "ocr")).Wait().Success() {
		return errors.New("load launcher OCR model failed")
	}
	if !resource.PostPipeline(fsutil.OutputPath("resource", "pipeline", "OfficialLauncher")).Wait().Success() {
		return errors.New("load official launcher Pipeline failed")
	}
	tasker, err := maa.NewTasker()
	if err != nil {
		return err
	}
	defer closeHandle("tasker", tasker.Destroy)
	if err := tasker.BindResource(resource); err != nil {
		return err
	}
	if err := tasker.BindController(controller); err != nil {
		return err
	}
	if !tasker.PostTask("OfficialLauncherStartGame").Wait().Success() {
		return errors.New("launcher did not confirm game startup; check launcher login, updates and permissions")
	}
	return nil
}

func closeHandle(name string, close func() error) {
	if err := close(); err != nil {
		log.Warn().Err(err).Str("component", "gamelaunch").Str("handle", name).Msg("Failed to release native handle")
	}
}
