package killproc

import (
	"encoding/json"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

// Compile-time interface check
var _ maa.CustomActionRunner = &KillProcAction{}

type killProcParam struct {
	// ProcessName 是要结束的进程名称（如 "Endfield.exe"），为空时不杀进程。
	ProcessName string `json:"process_name"`
	// PostStop 若为 true，则在动作成功返回后向 Tasker 发送异步停止信号。
	// 这是针对 MXU __MXU_KILLPROC__ kill_self=true 时序 bug 的 MaaEnd 侧绕过方案：
	// MXU 的实现在自定义动作回调内同步调用 MaaTaskerPostStop，导致动作结果来不及
	// 提交，出现 PipelineTask bad next / completed=false。
	// 本实现先返回 true，再在 goroutine 中延迟调用 PostStop，避免该竞态。
	PostStop bool `json:"post_stop"`
}

// KillProcAction 结束指定进程，并可选地在动作返回后向 Tasker 发送停止信号。
// 注册名称：KillProcess
// 参数示例：
//
//	{"process_name": "Endfield.exe", "post_stop": true}
//	{"process_name": "Endfield.exe"}   // 仅杀进程，不停止 Tasker
//	{"post_stop": true}               // 仅停止 Tasker，不杀进程
type KillProcAction struct{}

// Run kills the specified process and optionally posts a stop signal to the tasker.
func (a *KillProcAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Msg("KillProcess got nil custom action arg")
		return false
	}

	var params killProcParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("param", arg.CustomActionParam).
			Msg("KillProcess failed to parse custom_action_param")
		return false
	}

	if params.ProcessName == "" && !params.PostStop {
		log.Error().Msg("KillProcess requires at least one of: process_name, post_stop")
		return false
	}

	// 先同步结束进程
	if params.ProcessName != "" {
		if !killByName(params.ProcessName) {
			return false
		}
	}

	// 在动作返回后异步请求 Tasker 停止，避免在回调栈内调用 PostStop 导致
	// 动作结果无法提交（MXU kill_self 时序 bug 的根因）。
	if params.PostStop {
		tasker := ctx.GetTasker()
		go func() {
			// 短暂延迟，确保本次动作的返回值已被 MaaFramework 提交
			time.Sleep(300 * time.Millisecond)
			log.Info().Msg("KillProcess: posting tasker stop signal")
			tasker.PostStop()
		}()
	}

	return true
}

// killByName kills all processes whose name exactly matches the given string.
// Returns true if at least one process was killed, or if no matching process was found
// (idempotent: process already gone is treated as success).
// Returns false only on enumeration or kill errors.
func killByName(name string) bool {
	procs, err := process.Processes()
	if err != nil {
		log.Error().
			Err(err).
			Str("process", name).
			Msg("KillProcess failed to enumerate processes")
		return false
	}

	killedAny := false
	for _, p := range procs {
		pName, err := p.Name()
		if err != nil {
			continue
		}
		if pName != name {
			continue
		}
		if err := p.Kill(); err != nil {
			log.Error().
				Err(err).
				Str("process", name).
				Int32("pid", p.Pid).
				Msg("KillProcess failed to kill process")
			return false
		}
		log.Info().
			Str("process", name).
			Int32("pid", p.Pid).
			Msg("KillProcess killed process")
		killedAny = true
	}

	if !killedAny {
		log.Warn().
			Str("process", name).
			Msg("KillProcess: process not found (already gone), treating as success")
	}
	return true
}
