package seizedeliveryjobs

import (
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

// filteredDetail holds the parsed OCR sub-recognition result.
// The Text field is only populated for origin (index 1); others leave it zero.
type filteredDetail struct {
	Filtered []struct {
		Box   []int   `json:"box"`
		Score float64 `json:"score"`
		Text  string  `json:"text"`
	} `json:"filtered"`
}

// maxAttemptRounds 是尝试轮次上限：连续这么多轮没有接到委托就终止任务，避免无限刷新。
const maxAttemptRounds = 100

var (
	scannedJobItems []deliveryJobItem
	currentIndex    int
	// attemptRounds 记录「自本次进入抢单入口以来，尚未成功接到委托的轮次」。
	// 跨轮累计，不会被单轮扫描状态的清理清零，只在任务入口归零。
	// 一轮 = 一次列表刷新到下一次列表刷新，以下情况都算一轮未接到：
	// ① 列表里没有价格达标的委托；② 有达标委托但终点均不匹配；③ 匹配到终点但接取失败。
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

// boxToRect converts a [x, y, w, h] box slice to maa.Rect.
func boxToRect(box []int) maa.Rect {
	return maa.Rect{box[0], box[1], box[2], box[3]}
}

// SeizeDeliveryJobsResetScanStateAction 清空单轮扫描状态与跨轮计数，挂在任务入口 SeizeDeliveryJobsMain。
type SeizeDeliveryJobsResetScanStateAction struct{}

func (a *SeizeDeliveryJobsResetScanStateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	resetScanState()
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "reset_scan_state").
		Msg("scan state cleared")
	return true
}

// SeizeDeliveryJobsScanTargetRecognition scans the delivery job list once and caches
// all reward-qualified jobs for subsequent ScanTarget iterations.
type SeizeDeliveryJobsScanTargetRecognition struct{}

func (r *SeizeDeliveryJobsScanTargetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Subsequent calls: already have scanned data, just hit
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

	items, ok := scanJobs(ctx, arg.Img, minReward)
	if !ok || len(items) == 0 {
		// 本轮没有任何价格达标的委托。返回命中，让 ScanTargetAction 走「全部扫完」
		// 分支进入 SeizeDeliveryJobsNoProgress 计数；若按识别失败返回，
		// 框架只会视作本节点未命中并落到 Loop 的 Refresh 兜底，该路径将永远不计数。
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

// SeizeDeliveryJobsScanTargetAction overrides pipeline click targets for the current scanned job item and advances the scan index.
type SeizeDeliveryJobsScanTargetAction struct{}

func (a *SeizeDeliveryJobsScanTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 全部委托扫描完毕 → on_error：SeizeDeliveryJobsNoProgress → Refresh
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

// SeizeDeliveryJobsNoProgressAction 处理「本轮没有接到委托」，是三条失败路径的统一收口：
// ① Loop 的 FindTarget 未命中（列表无价格达标委托）；
// ② ScanTarget 全部扫完仍未匹配到终点（含 0 个合格委托）；
// ③ AcceptClick 接取失败。
// 未达上限：清空单轮扫描状态并返回 true，由 next 走 Refresh 重新拉列表；
// 达到上限：返回 false，由 on_error 终止任务并提示人工介入，不再无限刷新。
type SeizeDeliveryJobsNoProgressAction struct{}

func (a *SeizeDeliveryJobsNoProgressAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	clearRoundState()
	attemptRounds++

	if attemptRounds >= maxAttemptRounds {
		log.Error().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "no_progress").
			Int("attempt_rounds", attemptRounds).
			Msg("no seize after repeated rounds, abort task")
		maafocus.Print(ctx, i18n.T("seizedeliveryjobs.give_up", attemptRounds, maxAttemptRounds))
		return false
	}

	log.Warn().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "no_progress").
		Int("attempt_rounds", attemptRounds).
		Msg("no seize this round, refresh and retry")
	return true
}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsResetScanStateAction{}
	_ maa.CustomRecognitionRunner = &SeizeDeliveryJobsScanTargetRecognition{}
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsScanTargetAction{}
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsNoProgressAction{}
)
