package launchgamepc

import (
	"encoding/json"
	"errors"
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
	regionTapTap   = "TapTap"
	regionSteam    = "Steam"
	regionEpic     = "Epic"

	gameProcessName = "Endfield.exe"
	launcherExeName = "Launcher.exe"

	pipelineEntry = "GameLauncherMain"

	// 开机自启动时负载高、游戏首次启动较慢，与 MXU 前置程序等待窗口的时长保持一致
	launcherWindowTimeout = 300 * time.Second
	gameWindowTimeout     = 300 * time.Second
	pollInterval          = 5 * time.Second
	// 与 MXU 前置程序 preActionConnectDelaySec 的默认值一致，窗口出现后先等待初始化再连接
	// 启动器过早连接会出现异常画面 https://github.com/MaaEnd/MaaEnd/issues/6585
	launcherWindowSettleWait = 5 * time.Second
	gameWindowSettleWait     = 5 * time.Second
)

// 与 interface.json 中 Win32-Front 的窗口匹配规则保持一致，供 MXU 后续连接。
var (
	gameClassRegex     = regexp.MustCompile(`UnityWndClass`)
	gameWindowRegex    = regexp.MustCompile(`Endfield`)
	launcherClassRegex = regexp.MustCompile(`^Qt\d+QWindowIcon$`)
)

// launcherInfo 描述各区服启动器：uninstallKey 为注册表 Uninstall 下的键名，
// protocolKey 为启动器注册的 URL 协议键名，作为卸载项缺失时的备选；
// launchURL 非空时不查找安装目录，改为打开该链接拉起启动器或游戏（TapTap 版直接运行 Launcher.exe 会提示需要重启应用）；
// windowRegex 匹配启动器主窗口标题（B 服与 TapTap 版相同），为 nil 表示没有启动器（Steam 版直接拉起游戏）；
// settingRegion 为游戏设置所在的注册表区服：B 服、TapTap 与国服共用 Hypergryph 路径，Steam、Epic 与国际服共用 Gryphline 路径。
type launcherInfo struct {
	uninstallKey  string
	protocolKey   string
	launchURL     string
	windowRegex   *regexp.Regexp
	settingRegion string
}

var launchers = map[string]launcherInfo{
	regionCN: {
		uninstallKey:  "33a0a6296a20400d503c59ac0fd6341e",
		protocolKey:   "hypergryphlauncher-abYeZZ16BPluCFyT",
		windowRegex:   regexp.MustCompile(`^鹰角启动器$`),
		settingRegion: regionCN,
	},
	regionGlobal: {
		uninstallKey:  "d9f71f86325c5b9733268efda40413c8",
		protocolKey:   "gryphlinklauncher-TiaytKBUIEdoEwRT",
		windowRegex:   regexp.MustCompile(`^GRYPHLINK$`),
		settingRegion: regionGlobal,
	},
	regionBilibili: {
		uninstallKey:  "d957df99efbdf610162fd47c00ceaba7",
		protocolKey:   "hypergryphlauncher-RhAij5MdwvTWpQ8N",
		windowRegex:   regexp.MustCompile(`^明日方舟：终末地$`),
		settingRegion: regionCN,
	},
	regionTapTap: {
		launchURL:     "taptap://taptap.com/app?app_id=232326&auto_launch=true",
		windowRegex:   regexp.MustCompile(`^明日方舟：终末地$`),
		settingRegion: regionCN,
	},
	regionSteam: {
		launchURL:     "steam://rungameid/4732690",
		settingRegion: regionGlobal,
	},
	regionEpic: {
		launchURL:     "com.epicgames.launcher://apps/14d53b5d3fcb4526a69d59f35c9dd28f%3A6838c695288a4fcea4486285edebcfdf%3Abcd55b0d87c245dd867f5b1bd496f1df?action=launch&silent=true",
		windowRegex:   regexp.MustCompile(`^Arknights: Endfield$`),
		settingRegion: regionGlobal,
	},
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

	if isProcessRunning(gameProcessName) {
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
		err := gamesetting.ApplyAll(info.settingRegion, settings)
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

	// 查找启动器窗口依赖 MaaFramework，需在启动启动器之前初始化
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

	// 无显示器时/部分DP显示器关闭后，系统判断显示器断开，启动器窗口不绘制，所有截图方案均不可用
	if !hasActiveMonitor() {
		log.Warn().
			Str("component", component).
			Msg("未检测到显示器，可能无法识别启动器画面 / No monitor detected, the launcher screen may not be recognized")
	}

	if info.launchURL != "" {
		if err := openLaunchURL(info.launchURL); err != nil {
			log.Error().
				Err(err).
				Str("component", component).
				Str("url", info.launchURL).
				Msg("failed to open launch url")
			return false
		}
	} else if !startLauncherFromInstallDir(opts, info) {
		return false
	}

	if info.windowRegex != nil && !runLauncher(info.windowRegex) {
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

// runLauncher 等待启动器窗口出现后执行启动器 pipeline。
func runLauncher(windowRegex *regexp.Regexp) bool {
	log.Info().
		Str("component", component).
		Str("title", windowRegex.String()).
		Dur("timeout", launcherWindowTimeout).
		Msg("waiting for launcher window")
	// 此处避免使用进程名判断，避免游戏启动期间触发 ACE。
	hWnd, ok := waitWindow(launcherClassRegex, windowRegex, launcherWindowTimeout)
	if !ok {
		log.Error().
			Str("component", component).
			Str("title", windowRegex.String()).
			Msg("launcher window not found")
		return false
	}
	log.Info().
		Str("component", component).
		Str("title", windowRegex.String()).
		Msg("launcher window found")

	log.Info().
		Str("component", component).
		Dur("wait", launcherWindowSettleWait).
		Msg("waiting before connecting launcher window")
	time.Sleep(launcherWindowSettleWait)

	if moved, err := moveLauncherToPrimaryMonitor(hWnd); err != nil {
		log.Warn().
			Err(err).
			Str("component", component).
			Msg("failed to move launcher window to primary monitor")
	} else if moved {
		log.Info().
			Str("component", component).
			Msg("launcher window moved to primary monitor")
	}

	log.Info().
		Str("component", component).
		Str("entry", pipelineEntry).
		Msg("running launcher pipeline")
	return runLauncherPipeline(hWnd)
}

// startLauncherFromInstallDir 按任务设置或注册表找到安装目录并运行 Launcher.exe。
// 启动器窗口已存在时直接复用，不再运行 Launcher.exe，避免启动器窗口进入异常状态。
func startLauncherFromInstallDir(opts launchGamePCOptions, info launcherInfo) bool {
	if info.windowRegex != nil {
		if _, ok := findWindow(launcherClassRegex, info.windowRegex); ok {
			log.Info().
				Str("component", component).
				Str("title", info.windowRegex.String()).
				Msg("launcher window already exists, reuse it")
			return true
		}
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
		dir, err := findLauncherInstallDir(info)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", component).
				Str("region", opts.Region).
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

	// 启动器最小化到托盘时进程仍在但窗口不可见，需要再执行一次 Launcher.exe 唤出窗口。
	if err := startLauncher(installDir); err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Str("install_dir", installDir).
			Msg("failed to start launcher")
		return false
	}
	return true
}

// findLauncherInstallDir 先查 Uninstall 卸载项，找不到再查 URL 协议键。
func findLauncherInstallDir(info launcherInfo) (string, error) {
	var errs []error
	if info.uninstallKey != "" {
		dir, err := gamesetting.FindLauncherInstallDir(info.uninstallKey)
		if err == nil {
			return dir, nil
		}
		errs = append(errs, err)
	}
	dir, err := gamesetting.FindLauncherInstallDirByProtocol(info.protocolKey)
	if err == nil {
		return dir, nil
	}
	errs = append(errs, err)
	return "", errors.Join(errs...)
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

// isProcessRunning 按进程名查找。
func isProcessRunning(name string) bool {
	procs, err := process.Processes()
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", component).
			Msg("failed to enumerate processes")
		return false
	}

	for _, p := range procs {
		procName, err := p.Name()
		if err == nil && strings.EqualFold(procName, name) {
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

// openLaunchURL 交给系统的协议处理程序打开链接；不经过 cmd，避免 & 被解析。
func openLaunchURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Info().
		Str("component", component).
		Str("url", url).
		Msg("launch url opened")
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
		Dur("wait", gameWindowSettleWait).
		Msg("game window found, waiting before connecting")
	time.Sleep(gameWindowSettleWait)
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
	// 鼠标用 Seize（SendInput 真实输入），显示器息屏后点击可唤醒显示器；PostMessage 不算用户输入，无法唤醒
	ctrl, err := maa.NewWin32Controller(hWnd, win32.ScreencapPrintWindow, win32.InputSeize, win32.InputPostMessage)
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
