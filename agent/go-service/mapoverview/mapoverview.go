// Package mapoverview 使用在线数据更新已加载的地图总览点击坐标。
// 下载结果在 Agent 内缓存，各任务开始时仅将变化的 target 合入宿主 Resource；不读写本地 JSON。
package mapoverview

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// 在线总览数据源，其中 click 使用 1280×720 基准。
	sourceURL = "https://voice.fz.wiki/output_maaend/region3d_data.json"
	// 与 tools/pipeline-generate/MapOverview 的节点命名保持一致，便于直接定位本地节点。
	nodePrefix = "__ScenePrivateMapOverview_"
)

// download 将远端地区字典转换成“本地节点名 → 点击坐标”，只返回完整解析后的结果。
// 网络、HTTP 状态或已声明字段的类型错误会使整份下载失败；缺失或越界坐标仅跳过对应地图。
// 没有可用坐标时返回空 map；成功结果由调用方作为只读缓存共享。
func download(client *http.Client, now time.Time) (map[string][2]int, error) {
	// 同一五分钟时间段使用相同 URL，让 CDN 复用缓存（例如 12:00、12:05）。
	url := fmt.Sprintf("%s?refresh=%d", sourceURL, now.Truncate(5*time.Minute).Unix())
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("region3d: HTTP %d", response.StatusCode)
	}
	// 按 region → area → click 解码，类型不匹配时直接放弃本次数据。
	// 运行时只需要点击坐标，因此不声明也不校验 name、polygon 等无关字段。
	var source map[string]struct {
		Area map[string]struct {
			Click []*int `json:"click"` // 区分 null 和合法坐标 0。
		} `json:"area"`
	}
	if err := json.NewDecoder(response.Body).Decode(&source); err != nil {
		return nil, err
	}
	points := make(map[string][2]int)
	// 最终是否能应用，由 update 对本地节点存在性和动作类型的检查决定。
	for region, data := range source {
		for level, entry := range data.Area {
			// 先检查长度和空值，再按 1280×720 基准检查边界；短路求值保证解引用安全。
			if len(entry.Click) != 2 || entry.Click[0] == nil || entry.Click[1] == nil ||
				*entry.Click[0] < 0 || *entry.Click[0] >= 1280 || *entry.Click[1] < 0 || *entry.Click[1] >= 720 {
				log.Warn().Str("component", "mapoverview").Str("region", region).Str("map", level).Msg("Skipping invalid click")
				continue
			}
			// 拼出生成器使用的节点名，只缓存坐标，不构造或替换整份节点定义。
			points[nodePrefix+region+"_EnterMap_"+level] = [2]int{*entry.Click[0], *entry.Click[1]}
		}
	}
	return points, nil
}

// sink 监听任务生命周期，持有可跨回调共享的坐标加载函数，不保存框架句柄。
type sink struct {
	// Start 使用 OnceValue 封装此函数，下载完成后所有回调都只读取同一份坐标。
	load func() map[string][2]int
}

var _ maa.TaskerEventSink = (*sink)(nil)

// OnTaskerTask 在任务开始事件中取得宿主当前绑定的资源，并尝试应用坐标更新。
// 这里的任务指宿主通过 Tasker.PostTask 提交的一次执行，不等同于 UI 的一次启动或单个 Pipeline 节点。
// AgentClient 异步转发任务事件，更新允许稍晚于任务开始，不保证赶上立即执行的第一个节点。
// 总览调用前通常还有界面切换，允许秒级更新延迟，因此无需逐节点监听来约束执行时序。
func (s *sink) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, _ maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}
	// 借用的 Resource 只在本回调内有效；只取一次并传给辅助函数，不缓存、不交给 goroutine。
	// 断连或尚未绑定资源时可能返回 nil，此时保留本地行为即可。
	resource := tasker.GetResource()
	if resource == nil {
		return
	}
	// 每次收到 Tasker.Task.Starting 都比较一次缓存坐标；相同坐标不会重复写入。
	s.update(resource)
}

// update 读取 Resource 中实际加载的节点，只汇总需要修改的 target，再一次性合入资源。
// 更新只作用于内存，普通 Pipeline 引用使用更新后的资源；显式 Tasker/Context 覆盖仍有更高优先级。
func (s *sink) update(resource *maa.Resource) {
	patch := make(map[string]any)
	// 首次下载未完成时 load 会等待；下载失败返回 nil，range 会执行零次，无需特殊分支。
	for name, point := range s.load() {
		// 通过框架读取节点，避免依赖开发目录与安装目录中不同的 JSON 路径。
		node, err := resource.GetNode(name)
		// 线上新增地图不自动创建节点，现有 OCR、Success 和 next 均由本地维护。
		if err != nil || node.Action == nil || node.Action.Type != maa.ActionTypeClick {
			continue
		}
		// 使用绑定提供的强类型参数读取 target；无法解释为 Click 参数时保留本地定义。
		click, ok := node.Action.Param.(*maa.ClickParam)
		if !ok {
			continue
		}
		// 只处理固定坐标；引用其他识别结果等动态 target 无法转为矩形时跳过。
		// 比较原始 target 的起点，target_offset 仍由框架按原配置叠加，避免把偏移算两次。
		target, err := click.Target.AsRect()
		if err != nil || (target.X() == point[0] && target.Y() == point[1]) {
			continue
		}
		// 补丁仅包含 target，避免把读取时的整份节点重新写回并覆盖其他配置。
		patch[name] = map[string]any{"action": map[string]any{"param": map[string]any{"target": point}}}
	}
	if len(patch) == 0 {
		return
	}
	// OverridePipeline 按字段合并，保留 next、OCR/Success 流程和其他动作参数。
	// 没有差异时已提前返回；这里将全部变化一次提交，减少跨进程调用。
	if err := resource.OverridePipeline(patch); err != nil {
		log.Warn().Err(err).Str("component", "mapoverview").Msg("Failed to override map overview coordinates")
		return
	}
	log.Info().Str("component", "mapoverview").Int("count", len(patch)).Msg("Updated map overview coordinates")
}
