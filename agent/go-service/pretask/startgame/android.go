package startgame

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func androidIntent(version string) (string, error) {
	// Keep these intents aligned with AndroidOpenGame's ClientVersion options.
	switch version {
	case "", "CN":
		return "com.hypergryph.endfield/com.u8.sdk.U8UnityContext", nil
	case "Bilibili":
		return "com.hypergryph.endfield.bilibili/com.u8.sdk.U8UnityContext", nil
	case "Global":
		return "com.gryphline.endfield.gp/com.u8.sdk.U8UnityContext", nil
	case "VN":
		return "com.hypergryph.endfield.vn/com.u8.sdk.U8UnityContext", nil
	case "Cloud":
		return "com.hypergryph.cloud.endfield/com.hypergryph.cloud.endfield.splash.SplashActivity", nil
	default:
		return "", fmt.Errorf("unsupported Android client version: %q", version)
	}
}

// startAndroidGame connects to the configured address with a temporary owned
// controller. If connection fails, it attempts emulator startup, waits once,
// and reconnects once. Success means the app-start job succeeded; cloud-game
// button recognition and clicks are handled by the later OpenGame Pipeline.
func startAndroidGame(opts launchOptions) error {
	libDir := os.Getenv("MAAFW_BINARY_PATH")
	if libDir == "" {
		libDir = fsutil.OutputPath("maafw")
	}
	if err := maa.Init(maa.WithLibDir(libDir), maa.WithLogDir(fsutil.OutputPath("debug", "startgame"))); err != nil {
		return fmt.Errorf("initialize MaaFramework: %w", err)
	}
	defer func() {
		if err := maa.Release(); err != nil {
			log.Warn().Err(err).Str("component", component).Msg("failed to release MaaFramework")
		}
	}()

	controller, err := connectAndroidDevice(opts.Address, libDir)
	if err != nil {
		log.Info().Err(err).Str("component", component).Str("address", opts.Address).
			Msg("ADB connection failed, starting configured emulator")
		if strings.TrimSpace(opts.Path) == "" {
			return fmt.Errorf("ADB connection failed and emulator path is empty; configure StartGameEmulatorPath or start the configured device: %w", err)
		}
		if err := launchProgram(opts.Path, opts.Args...); err != nil {
			return fmt.Errorf("launch emulator: %w", err)
		}
		log.Info().Str("component", component).Str("address", opts.Address).
			Dur("wait", opts.Wait).Msg("waiting for emulator startup")
		// Wait the configured duration even when launchProgram matched a running
		// emulator. This is a fixed startup wait, not a device-readiness poll.
		time.Sleep(opts.Wait)
		controller, err = connectAndroidDevice(opts.Address, libDir)
		if err != nil {
			return fmt.Errorf("connect ADB after emulator startup: %w", err)
		}
	} else {
		log.Info().Str("component", component).Str("address", opts.Address).
			Msg("ADB connected, skipping emulator launch and wait")
	}
	defer controller.Destroy()
	if !controller.PostStartApp(opts.Intent).Wait().Success() {
		return fmt.Errorf("start Android app %q on %q failed", opts.Intent, opts.Address)
	}
	return nil
}

// connectAndroidDevice returns an owned connected controller for the exact address.
// Failed controllers are destroyed before an emulator launch or another connection.
func connectAndroidDevice(address, libDir string) (*maa.Controller, error) {
	devices, err := maa.FindAdbDevices()
	if err != nil {
		return nil, fmt.Errorf("discover ADB devices: %w", err)
	}
	device, err := deviceAtAddress(devices, address)
	if err != nil {
		return nil, err
	}
	controller, err := maa.NewAdbController(device.AdbPath, address,
		device.ScreencapMethod, device.InputMethod, device.Config,
		filepath.Join(libDir, "MaaAgentBinary"))
	if err != nil {
		return nil, fmt.Errorf("create ADB controller: %w", err)
	}
	if !controller.PostConnect().Wait().Success() {
		if err := controller.Destroy(); err != nil {
			log.Warn().Err(err).Str("component", component).Msg("failed to destroy disconnected ADB controller")
		}
		return nil, fmt.Errorf("ADB device %q is not ready", address)
	}
	return controller, nil
}

func deviceAtAddress(devices []*maa.AdbDevice, address string) (*maa.AdbDevice, error) {
	if address == "" {
		return nil, fmt.Errorf("ADB address is required")
	}
	for _, device := range devices {
		if device != nil && device.Address == address {
			return device, nil
		}
	}
	return nil, fmt.Errorf("ADB device %q was not found", address)
}
