package seizedeliveryjobs

import (
	"encoding/json"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type deliveryJobItem struct {
	RewardBox       []int  `json:"reward_box"`
	OriginText      string `json:"origin_text"`
	AcceptBox       []int  `json:"accept_box"`
	ViewLocationBox []int  `json:"view_location_box"`
}

// filteredDetail 保存解析后的 OCR 子识别结果。
// 只有 origin（索引 1）会填充 Text 字段，其余结果保持零值。
type filteredDetail struct {
	Filtered []struct {
		Box   []int   `json:"box"`
		Score float64 `json:"score"`
		Text  string  `json:"text"`
	} `json:"filtered"`
}

// defaultMaxAttemptRounds 是「尝试轮次上限」的兜底值，任务选项未覆盖时使用。
const defaultMaxAttemptRounds = 100

var (
	scannedJobItems []deliveryJobItem
	currentIndex    int
	// attemptRounds 记录「自本次进入抢单入口以来，尚未成功接到委托的轮次」。
	// 它跨轮累计，不会被单轮扫描状态的清理影响；只在任务入口
	// （SeizeDeliveryJobsMain，即每次抢单尝试重新计时）归零。
	//
	// 一轮的定义是「一次列表刷新到下一次列表刷新」，以下三种情况都算一轮未接到：
	// ① 列表里没有价格达标的委托；② 有达标委托但终点均不匹配；③ 匹配到终点但接取失败。
	// 三条路径都汇入 SeizeDeliveryJobsNoProgress 节点统一计数，达到上限后终止任务，
	// 避免任一情况退化成无限刷新重扫。
	attemptRounds int
)

// clearRoundState 只清「单轮扫描状态」，保留跨轮计数。
func clearRoundState() {
	scannedJobItems = nil
	currentIndex = 0
}

// resetScanState 清空单轮扫描状态与跨轮计数。
func resetScanState() {
	clearRoundState()
	attemptRounds = 0
}

// boxToRect 将 [x, y, w, h] 格式的 box 切片转换为 maa.Rect。
func boxToRect(box []int) maa.Rect {
	return maa.Rect{box[0], box[1], box[2], box[3]}
}

// SeizeDeliveryJobsResetScanStateAction 清空单轮扫描状态与跨轮计数。
// 只挂在任务入口 SeizeDeliveryJobsMain：每次进入抢单入口视为一次新的抢单尝试，
// 重新获得完整的尝试轮次预算。中途的成功接单路径会经 AutoDelivery 跳回入口，
// 因此无需在接单成功处另行归零。
type SeizeDeliveryJobsResetScanStateAction struct{}

// Run 清空缓存列表，并重置跨轮尝试计数。
func (a *SeizeDeliveryJobsResetScanStateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	resetScanState()
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "reset_scan_state").
		Msg("scan state cleared")
	return true
}

// SeizeDeliveryJobsScanTargetRecognition 单次扫描委托列表，并缓存后续 ScanTarget 迭代所需的
// 全部报酬达标委托。
type SeizeDeliveryJobsScanTargetRecognition struct{}

// Run 单次扫描委托列表，并缓存全部符合条件的委托。
func (r *SeizeDeliveryJobsScanTargetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 后续调用：已有扫描数据，直接命中。
	if scannedJobItems != nil {
		log.Debug().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Int("remaining", len(scannedJobItems)-currentIndex).
			Msg("reusing existing scan data")
		return &maa.CustomRecognitionResult{
			Box: arg.Roi,
		}, true
	}

	minReward, err := readMinReward(ctx)
	if err != nil {
		log.Error().Err(err).
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Msg("read min reward")
		return nil, false
	}

	items, err := scanJobs(ctx, arg.Img, minReward)
	if err != nil {
		log.Error().Err(err).
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Msg("scan jobs")
		return nil, false
	}
	if len(items) == 0 {
		// 本轮没有任何价格达标的委托。返回命中，让 ScanTargetAction 走
		// 「全部扫完」分支进入 NoProgress，与终点不匹配共用同一套计数。
		log.Warn().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Float64("min_reward", minReward).
			Msg("no reward-qualified job in list")
		clearRoundState()
		return &maa.CustomRecognitionResult{
			Box: arg.Roi,
		}, true
	}
	scannedJobItems = items

	origins := make([]string, 0, len(items))
	for _, it := range items {
		origins = append(origins, it.OriginText)
	}
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "scan_target").
		Float64("min_reward", minReward).
		Int("item_count", len(items)).
		Strs("origins", origins).
		Msg("scanned job items")

	return &maa.CustomRecognitionResult{
		Box: arg.Roi,
	}, true
}

// SeizeDeliveryJobsScanTargetAction 为当前委托覆写 Pipeline 点击目标。
type SeizeDeliveryJobsScanTargetAction struct{}

// Run 覆写当前委托的点击目标，并推进扫描索引。
func (a *SeizeDeliveryJobsScanTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 全部委托扫描完毕 → on_error：NoProgress → Refresh
	if scannedJobItems == nil || currentIndex >= len(scannedJobItems) {
		log.Info().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Int("total", len(scannedJobItems)).
			Msg("all items scanned, will refresh")
		return false
	}

	item := scannedJobItems[currentIndex]
	maafocus.Print(ctx, i18n.T("seizedeliveryjobs.checking_job", currentIndex+1, len(scannedJobItems)))

	if len(item.ViewLocationBox) < 4 {
		log.Error().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Int("box_len", len(item.ViewLocationBox)).
			Msg("view location box invalid")
		return false
	}
	if len(item.AcceptBox) < 4 {
		log.Error().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Int("box_len", len(item.AcceptBox)).
			Msg("accept box invalid")
		return false
	}

	viewRect := boxToRect(item.ViewLocationBox)
	acceptRect := boxToRect(item.AcceptBox)

	log.Debug().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "scan_action").
		Int("index", currentIndex).
		Ints("view_location_box", item.ViewLocationBox).
		Ints("accept_box", item.AcceptBox).
		Msg("overriding pipeline targets")

	if err := ctx.OverridePipeline(map[string]any{
		"SeizeDeliveryJobsFoundTargetViewLocationClick": map[string]any{"target": viewRect},
		"SeizeDeliveryJobsAcceptClick":                  map[string]any{"target": acceptRect},
		"SeizeDeliveryJobsRetryClickAccept":             map[string]any{"target": acceptRect},
	}); err != nil {
		log.Error().Err(err).
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Msg("override pipeline failed")
		return false
	}

	currentIndex++
	return true
}

// readMaxAttemptRounds 解析尝试轮次上限，缺失或非法时回落到默认值。
// 参数来自 JSON 反序列化，数字可能是 float64；pipeline 替换后也可能落到 string，
// 因此与 batchaddfriends.parseMaxCount 一样做类型兜底。
func readMaxAttemptRounds(raw string) int {
	fallback := func(reason string) int {
		log.Warn().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "no_progress").
			Str("param", raw).
			Str("reason", reason).
			Int("fallback", defaultMaxAttemptRounds).
			Msg("invalid max_attempt_rounds, fallback to default")
		return defaultMaxAttemptRounds
	}

	if raw == "" {
		return defaultMaxAttemptRounds
	}
	var p struct {
		MaxAttemptRounds any `json:"max_attempt_rounds"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return fallback(err.Error())
	}
	switch v := p.MaxAttemptRounds.(type) {
	case float64:
		if n := int(v); n > 0 {
			return n
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback("not a positive integer")
}

// SeizeDeliveryJobsNoProgressAction 处理「本轮没有接到委托」，是三条失败路径的统一收口：
// ① Loop 的 FindTarget 未命中（列表无价格达标委托）；
// ② ScanTarget 全部扫完仍未匹配到终点（含 0 个合格委托）；
// ③ AcceptClick 接取失败。
// 未达上限：清空单轮扫描状态并返回 true，由 next 走 Refresh 重新拉列表；
// 达到上限：返回 false，由 on_error 终止任务并提示人工介入，不再无限刷新。
type SeizeDeliveryJobsNoProgressAction struct{}

// Run 增加无进展计数，并根据上限刷新列表或使任务失败。
func (a *SeizeDeliveryJobsNoProgressAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	raw := ""
	if arg != nil {
		raw = arg.CustomActionParam
	}
	maxAttemptRounds := readMaxAttemptRounds(raw)
	clearRoundState()
	attemptRounds++

	if attemptRounds >= maxAttemptRounds {
		log.Error().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "no_progress").
			Int("attempt_rounds", attemptRounds).
			Int("max_attempt_rounds", maxAttemptRounds).
			Msg("no seize after repeated rounds, abort task")
		maafocus.Print(ctx, i18n.T("seizedeliveryjobs.give_up", attemptRounds, maxAttemptRounds))
		return false
	}

	log.Warn().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "no_progress").
		Int("attempt_rounds", attemptRounds).
		Int("max_attempt_rounds", maxAttemptRounds).
		Msg("no seize this round, refresh and retry")
	return true
}

// 编译期接口实现检查。
var (
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsResetScanStateAction{}
	_ maa.CustomRecognitionRunner = &SeizeDeliveryJobsScanTargetRecognition{}
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsScanTargetAction{}
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsNoProgressAction{}
)
