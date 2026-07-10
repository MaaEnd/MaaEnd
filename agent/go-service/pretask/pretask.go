package pretask

import (
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pretask/gamesetting"
	"github.com/rs/zerolog/log"
)

func init() {
	Register("GameSetting", gamesetting.Run)
}

// Handler 是 pretask 的执行函数：成功返回 true，失败返回 false。
type Handler func(args []string) bool

var registry = map[string]Handler{}

// Register 注册 pretask 处理器。各子任务在 init 中调用。
func Register(name string, handler Handler) {
	registry[name] = handler
}

// Run 处理 `--pretask <taskname> [extra-args...]` 入口。
// 这是 PI 协议 pretask 的通用运行器，在 Controller 启动前作为独立子进程执行
// （如 GameSetting）。它不连接 MaaFramework Agent Socket，而是通过进程退出码向
// Client 反馈结果：成功 0，失败 1。
func Run(args []string) {
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

	handler, ok := registry[taskName]
	if !ok {
		log.Fatal().
			Str("task", taskName).
			Msg("Unknown pretask")
	}

	if !handler(extraArgs) {
		os.Exit(1)
	}

	log.Info().
		Str("task", taskName).
		Msg("Pretask succeeded")
}
