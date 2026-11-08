package visitfriends

import (
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	component = "BtwSell"

	target = 150000

	swipe = "VisitFriendsBtwSellItemSwipe"
)

var (
	coupons = []string{"item_domain_jinlong_coupon", "item_domain_tundra_coupon"}

	mu        sync.Mutex
	remaining int
	base      *int
)

// Swipe 从节点识别结果取单价，按差值计算本次售卖数量并覆写滑条目标
// 首次调用记录 IMS 获得过的调度券数量，给 FinishedRecognition 用
type SwipeAction struct{}

// Finished 用 IMS 累计的调度券增量扣减差值，差值耗尽时命中并重置差值
type FinishedRecognition struct{}

var (
	_ maa.CustomActionRunner      = (*SwipeAction)(nil)
	_ maa.CustomRecognitionRunner = (*FinishedRecognition)(nil)
)

func (a *SwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", component).Msg("custom action arg is nil")
		return false
	}

	mu.Lock()
	if base == nil {
		snap := ims.ItemsSnapshot()
		sum := 0
		for _, id := range coupons {
			sum += snap[id]
		}
		base = &sum
	}
	rest := remaining
	mu.Unlock()

	detail, err := recogtarget.SelectDetail(ctx, arg.CurrentTaskName, arg.RecognitionDetail)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("select detail failed")
		return false
	}
	price, err := ocrnum.Extract(detail)
	if err != nil || price <= 0 {
		log.Error().Err(err).Str("component", component).Msg("price parse failed")
		return false
	}

	qty := rest / price
	if qty < 1 {
		qty = 1
	}

	err = ctx.OverridePipeline(map[string]any{
		swipe: map[string]any{"attach": map[string]any{"TargetQuantity": qty}},
	})
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to override btwsell pipeline")
		return false
	}

	log.Info().
		Str("component", component).
		Int("price", price).
		Int("qty", qty).
		Int("remaining", rest).
		Msg("btwsell planned")
	return true
}

func (r *FinishedRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	mu.Lock()
	defer mu.Unlock()

	snap := ims.ItemsSnapshot()
	cur := 0
	for _, id := range coupons {
		cur += snap[id]
	}
	// get the result by this sell from IMS
	gain := 0
	if base != nil {
		gain = cur - *base
		if gain < 0 {
			gain = 0
		}
		remaining -= gain
	}
	base = &cur

	if remaining > 0 {
		log.Info().
			Str("component", component).
			Int("gain", gain).
			Int("remaining", remaining).
			Msg("btwsell ongoing")
		return nil, false
	}
	remaining = target
	log.Info().
		Str("component", component).
		Int("gain", gain).
		Msg("btwsell finished")
	return &maa.CustomRecognitionResult{}, true
}
