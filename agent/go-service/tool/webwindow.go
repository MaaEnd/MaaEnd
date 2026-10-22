// Package tool 提供由 GUI（MXU 的 PI 扩展 `tools`）点按钮触发的工具命令。
//
// 目前只有一组：`--tool web-window <action>`，用来操作「云·终末地」网页端的浏览器窗口。
// 之所以放在 go-service 里而不是 Python：GUI 点按钮时 exec 的是随包分发的二进制，
// 不能假设用户机器上有 Python 环境。
//
// 后端有两种，命令按"哪个实例活着"自动选：
//   - WebView2 宿主：读 %LOCALAPPDATA%\MaaEnd\webview2-profile\host-state.json，
//     通过它的本地控制口（按行 JSON）执行 show/park/hide/quit；
//   - 系统 Edge：读 %LOCALAPPDATA%\MaaEnd\web-profile\backend.json，
//     用 Win32 直接改窗口位置/扩展样式，close 用 WM_CLOSE（Chromium 会自己 flush profile）。
package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const usage = `Usage: go-service --tool web-window <action>

action:
  show          把窗口摆到屏幕上（默认 120,120）
  hide          藏回后台（WebView2 用 park：仍渲染，自动化可继续）
  close         优雅关闭（WebView2 = quit；Edge = WM_CLOSE）
  taskbar-on    出现在任务栏 / Alt+Tab
  taskbar-off   改回工具窗口（对用户隐形）
  status        打印当前实例与窗口状态（JSON）`

// Run 是 --tool 的入口，返回进程退出码。
func Run(args []string) int {
	if len(args) < 2 || args[0] != "web-window" {
		fmt.Println(usage)
		return 2
	}
	action := args[1]
	code, err := webWindow(action)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web-window %s 失败: %v\n", action, err)
		return 1
	}
	fmt.Println(code)
	return 0
}

type hostState struct {
	PID         int    `json:"pid"`
	CDPPort     int    `json:"cdp_port"`
	ControlPort int    `json:"control_port"`
	Profile     string `json:"profile"`
	URL         string `json:"url"`
	Visible     bool   `json:"visible"`
	Offscreen   bool   `json:"offscreen"`
	Runtime     string `json:"runtime"`
	Started     string `json:"started"`
}

type backendState struct {
	Kind    string `json:"kind"`
	Mode    string `json:"mode"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Exe     string `json:"exe"`
	Profile string `json:"profile"`
	Started string `json:"started"`
}

func dataDir() string {
	if d := os.Getenv("MAAEND_WEB_DATA"); d != "" {
		return d
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "MaaEnd")
	}
	return "."
}

// webWindow 找到活着的实例并执行动作，返回给调用方打印的结果文本。
func webWindow(action string) (string, error) {
	dpiAware()
	root := dataDir()
	// 顺序很重要：webview2 优先（它的 hide 是"真停车"），Edge 次之
	if hs, ok := loadHost(root); ok {
		return viaHost(hs, action)
	}
	if bs, ok := loadBackend(root); ok {
		return viaWin32(bs, action)
	}
	return "", fmt.Errorf("没找到正在运行的后端（%s 下没有 host-state.json / backend.json）", root)
}

func loadHost(root string) (hostState, bool) {
	path := filepath.Join(root, "webview2-profile", "host-state.json")
	var s hostState
	if !readJSON(path, &s) {
		return s, false
	}
	if !pidAlive(s.PID) {
		return s, false
	}
	return s, true
}

func loadBackend(root string) (backendState, bool) {
	path := filepath.Join(root, "web-profile", "backend.json")
	var s backendState
	if !readJSON(path, &s) {
		return s, false
	}
	if !pidAlive(s.PID) {
		return s, false
	}
	return s, true
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// viaHost 走 WebView2 宿主的本地控制口（按行 JSON，一问一答）。
func viaHost(hs hostState, action string) (string, error) {
	var payload map[string]any
	switch action {
	case "show":
		payload = map[string]any{"cmd": "show", "left": 120, "top": 120}
	case "hide":
		// park = 屏幕外但继续渲染：自动化还能跑，用户看不见
		payload = map[string]any{"cmd": "park"}
	case "hide-hard":
		payload = map[string]any{"cmd": "hide"}
	case "close":
		payload = map[string]any{"cmd": "quit"}
	case "status":
		payload = map[string]any{"cmd": "status"}
	case "taskbar-on", "taskbar-off":
		if err := setTaskbar(hs.PID, action == "taskbar-on"); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"ok":true,"action":%q,"via":"win32"}`, action), nil
	default:
		return "", fmt.Errorf("未知动作 %q", action)
	}
	reply, err := controlCall(hs.ControlPort, payload)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(reply)
	return string(out), nil
}

func controlCall(port int, payload map[string]any) (map[string]any, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连不上控制口 %d: %w", port, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	b, _ := json.Marshal(payload)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	var reply map[string]any
	if err := json.Unmarshal(line, &reply); err != nil {
		return nil, fmt.Errorf("控制口回包不是 JSON: %q", string(line))
	}
	return reply, nil
}

// viaWin32 直接操作系统 Edge 的窗口。close 用 WM_CLOSE，让 Chromium 自己落盘 profile。
func viaWin32(bs backendState, action string) (string, error) {
	hwnd := findMainWindow(uint32(bs.PID))
	if hwnd == 0 {
		return "", fmt.Errorf("没找到 pid=%d 的窗口（可能已最小化到屏幕外或已退出）", bs.PID)
	}
	switch action {
	case "show":
		moveWindow(hwnd, 120, 120)
	case "hide", "hide-hard":
		moveWindow(hwnd, -32000, -32000)
	case "close":
		// WM_CLOSE 让 Chromium 自己收尾（profile 落盘），比强杀安全；
		// 但 Edge 关完窗口后进程有时还挂着（后台任务），所以给一段时间再兜底强杀，
		// 并在输出里说明是 graceful 还是 forced——强杀有可能丢掉刚写的登录态。
		if err := postMessage(hwnd, wmClose); err != nil {
			return "", err
		}
		graceful := waitExit(uint32(bs.PID), 8*time.Second)
		if !graceful {
			_ = exec.Command("taskkill", "/PID", itoa(bs.PID), "/T", "/F").Run()
		}
		out := map[string]any{
			"ok": true, "action": action, "via": "win32", "pid": bs.PID,
			"closed": map[bool]string{true: "graceful", false: "forced"}[graceful],
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	case "status":
		// 交给下面统一打印
	case "taskbar-on":
		if err := setTaskbarHandle(hwnd, true); err != nil {
			return "", err
		}
	case "taskbar-off":
		if err := setTaskbarHandle(hwnd, false); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("未知动作 %q", action)
	}
	left, top, w, h := windowRect(hwnd)
	ex, _ := getWindowLongPtr(hwnd, gwlExStyle)
	out := map[string]any{
		"ok": true, "action": action, "via": "win32", "pid": bs.PID,
		"hwnd": hwnd, "rect": []int32{left, top, w, h},
		"exstyle": fmt.Sprintf("0x%08X", uint32(ex)),
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func setTaskbar(pid int, on bool) error {
	hwnd := findMainWindow(uint32(pid))
	if hwnd == 0 {
		return fmt.Errorf("没找到 pid=%d 的窗口", pid)
	}
	return setTaskbarHandle(hwnd, on)
}

const (
	gwlExStyle     = -20
	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
	wmClose        = 0x0010
	swpNoSize      = 0x0001
	swpNoMove      = 0x0002
	swpNoZOrder    = 0x0004
	swpNoActivate  = 0x0010
	swpFrameChange = 0x0020
	swHide         = 0
	swShowNoActive = 4
)

func setTaskbarHandle(hwnd windows.Handle, on bool) error {
	ex, err := getWindowLongPtr(hwnd, gwlExStyle)
	if err != nil {
		return err
	}
	next := ex
	if on {
		next = (ex &^ wsExToolWindow) | wsExAppWindow
	} else {
		next = (ex &^ wsExAppWindow) | wsExToolWindow
	}
	if err := setWindowLongPtr(hwnd, gwlExStyle, next); err != nil {
		return err
	}
	// 任务栏条目是窗口创建时登记的：藏一下再显示才会刷新
	_, _, _ = procShowWindow.Call(uintptr(hwnd), swHide)
	_, _, _ = procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChange)
	_, _, _ = procShowWindow.Call(uintptr(hwnd), swShowNoActive)
	return nil
}

// waitExit 等到进程退出或超时，返回是否真的退出了。
func waitExit(pid uint32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidAlive(int(pid)) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func itoa(v int) string { return strconv.Itoa(v) }

func moveWindow(hwnd windows.Handle, x, y int32) {
	_, _, w, h := windowRect(hwnd)
	_, _, _ = procSetWindowPos.Call(uintptr(hwnd), 0,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoZOrder|swpNoActivate)
}

func windowRect(hwnd windows.Handle) (x, y, w, h int32) {
	var r struct{ Left, Top, Right, Bottom int32 }
	_, _, _ = procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r.Left, r.Top, r.Right - r.Left, r.Bottom - r.Top
}

// findMainWindow 按 pid 找它可见、面积最大的顶层窗口。
func findMainWindow(pid uint32) windows.Handle {
	type hit struct {
		area int32
		hwnd windows.Handle
	}
	var hits []hit
	cb := windows.NewCallback(func(hwnd windows.Handle, _ uintptr) uintptr {
		var wpid uint32
		_, _, _ = procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1
		}
		vis, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
		if vis == 0 {
			return 1
		}
		_, _, w, h := windowRect(hwnd)
		if w > 0 && h > 0 {
			hits = append(hits, hit{area: w * h, hwnd: hwnd})
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	if len(hits) == 0 {
		return 0
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].area > hits[j].area })
	return hits[0].hwnd
}

// GetWindowLongPtr/SetWindowLongPtr 在 x/sys/windows 里没有，自己取（amd64/arm64 用 Ptr 版）。
// Win32 调用成功时也会返回非 nil 的 errno（"The operation completed successfully."），
// 所以必须拿 errno 数值判断，不能看 err != nil。
func winErr(err error) error {
	if err == nil {
		return nil
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return nil
	}
	return err
}

func getWindowLongPtr(hwnd windows.Handle, index int32) (int64, error) {
	r, _, err := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index))
	if r == 0 {
		if e := winErr(err); e != nil {
			return 0, e
		}
	}
	return int64(r), nil
}

func setWindowLongPtr(hwnd windows.Handle, index int32, value int64) error {
	r, _, err := procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index), uintptr(value))
	if r == 0 {
		return winErr(err)
	}
	return nil
}

func postMessage(hwnd windows.Handle, msg uintptr) error {
	r, _, err := procPostMessageW.Call(uintptr(hwnd), msg, 0, 0)
	if r == 0 {
		return winErr(err)
	}
	return nil
}

// dpiAware 让本进程按物理像素取坐标。不做的话在 150% 缩放的屏上，
// GetWindowRect/SetWindowPos 都会被 Windows 虚拟化（实测 -32000 读成 -21333）。
func dpiAware() {
	_, _, _ = user32.NewProc("SetProcessDpiAwarenessContext").Call(uintptr(^uintptr(3))) // PER_MONITOR_AWARE_V2 = -4
}

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)
