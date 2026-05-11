package main

import (
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/gamesetting"
	"github.com/rs/zerolog/log"
)

// pretaskHandler 是 pretask 任务的执行函数：成功返回 true，失败返回 false。
type pretaskHandler func(args []string) bool

// pretaskRegistry 把命令行任务名映射到具体实现。新增 pretask 时只需在此注册。
var pretaskRegistry = map[string]pretaskHandler{
	"gamesetting_save": func(args []string) bool {
		_ = args
		return gamesetting.Save()
	},
	"gamesetting_recover": func(args []string) bool {
		_ = args
		return gamesetting.Recover()
	},
}

// runPretask 处理 `--pretask <taskname> [extra-args...]` 入口。
// 区别于 Agent 模式：它不连接 MaaFramework Agent Socket，而是作为独立子进程在
// Client（MaaPiCli/MXU）正式发起 Tasker 任务前完成必要的前置工作（如环境校验、
// 资源准备、外部 IO 等）。pretask 通过进程退出码向 Client 反馈结果：成功 0，失败 1。
func runPretask(args []string) {
	if len(args) < 1 {
		log.Fatal().
			Msg("Usage: go-service --pretask <taskname> [args...]")
	}

	taskName := args[0]
	extraArgs := args[1:]

	log.Info().
		Str("task", taskName).
		Strs("args", extraArgs).
		Msg("Pretask invoked")

	handler, ok := pretaskRegistry[taskName]
	if !ok {
		log.Fatal().
			Str("task", taskName).
			Msg("Unknown pretask")
	}

	if !handler(extraArgs) {
		log.Error().
			Str("task", taskName).
			Msg("Pretask failed")
		os.Exit(1)
	}

	log.Info().
		Str("task", taskName).
		Msg("Pretask succeeded")
}
