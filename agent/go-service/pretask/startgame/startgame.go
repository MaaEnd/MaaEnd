// Package startgame launches the configured PC program or Android app before
// the client connects its controller. In-game navigation belongs to Pipeline.
package startgame

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const component = "startgame"

type pathOption struct {
	Path string `json:"Path"`
	Args string `json:"Args"`
}

type startGameOptions struct {
	Emulator *pathOption `json:"StartGameEmulatorPath"`
	PC       *pathOption `json:"StartGamePCPath"`
	ADB      struct {
		Address     string `json:"Address"`
		WaitSeconds string `json:"WaitSeconds"`
	} `json:"StartGameADB"`
	ClientVersion string `json:"ClientVersion"`
	CloudVersion  string `json:"ClientVersionCloudLocked"`
}

type launchOptions struct {
	Path    string
	Args    []string
	Address string
	Wait    time.Duration
	Intent  string
}

// Run handles the StartGame pretask. The client appends option values as JSON;
// ADB connects first and attempts emulator startup only when connection fails.
// Success means the PC process was started or matched, or the Android start-app
// job succeeded; it does not confirm that the game is ready for interaction.
func Run(args []string) bool {
	opts, err := optionsFromArgs(args, pienv.ControllerType())
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("invalid launch options")
		return false
	}

	if opts.Intent == "" {
		if err := launchProgram(opts.Path, opts.Args...); err != nil {
			log.Error().Err(err).Str("component", component).
				Str("path", opts.Path).Msg("failed to launch program")
			return false
		}
		return true
	}

	if err := startAndroidGame(opts); err != nil {
		log.Error().Err(err).Str("component", component).
			Str("address", opts.Address).Msg("failed to launch Android game")
		return false
	}
	log.Info().Str("component", component).Str("address", opts.Address).
		Str("intent", opts.Intent).Msg("Android game launched")
	return true
}

// launchProgram skips startup when an existing process matches the executable
// path and any supplied arguments. It returns without waiting for a window.
// In the ADB fallback branch, the caller still waits and reconnects after a match.
func launchProgram(path string, args ...string) error {
	path, err := resolveExecutable(path)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	running, err := isProgramRunning(path, args...)
	if err != nil {
		return err
	}
	if running {
		log.Info().Str("component", component).Str("path", path).
			Msg("program is already running, skipping launch")
		return nil
	}

	// Use the executable directly: paths containing spaces are not shell commands.
	// Leave stdio disconnected so the client can finish waiting for this pretask.
	cmd := exec.Command(path, args...)
	cmd.Dir = filepath.Dir(path)
	configureCommand(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	// The launched application outlives this short-lived pretask process.
	if err := cmd.Process.Release(); err != nil {
		log.Warn().Err(err).Str("component", component).
			Int("pid", pid).Msg("failed to release launched process handle")
	}
	log.Info().Str("component", component).Str("path", path).
		Int("pid", pid).Msg("program launched")
	return nil
}

func optionsFromArgs(args []string, controllerType string) (launchOptions, error) {
	var result launchOptions
	if len(args) == 0 {
		return result, fmt.Errorf("missing StartGame options")
	}
	var opts startGameOptions
	if err := json.Unmarshal([]byte(args[len(args)-1]), &opts); err != nil {
		return result, fmt.Errorf("parse StartGame options: %w", err)
	}

	controllerType = strings.ToLower(strings.TrimSpace(controllerType))
	if controllerType == "" {
		// When the pretask has no PI controller type, infer it from the selected
		// path option. This requires the client to filter options by controller.
		switch {
		case opts.PC != nil && opts.Emulator == nil:
			controllerType = "win32"
		case opts.Emulator != nil && opts.PC == nil:
			controllerType = "adb"
		default:
			return result, fmt.Errorf("cannot determine controller from StartGame options")
		}
	}
	var selected *pathOption
	switch controllerType {
	case "win32":
		selected = opts.PC
	case "adb":
		selected = opts.Emulator
	default:
		return result, fmt.Errorf("unsupported PI controller type: %q", controllerType)
	}
	if selected == nil {
		return result, fmt.Errorf("missing executable path for controller %q", controllerType)
	}
	path := strings.TrimSpace(selected.Path)
	// Windows' Copy as path includes a pair of quotes around the filename.
	if len(path) >= 2 && strings.HasPrefix(path, `"`) && strings.HasSuffix(path, `"`) {
		path = path[1 : len(path)-1]
	}
	if controllerType == "win32" && strings.TrimSpace(path) == "" {
		return result, fmt.Errorf("executable path is empty for controller %q; configure StartGamePCPath", controllerType)
	}
	result.Path = path
	var err error
	result.Args, err = parseArguments(selected.Args)
	if err != nil {
		return result, fmt.Errorf("parse launch arguments: %w", err)
	}
	if controllerType == "win32" {
		return result, nil
	}

	result.Address = strings.TrimSpace(opts.ADB.Address)
	if result.Address == "" {
		return result, fmt.Errorf("ADB address is required")
	}
	wait := strings.TrimSpace(opts.ADB.WaitSeconds)
	if wait == "" {
		wait = "30"
	}
	seconds, err := strconv.ParseUint(wait, 10, 64)
	if err != nil || seconds > uint64((1<<63-1)/time.Second) {
		return result, fmt.Errorf("invalid ADB wait seconds: %q", wait)
	}
	result.Wait = time.Duration(seconds) * time.Second
	version := opts.ClientVersion
	if opts.CloudVersion != "" {
		version = opts.CloudVersion
	}
	result.Intent, err = androidIntent(version)
	return result, err
}

func resolveExecutable(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("executable path is not a regular file: %q", absPath)
	}
	return absPath, nil
}

// isProgramRunning compares executable paths and, when provided, arguments.
// Another installation or instance must not suppress this program's launch.
func isProgramRunning(path string, args ...string) (bool, error) {
	procs, err := process.Processes()
	if err != nil {
		return false, fmt.Errorf("enumerate processes: %w", err)
	}
	for _, proc := range procs {
		exe, err := proc.Exe()
		if err != nil {
			// Unrelated protected processes must not block launching. If a process
			// has the target's name but its path is inaccessible, do not risk a duplicate.
			name, nameErr := proc.Name()
			if nameErr == nil && samePath(name, filepath.Base(path)) {
				if running, runningErr := proc.IsRunning(); runningErr == nil && !running {
					continue
				}
				return false, fmt.Errorf("read executable path of process %d: %w", proc.Pid, err)
			}
			continue
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if samePath(exe, path) {
			if len(args) == 0 {
				return true, nil
			}
			commandLine, err := proc.CmdlineSlice()
			if err != nil {
				if running, runningErr := proc.IsRunning(); runningErr == nil && !running {
					continue
				}
				return false, fmt.Errorf("read arguments of process %d: %w", proc.Pid, err)
			}
			if len(commandLine) > 0 && slices.Equal(commandLine[1:], args) {
				return true, nil
			}
		}
	}
	return false, nil
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
