package mapoverview

import (
	"net/http"
	"sync"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Start 启用地图总览点击坐标的在线更新。
// 应在 MaaFramework 初始化后、AgentServer 启动前调用，每次 Agent 运行只调用一次。
func Start() {
	// 后台预下载和任务回调共用同一个 load：并发调用只执行一次下载，
	// 其他调用等待下载结束，再取得完整结果。结果 map 发布后只读，不需要额外加锁。
	load := sync.OnceValue(func() map[string][2]int {
		// 超时限制首次任务回调等待下载的时间，避免网络故障让 Agent 长时间无响应。
		points, err := download(&http.Client{Timeout: 10 * time.Second}, time.Now())
		if err != nil {
			log.Warn().Err(err).Str("component", "mapoverview").Msg("Failed to download map overview coordinates; using local coordinates")
		}
		// 失败时的 nil 也会被 OnceValue 缓存，本次 Agent 运行期间不会自动重试下载。
		// 消费方只遍历结果，nil map 与空 map 都会自然跳过覆盖，继续使用本地坐标。
		return points
	})
	// 资源由宿主加载，启动时尚未取得其句柄；任务事件能提供绑定了资源的 Tasker。
	// 在任务开始时统一更新资源，普通 Pipeline 引用即可使用新坐标，无需新增调用节点。
	maa.AgentServerAddTaskerSink(&sink{load: load})
	// 提前发起请求；即使首个任务先到达，也会通过同一个 load 等待，避免重复下载。
	// 后台 goroutine 只处理 HTTP 和普通 Go 数据，不持有任何 MaaFramework 借用句柄。
	go load()
}
