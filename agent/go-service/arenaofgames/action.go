package arenaofgames

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// —— Decide 动作（借鉴 trialofswordmancy/action.go 的单步决策模式）——

var _ maa.CustomActionRunner = &DecideAction{}

// DecideAction 消费 ArenaOfGames.CheckHand 识别产出的手牌明细（识别每步只跑一次，本动作不重复读牌），
// 对照勾选目标算出三态并用 OverrideNext 路由：凑成即结束（早停）、无法凑出转放弃链、否则转抽牌。
// 抽牌锚点（确认第几张牌完整可读）是按手牌数预定义的静态节点
// ArenaOfGamesDrawCard1-5 / ArenaOfGamesDrawCardSuccess1-5，路由即可，无需 OverridePipeline。
// 流程仍归 pipeline：点击与等动画由执行节点（DrawCard / Abandon 链）完成，Go 只做判定与路由。
type DecideAction struct{}

// 路由与执行节点名（定义在 ArenaOfGames.json / ArenaOfGamesCommon.json）。
const (
	decideComponent    = "ArenaOfGames.Decide"
	nodeDecide         = "ArenaOfGamesDecide"
	nodeStop           = "ArenaOfGamesStop"
	nodeDrawCardPrefix = "ArenaOfGamesDrawCard" // + "1".."5"：抽出第 N 张牌的抽牌节点
	nodeAbandon        = "ArenaOfGamesAbandon"
)

// 今日好运解析失败时的重试参数：瞬时失败（界面淡入、动画扫过 roi）换帧重试几次即可恢复，
// 与旧版三态识别节点靠框架逐心跳重试的容错窗口相当。
const (
	decideRetryAttempts = 3
	decideRetryInterval = 500 * time.Millisecond
)

// Run 解析手牌明细、判定三态并路由。
func (a *DecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("decide action context or arg is nil")
		return false
	}
	detail, ok := unwrapHandDetail(arg.RecognitionDetail)
	if !ok {
		log.Error().Str("component", component).Msg("decide action got no hand detail")
		return false
	}
	// 句柄生命周期：本回调内 Tasker / Controller 只取一次并往下传。
	tasker := ctx.GetTasker()
	if tasker == nil {
		log.Error().Str("component", component).Msg("decide action got no tasker")
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		log.Error().Str("component", component).Msg("decide action got no controller")
		return false
	}
	img := cachedImage(ctrl)
	if img == nil {
		log.Error().Str("component", component).Msg("decide action got no cached image")
		return false
	}
	targets, err := resolveTargets(ctx, img)
	if err != nil {
		// 决策动作把原先分散在三态识别节点、由框架逐心跳兜底的目标解析集中成了
		// 一次性调用，瞬时 OCR 失败若不重试会直接终止任务，故在此换新截图补回容错。
		targets, err = retryResolveTargets(ctx, tasker, ctrl)
	}
	if err != nil {
		log.Warn().Err(err).Str("component", component).Msg("target hands could not be resolved")
		return false
	}
	status := evaluateHand(handCounts(detail), detail.Count, targets)

	next, err := decideNext(status, detail.Count)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("status", status).Msg("failed to map decision")
		return false
	}
	if err := ctx.OverrideNext(nodeDecide, []maa.NextItem{{Name: next}}); err != nil {
		log.Error().Err(err).Str("component", component).Str("status", status).Msg("failed to route decision")
		return false
	}
	log.Info().
		Str("component", component).
		Int("count", detail.Count).
		Str("category", detail.Category).
		Str("status", status).
		Str("next", next).
		Msg("decision made")
	return true
}

// retryResolveTargets 等待一个心跳间隔后截新帧重试目标解析，共尝试 decideRetryAttempts 次；
// 返回最后一次错误，tasker 停止时立即放弃。
func retryResolveTargets(ctx *maa.Context, tasker *maa.Tasker, ctrl *maa.Controller) (map[category]bool, error) {
	err := fmt.Errorf("targets unresolved")
	for range decideRetryAttempts {
		if tasker.Stopping() {
			return nil, err
		}
		time.Sleep(decideRetryInterval)
		ctrl.PostScreencap().Wait()
		img, imgErr := ctrl.CacheImage()
		if imgErr != nil || img == nil {
			err = fmt.Errorf("screencap while retrying targets: %w", imgErr)
			continue
		}
		targets, resolveErr := resolveTargets(ctx, img)
		if resolveErr == nil {
			return targets, nil
		}
		err = resolveErr
	}
	return nil, err
}

// decideNext 把三态映射到执行节点：complete → 结束；rejected → 放弃链；
// continuing → 按手牌数路由到静态抽牌节点（抽第 count+1 张牌）。
func decideNext(status string, count int) (string, error) {
	switch status {
	case "complete":
		return nodeStop, nil
	case "rejected":
		return nodeAbandon, nil
	case "continuing":
		if count < 0 || count > 4 {
			return "", fmt.Errorf("cannot draw the %dth card with %d cards in hand", count+1, count)
		}
		return nodeDrawCardPrefix + strconv.Itoa(count+1), nil
	default:
		return "", fmt.Errorf("unknown status %q", status)
	}
}

// cachedImage 取框架缓存的截图——Decide 的识别刚在同一帧上完成，无需再截。
func cachedImage(ctrl *maa.Controller) image.Image {
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return nil
	}
	return img
}

// unwrapHandDetail 从 CheckHand 识别的 DetailJson 中取回手牌明细。
// 框架可能把 Custom 识别的 detail 包成 {"best":{"detail": <raw>}}，raw 还可能再被
// 序列化成 JSON 字符串（部分核心版本的行为，见 trialofswordmancy 的同类处理），都兼容。
func unwrapHandDetail(detail *maa.RecognitionDetail) (handDetail, bool) {
	if detail == nil || detail.DetailJson == "" {
		return handDetail{}, false
	}
	raw := json.RawMessage(detail.DetailJson)
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		raw = wrapped.Best.Detail
	}
	if s, ok := rawJSONToString(raw); ok {
		raw = json.RawMessage(s)
	}
	var parsed *handDetail
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed == nil {
		return handDetail{}, false
	}
	return *parsed, true
}

// rawJSONToString 解开「被序列化成 JSON 字符串的 JSON」：raw 以引号开头则解出字符串
// 内容，否则原样返回。
func rawJSONToString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}
