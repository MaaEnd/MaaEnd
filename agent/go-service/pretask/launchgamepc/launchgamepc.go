package launchgamepc

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/gamesetting"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/maa-framework-go/v4/controller/win32"
	"github.com/bytedance/sonic"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	component = "launchgamepc"

	regionCN       = "CN"
	regionGlobal   = "Global"
	regionBilibili = "Bilibili"

	gameProcessName     = "Endfield.exe"
	launcherProcessName = "Games.exe"
	launcherExeName     = "Launcher.exe"

	pipelineEntry = "GameLauncherMain"

	launcherWindowTimeout = 60 * time.Second
	gameWindowTimeout     = 120 * time.Second
	pollInterval          = 5 * time.Second
)

// 与 interface.json 中 Win32-Front 的窗口匹配规则保持一致，供 MXU 后续连接。
var (
	gameClassRegex     = regexp.MustCompile(`UnityWndClass`)
	gameWindowRegex    = regexp.MustCompile(`Endfield`)
	launcherClassRegex = regexp.MustCompile(`^Qt\d+QWindowIcon$`)
)

// launcherInfo 描述各区服启动器：uninstallKey 为注册表 Uninstall 下的键名，
// windowTitle 为启动器主窗口标题（三个区服各不相同）。
type launcherInfo struct {
	uninstallKey string
	windowTitle  string
}

var launchers = map[string]launcherInfo{
	regionCN:       {uninstallKey: "33a0a6296a20400d503c59ac0fd6341e", windowTitle: "鹰角启动器"},
	regionGlobal:   {uninstallKey: "d9f71f86325c5b9733268efda40413c8", windowTitle: "GRYPHLINK"},
	regionBilibili: {uninstallKey: "d957df99efbdf610162fd47c00ceaba7", windowTitle: "明日方舟：终末地"},
}

type launchGamePCOptions struct {
	Region       string `json:"LaunchGamePCRegion"`
	LauncherPath string `json:"LaunchGamePCLauncherPath"`
	LauncherExe  struct {
		LauncherExe string `json:"LauncherExe"`
	} `json:"LaunchGamePCLauncherExe"`
	GameSetting     string `json:"LaunchGamePCGameSetting"`
	Language        string `json:"LaunchGamePCLanguage"`
	DisplayType     string `json:"LaunchGamePCDisplayType"`
	Resolution      string `json:"LaunchGamePCResolution"`
	GraphicsQuality string `json:"LaunchGamePCGraphicsQuality"`
	FrameRate       string `json:"LaunchGamePCFrameRate"`
	AutoHDR         string `json:"LaunchGamePCAutoHDR"`
}

// settingRegion 把启动器区服映射为游戏设置的注册表区服：B 服与国服共用 Hypergryph 路径。
func settingRegion(region string) string {
	if region == regionGlobal {
		return regionGlobal
	}
	return regionCN
}

// Run 对应 assets/tasks/pretasks/LaunchGamePC.json 的 pretask 入口。
// 启动器界面流程由 pipeline GameLauncher 负责，这里只处理进程、窗口与 MaaFramework 初始化。
func Run(args []string) bool {
	opts := parseOptions(args)

	info, ok := launchers[opts.Region]
	if !ok {
		log.Error().
			Str("component", component).
			Str("region", opts.Region).
			Msg("unknown region")
		return false
	}

	if isProcessRunning(gameProcessName, "") {
		log.Info().
			Str("component", component).
			Msg("game is already running, skip launching")
		return true
	}
	log.Info().
		Str("component", component).
		Msg("game is not running, start launching")

	if opts.GameSetting == "Yes" {
		settings := gamesetting.Settings{
			Language:        opts.Language,
			DisplayType:     opts.DisplayType,
			Resolution:      opts.Resolution,
			GraphicsQuality: opts.GraphicsQuality,
			FrameRate:       opts.FrameRate,
			AutoHDR:         opts.AutoHDR,
		}
		// 设置写入失败不影响启动
		err := gamesetting.ApplyAll(settingRegion(opts.Region), settings)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", component).
				Msg("failed to apply game settings, continue launching")
		} else {
			log.Info().
				Str("component", component).
				Msg("game settings applied")
		}
	} else {
		log.Info().
			Str("component", component).
			Msg("game setting is disabled, skip applying")
	}

	var installDir string
	if opts.LauncherPath == "Manual" {
		launcherPath := filepath.Clean(strings.Trim(strings.TrimSpace(opts.LauncherExe.LauncherExe), `"`))
		_, err := os.Stat(launcherPath)
		if err == nil && !strings.EqualFold(filepath.Base(launcherPath), launcherExeName) {
			err = fmt.Errorf("path is not %s", launcherExeName)
		}
		if err != nil {
			log.Error().
				Err(err).
				Str("component", component).
				Str("launcher_path", launcherPath).
				Msg("填写的 Launcher.exe 路径无效，请检查任务设置中的路径")
			return false
		}
		installDir = filepath.Dir(launcherPath)
	} else {
		dir, err := gamesetting.FindLauncherInstallDir(info.uninstallKey)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", component).
				Str("region", opts.Region).
				Str("uninstall_key", info.uninstallKey).
				Msg("无法找到启动器，请在任务设置中手动填写路径")
			return false
		}
		installDir = dir
	}
	log.Info().
		Str("component", component).
		Str("region", opts.Region).
		Str("install_dir", installDir).
		Msg("launcher install directory found")

	if isProcessRunning(launcherProcessName, installDir) {
		log.Info().
			Str("component", component).
			Str("install_dir", installDir).
			Msg("launcher is already running, skip starting")
	} else if err := startLauncher(installDir); err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Str("install_dir", installDir).
			Msg("failed to start launcher")
		return false
	}

	if err := initMaa(); err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Msg("failed to initialize MAA framework")
		return false
	}
	defer maa.Release()
	log.Info().
		Str("component", component).
		Msg("MAA framework initialized")

	log.Info().
		Str("component", component).
		Str("title", info.windowTitle).
		Dur("timeout", launcherWindowTimeout).
		Msg("waiting for launcher window")
	titleRegex := regexp.MustCompile("^" + regexp.QuoteMeta(info.windowTitle) + "$")
	// 此处避免使用进程名判断，避免游戏启动期间触发 ACE。
	hWnd, ok := waitWindow(launcherClassRegex, titleRegex, launcherWindowTimeout)
	if !ok {
		log.Error().
			Str("component", component).
			Str("title", info.windowTitle).
			Msg("launcher window not found")
		return false
	}
	log.Info().
		Str("component", component).
		Str("title", info.windowTitle).
		Msg("launcher window found")

	log.Info().
		Str("component", component).
		Str("entry", pipelineEntry).
		Msg("running launcher pipeline")
	if !runLauncherPipeline(hWnd) {
		return false
	}

	log.Info().
		Str("component", component).
		Dur("timeout", gameWindowTimeout).
		Msg("waiting for game window")
	if !waitGame(gameWindowTimeout) {
		log.Error().
			Str("component", component).
			Msg("game window not found after launching")
		return false
	}

	log.Info().
		Str("component", component).
		Msg("game launched")
	return true
}

func parseOptions(args []string) launchGamePCOptions {
	opts := launchGamePCOptions{Region: regionCN}
	if len(args) == 0 {
		log.Warn().
			Str("component", component).
			Msg("no options passed, using defaults")
		return opts
	}
	raw := strings.TrimSpace(args[len(args)-1])
	if !strings.HasPrefix(raw, "{") {
		log.Warn().
			Str("component", component).
			Str("arg", raw).
			Msg("last arg is not options JSON, using defaults")
		return opts
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		log.Warn().
			Err(err).
			Str("component", component).
			Msg("failed to parse options, using defaults")
	}
	if opts.Region == "" {
		opts.Region = regionCN
	}
	return opts
}

// isProcessRunning 按进程名查找；dirPrefix 非空时还要求可执行文件位于该目录下，用于区分不同区服的启动器。
func isProcessRunning(name, dirPrefix string) bool {
	procs, err := process.Processes()
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", component).
			Msg("failed to enumerate processes")
		return false
	}

	prefix := strings.ToLower(filepath.Clean(dirPrefix)) + string(filepath.Separator)
	for _, p := range procs {
		procName, err := p.Name()
		if err != nil || !strings.EqualFold(procName, name) {
			continue
		}
		if dirPrefix == "" {
			return true
		}
		exe, err := p.Exe()
		if err == nil && strings.HasPrefix(strings.ToLower(filepath.Clean(exe)), prefix) {
			return true
		}
	}
	return false
}

func startLauncher(installDir string) error {
	exePath := filepath.Join(installDir, launcherExeName)
	if _, err := os.Stat(exePath); err != nil {
		return err
	}

	cmd := exec.Command(exePath)
	cmd.Dir = installDir
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Info().
		Str("component", component).
		Str("path", exePath).
		Msg("launcher started")
	return cmd.Process.Release()
}

func initMaa() error {
	// 与 agent.go 一致：默认取工作目录下的 maafw，宿主可用 MAAFW_BINARY_PATH 覆盖
	libDir := os.Getenv("MAAFW_BINARY_PATH")
	if libDir == "" {
		libDir = fsutil.OutputPath("maafw")
	}
	if err := maa.Init(
		maa.WithLibDir(libDir),
		maa.WithJSONEncoder(sonic.Marshal),
		maa.WithJSONDecoder(sonic.Unmarshal),
	); err != nil {
		return err
	}
	if err := maa.ConfigInitOption(fsutil.OutputPath(), "{}"); err != nil {
		log.Warn().
			Err(err).
			Str("component", component).
			Msg("failed to init toolkit config option")
	}
	return nil
}

func findWindow(classRegex, titleRegex *regexp.Regexp) (unsafe.Pointer, bool) {
	windows, err := maa.FindDesktopWindows()
	if err != nil {
		return nil, false
	}
	for _, w := range windows {
		if classRegex.MatchString(w.ClassName) && titleRegex.MatchString(w.WindowName) {
			return w.Handle, true
		}
	}
	return nil, false
}

func waitWindow(classRegex, titleRegex *regexp.Regexp, timeout time.Duration) (unsafe.Pointer, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if hWnd, ok := findWindow(classRegex, titleRegex); ok {
			return hWnd, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(pollInterval)
	}
}

// waitGame 等待 MXU 将要连接的游戏窗口出现。只枚举窗口，不打开游戏进程句柄，避免游戏启动期间触发 ACE。
func waitGame(timeout time.Duration) bool {
	if _, ok := waitWindow(gameClassRegex, gameWindowRegex, timeout); !ok {
		return false
	}
	log.Info().
		Str("component", component).
		Msg("game window is ready")
	return true
}

func runLauncherPipeline(hWnd unsafe.Pointer) bool {
	if err := runPipeline(hWnd); err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Msg("failed to run launcher pipeline")
		return false
	}
	log.Info().
		Str("component", component).
		Msg("launcher pipeline succeeded")
	return true
}

func runPipeline(hWnd unsafe.Pointer) error {
	ctrl, err := maa.NewWin32Controller(hWnd, win32.ScreencapPrintWindow, win32.InputPostMessage, win32.InputPostMessage)
	if err != nil {
		return err
	}
	defer ctrl.Destroy()
	if !ctrl.PostConnect().Wait().Success() {
		return fmt.Errorf("failed to connect launcher window")
	}

	res, err := maa.NewResource()
	if err != nil {
		return err
	}
	defer res.Destroy()
	resourceDir := fsutil.OutputPath("resource")
	if !res.PostBundle(resourceDir).Wait().Success() {
		return fmt.Errorf("failed to load resource: %s", resourceDir)
	}

	tasker, err := maa.NewTasker()
	if err != nil {
		return err
	}
	defer tasker.Destroy()
	if err := tasker.BindController(ctrl); err != nil {
		return err
	}
	if err := tasker.BindResource(res); err != nil {
		return err
	}

	if !tasker.PostTask(pipelineEntry).Wait().Success() {
		return fmt.Errorf("pipeline %s failed", pipelineEntry)
	}
	return nil
}
