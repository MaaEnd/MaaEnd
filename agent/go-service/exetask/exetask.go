package exetask

import (
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/exetask/gamesetting"
	"github.com/rs/zerolog/log"
)

func init() {
	Register("GameSetting", gamesetting.Run)
}

// Handler 是外部子任务的执行函数：成功返回 true，失败返回 false。
type Handler func(args []string) bool

var registry = map[string]Handler{}

// Register 注册外部子任务。各子任务在 init 中调用。
func Register(name string, handler Handler) {
	registry[name] = handler
}

// Run 处理 `--exetask <taskname> [extra-args...]` 入口。
// 这是一个通用的外部子任务运行器，可服务于 PI 协议中的 pretask（Controller 启动前
// 执行，如 GameSetting）以及 exec_task（由 Client 自由调度）两类外部程序任务。
// 区别于 Agent 模式：它不连接 MaaFramework Agent Socket，而是作为独立子进程运行，
// 通过进程退出码向 Client 反馈结果：成功 0，失败 1。
func Run(args []string) {
	if len(args) < 1 {
		log.Fatal().
			Msg("Usage: go-service --exetask <taskname> [args...]")
	}

	taskName := args[0]
	extraArgs := args[1:]

	log.Info().
		Str("task", taskName).
		Strs("args", extraArgs).
		Msg("Exec task invoked")

	handler, ok := registry[taskName]
	if !ok {
		log.Fatal().
			Str("task", taskName).
			Msg("Unknown exec task")
	}

	if !handler(extraArgs) {
		os.Exit(1)
	}

	log.Info().
		Str("task", taskName).
		Msg("Exec task succeeded")
}
