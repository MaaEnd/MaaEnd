//go:build linux

package exitlauncher

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const component = "ExitGameLauncherAction"

const (
	gameExeName     = "Endfield.exe"
	launcherExeName = "Games.exe"

	// SIGKILL 后轮询确认启动器已被回收，避免短暂残留让 Action 误报失败。
	exitPollInterval = 100 * time.Millisecond
	exitTimeout      = 2 * time.Second
)

var _ maa.CustomActionRunner = &ExitGameLauncherAction{}

// ExitGameLauncherAction 结束与当前游戏实例同属一个 Wine 会话的鹰角启动器（Games.exe）。
// 目标进程由 Endfield.exe 的进程树定位：沿 PPID 上溯到第一个含启动器的祖先，取该祖先子树中的 Games.exe。
// 无参数，供 Linux 版 GameLauncherExit 节点调用。
type ExitGameLauncherAction struct{}

// Run 实现 maa.CustomActionRunner；未发现运行中的游戏或启动器时视为无需处理并返回成功。
func (a *ExitGameLauncherAction) Run(_ *maa.Context, _ *maa.CustomActionArg) bool {
	procs, err := process.Processes()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Msg("failed to enumerate processes")
		return false
	}

	snap := newSnapshot(procs)
	games := snap.findByExeName(gameExeName)
	if len(games) == 0 {
		log.Warn().
			Str("component", component).
			Msg("Endfield.exe is not running, skip exiting launcher")
		return true
	}

	targets := snap.findLaunchers(games)
	if len(targets) == 0 {
		log.Warn().
			Str("component", component).
			Msg("no Games.exe found in the Endfield.exe process tree, skip exiting launcher")
		return true
	}

	ok := true
	for _, info := range targets {
		log.Info().
			Str("component", component).
			Int32("pid", info.proc.Pid).
			Str("cmdline", info.cmdline).
			Msg("killing hypergryph launcher")
		if !killAndWait(info.proc) {
			log.Error().
				Str("component", component).
				Int32("pid", info.proc.Pid).
				Msg("hypergryph launcher did not exit")
			ok = false
		}
	}
	return ok
}

// killAndWait 结束进程并轮询确认其退出；进程已经不存在同样视为成功。
func killAndWait(p *process.Process) bool {
	killErr := p.Kill()
	deadline := time.Now().Add(exitTimeout)
	for {
		running, err := p.IsRunning()
		if err != nil || !running {
			return true
		}
		if !time.Now().Before(deadline) {
			if killErr != nil {
				log.Error().
					Err(killErr).
					Str("component", component).
					Msg("failed to send kill signal")
			}
			return false
		}
		time.Sleep(exitPollInterval)
	}
}

// procInfo 是进程快照中的一项：进程句柄、父进程，以及从 cmdline 解析出的可执行文件名。
type procInfo struct {
	proc    *process.Process
	ppid    int32
	exeName string
	cmdline string
}

// snapshot 缓存一次进程枚举结果，避免在进程树遍历中反复读 /proc。
type snapshot struct {
	infos    map[int32]*procInfo
	children map[int32][]int32
}

func newSnapshot(procs []*process.Process) *snapshot {
	snap := &snapshot{
		infos:    make(map[int32]*procInfo, len(procs)),
		children: make(map[int32][]int32, len(procs)),
	}
	for _, p := range procs {
		ppid, err := p.Ppid()
		if err != nil {
			continue // 进程可能在枚举后退出
		}
		exeName, cmdline := cmdlineOf(p)
		snap.infos[p.Pid] = &procInfo{
			proc:    p,
			ppid:    ppid,
			exeName: exeName,
			cmdline: cmdline,
		}
		snap.children[ppid] = append(snap.children[ppid], p.Pid)
	}
	return snap
}

// cmdlineOf 返回 argv[0] 的文件名与完整命令行。
// Wine 进程的 comm 与 exe 分别被线程名、wine loader 覆盖，只有 cmdline 保留真实程序名。
func cmdlineOf(p *process.Process) (string, string) {
	argv, err := p.CmdlineSlice()
	if err != nil || len(argv) == 0 {
		return "", ""
	}
	argv0 := strings.ReplaceAll(argv[0], `\`, "/")
	return path.Base(argv0), strings.Join(argv, " ")
}

// findByExeName 返回 argv[0] 文件名匹配的进程（忽略大小写），按 PID 升序。
func (s *snapshot) findByExeName(name string) []*procInfo {
	infos := make([]*procInfo, 0, 1)
	for _, info := range s.infos {
		if strings.EqualFold(info.exeName, name) {
			infos = append(infos, info)
		}
	}
	return sortByPID(infos)
}

// findLaunchers 汇总所有游戏实例对应的启动器进程，按 PID 去重升序。
func (s *snapshot) findLaunchers(games []*procInfo) []*procInfo {
	seen := make(map[int32]bool)
	var targets []*procInfo
	for _, game := range games {
		for _, info := range s.launchersOf(game) {
			if !seen[info.proc.Pid] {
				seen[info.proc.Pid] = true
				targets = append(targets, info)
			}
		}
	}
	return sortByPID(targets)
}

// launchersOf 从游戏进程沿 PPID 上溯，返回第一个含启动器的祖先子树中的启动器进程。
// 命中一层即停止上溯，避免搜索范围扩大到无关会话。
func (s *snapshot) launchersOf(game *procInfo) []*procInfo {
	visited := make(map[int32]bool)
	for pid := game.ppid; pid > 1 && !visited[pid]; {
		visited[pid] = true
		var targets []*procInfo
		for _, candidate := range s.descendants(pid) {
			info, ok := s.infos[candidate]
			if !ok || !strings.EqualFold(info.exeName, launcherExeName) {
				continue
			}
			if sameWineSession(info.proc, game.proc) {
				targets = append(targets, info)
			}
		}
		if len(targets) > 0 {
			return targets
		}
		next, ok := s.infos[pid]
		if !ok {
			break
		}
		pid = next.ppid
	}
	return nil
}

// descendants 返回 pid 自身及其全部后代。
func (s *snapshot) descendants(pid int32) []int32 {
	out := []int32{pid}
	for i := 0; i < len(out); i++ {
		out = append(out, s.children[out[i]]...)
	}
	return out
}

func sortByPID(infos []*procInfo) []*procInfo {
	sort.Slice(infos, func(i, j int) bool { return infos[i].proc.Pid < infos[j].proc.Pid })
	return infos
}

// sameWineSession 判断两个进程是否属于同一 Wine/Proton 会话。
// 同一会话的进程共享 WINEPREFIX（wine 层）或 STEAM_COMPAT_DATA_PATH（umu 层）；
// 任一方的环境读不到时不做过滤，按进程树结果处理。
func sameWineSession(a, b *process.Process) bool {
	keysA, okA := sessionKeys(a)
	keysB, okB := sessionKeys(b)
	if !okA || !okB {
		return true
	}
	for key := range keysA {
		if keysB[key] {
			return true
		}
	}
	return false
}

func sessionKeys(p *process.Process) (map[string]bool, bool) {
	env, err := p.Environ()
	if err != nil {
		return nil, false
	}
	keys := make(map[string]bool, 2)
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || value == "" {
			continue
		}
		if name == "WINEPREFIX" || name == "STEAM_COMPAT_DATA_PATH" {
			keys[filepath.Clean(value)] = true
		}
	}
	return keys, true
}
