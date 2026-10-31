// Package launchgameplaycover 实现 PlayCover 游戏启动预任务。
package launchgameplaycover

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	component = "launchgameplaycover"
	bundleID  = "com.hypergryph.endfield"
)

// launchOptions 保存游戏应用路径和启动后的等待秒数。
type launchOptions struct {
	AppPath struct {
		Path string `json:"Path"`
	} `json:"LaunchGamePlayCoverAppPath"`
	LaunchDelay struct {
		Seconds string `json:"Seconds"`
	} `json:"LaunchGamePlayCoverLaunchDelay"`
}

// Run 解析预任务选项，打开终末地并等待指定时长，成功时返回 true。
func Run(args []string) bool {
	if runtime.GOOS != "darwin" {
		log.Error().Str("component", component).Msg("PlayCover game launch requires macOS")
		return false
	}
	opts, delay, err := parseOptions(args)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("invalid launch options")
		return false
	}
	bundle, err := gameBundle(opts.AppPath.Path)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("invalid PlayCover game application")
		return false
	}

	// 将应用启动命令的执行时间限制为 90 秒。
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// 在后台打开指定应用，并复用已运行的应用实例。
	cmd := exec.CommandContext(ctx, "/usr/bin/open", "-g", "-a", bundle)
	// 构建启动环境，过滤动态库、Python 和 PyInstaller 相关变量。
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "DYLD_") && !strings.HasPrefix(env, "PYTHON") && !strings.HasPrefix(env, "_PYI") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Error().Err(err).Str("component", component).Str("output", strings.TrimSpace(string(output))).
			Msg("failed to open PlayCover game")
		return false
	}
	log.Info().Str("component", component).Str("path", bundle).Dur("wait", delay).
		Msg("game opened, waiting before connecting")
	// 按配置的时长等待游戏初始化。
	time.Sleep(delay)
	return true
}

// parseOptions 从末尾参数读取 JSON 选项，将 0～300 秒的等待时间转换为时长，默认 10 秒。
func parseOptions(args []string) (launchOptions, time.Duration, error) {
	var opts launchOptions
	opts.LaunchDelay.Seconds = "10"
	if len(args) > 0 {
		raw := strings.TrimSpace(args[len(args)-1])
		if !strings.HasPrefix(raw, "{") {
			return opts, 0, fmt.Errorf("launch options must be a JSON object")
		}
		if err := json.Unmarshal([]byte(raw), &opts); err != nil {
			return opts, 0, fmt.Errorf("parse launch options: %w", err)
		}
	}
	seconds, err := strconv.ParseFloat(opts.LaunchDelay.Seconds, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 300 {
		return opts, 0, fmt.Errorf("launch delay must be between 0 and 300 seconds")
	}
	return opts, time.Duration(seconds * float64(time.Second)), nil
}

// gameBundle 解析应用路径并校验终末地 Bundle ID，返回应用的绝对路径。
func gameBundle(path string) (string, error) {
	path = strings.TrimSpace(path)
	// 空路径使用 PlayCover 默认安装位置，~/ 路径展开为用户主目录。
	if path == "" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "" {
			path = filepath.Join(home, "Library/Containers/io.playcover.PlayCover/Applications", bundleID+".app")
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	bundle, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	bundle, err = filepath.Abs(bundle)
	if err != nil {
		return "", err
	}
	if filepath.Ext(bundle) != ".app" {
		return "", fmt.Errorf("select the PlayCover Endfield .app")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 通过 plutil 读取 XML 或二进制 Info.plist 中的 Bundle ID。
	id, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-",
		filepath.Join(bundle, "Info.plist")).Output()
	if err != nil {
		return "", fmt.Errorf("read game bundle identifier: %w", err)
	}
	if strings.TrimSpace(string(id)) != bundleID {
		return "", fmt.Errorf("select the PlayCover Endfield application (%s)", bundleID)
	}
	return bundle, nil
}
