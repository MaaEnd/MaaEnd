//go:build linux

package ziplineimport

import (
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
)

const (
	kPollIntervalMs = 200
	// 抓到真实标记后：全部标定图都覆盖，静默这么久就提前收工（给同批次最后几条留余量）。
	kSettleMs = 1200
	// 抓到了一些但凑不齐标定图：静默这么久也收工，不干等满 timeout。
	kIdleCloseMs = 20000
)

// runCapture 用临时 profile 拉起可见 Firefox（经受限代理），等页面自动/人工登录后，页面
// 自己拉取各图的 mark/list，代理抄下响应体。完成判据与本仓库 win32/WebView2 参考一致：
//   - 真实标记出现且所有标定图都覆盖，静默 1.2s 后提前收工；
//   - 有真实标记但凑不齐标定图，静默 20s 后收工（日志列出缺图）；
//   - 用户关窗 / 任务停止 / 超时：同样收工，已抓到的按部分成功处理。
//
// 返回是否至少有一张图出现了真实标记。
func runCapture(ctx *maa.Context, p actionParam, profileDir string, proxy *mitmProxy, expected map[string]bool) bool {
	cmd := exec.Command(p.Firefox, "-profile", profileDir, "--no-remote", "--new-instance", p.URL)
	// 自成进程组，便于超时/停止时把整棵树一起杀干净。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("firefox", p.Firefox).
			Msg("zipline import: failed to launch firefox for login/capture")
		return false
	}
	defer killProcessGroup(cmd)

	maafocus.Print(ctx, i18n.T("ziplineimport.sign_in_hint"))
	log.Info().Str("component", componentName).Str("url", p.URL).
		Msg("zipline import: waiting for sign-in and mark capture")

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	windowClosed := false
	signinHintLogged := false
	loginOkNoticed := false

	deadline := time.Now().Add(time.Duration(p.Timeout) * time.Millisecond)
	for {
		before := time.Now()
		covered := proxy.coveredMapSnapshot()
		capturedN := proxy.capturedCount()
		lastEvent := proxy.lastEventAt()

		if len(covered) > 0 {
			if !loginOkNoticed {
				loginOkNoticed = true
				maafocus.Print(ctx, i18n.T("ziplineimport.login_ok"))
				log.Info().Str("component", componentName).Msg("zipline import: login detected, importing")
			}
			quiet := time.Since(lastEvent)
			if allCovered(expected, covered) && quiet >= time.Duration(kSettleMs)*time.Millisecond {
				log.Info().Str("component", componentName).
					Msg("zipline import: all calibrated maps captured, closing")
				return true
			}
			if quiet >= time.Duration(kIdleCloseMs)*time.Millisecond {
				log.Warn().Str("component", componentName).Str("missing", missingMaps(expected, covered)).
					Msg("zipline import: closing without every calibrated map")
				return true
			}
		} else if capturedN > 0 && !signinHintLogged &&
			time.Since(lastEvent) >= time.Duration(kIdleCloseMs)*time.Millisecond {
			// 有 mark/list 响应但一个可归属账号的真实标记都没有（未登录时 saveMarks 为空
			// 数组；已登录但还没选角色时响应里也没有 roleId）。
			signinHintLogged = true
			log.Info().Str("component", componentName).
				Msg("zipline import: no account-scoped marks yet, waiting for sign-in or role selection")
		}

		// 超时即收工，已抓到的按部分成功处理（与 win32 参考一致：有数据就算抓到了）。
		if !time.Now().Before(deadline) {
			log.Warn().Str("component", componentName).Int64("timeout_ms", p.Timeout).
				Msg("zipline import: timed out waiting for the mark list")
			return len(covered) > 0
		}
		if ctx != nil && ctx.GetTasker() != nil && ctx.GetTasker().Stopping() {
			log.Info().Str("component", componentName).Msg("zipline import: task stopping, abort capture")
			return len(covered) > 0
		}
		select {
		case <-waitDone:
			windowClosed = true
		default:
		}
		if windowClosed {
			log.Info().Str("component", componentName).Msg("zipline import: window closed by user")
			return len(covered) > 0
		}

		// 保证最低轮询节拍，避免忙等。
		if elapsed := time.Since(before); elapsed < time.Duration(kPollIntervalMs)*time.Millisecond {
			time.Sleep(time.Duration(kPollIntervalMs)*time.Millisecond - elapsed)
		}
	}
}

// allCovered 返回 expected 里每张图都已被 covered 覆盖（expected 为空视为不满足，
// 不触发「抓齐即提前关」的快路径，与 cpp 一致）。
func allCovered(expected, covered map[string]bool) bool {
	if len(expected) == 0 {
		return false
	}
	for id := range expected {
		if !covered[id] {
			return false
		}
	}
	return true
}

// missingMaps 列出 expected 里尚未被 covered 覆盖的图（用于收工告警）。
func missingMaps(expected, covered map[string]bool) string {
	var parts []string
	for id := range expected {
		if !covered[id] {
			parts = append(parts, id)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
