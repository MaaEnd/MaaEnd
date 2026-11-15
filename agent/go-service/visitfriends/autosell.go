package visitfriends

import (
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	component = "BtwSell"

	target = 150000

	sliding = "VisitFriendsBtwSellItemSliding"
)

var (
	mu         sync.Mutex
	remaining  = target
	sellPrice  int
)

// Sliding 从节点识别结果取单价，按差值计算本次售卖数量并覆写滑条目标
type SlidingAction struct{}

// Settle 按滑条实际数量与单价扣减差值
type SettleAction struct{}

// Finished 差值已耗尽时命中并重置差值
type FinishedRecognition struct{}

var (
	_ maa.CustomActionRunner      = (*SlidingAction)(nil)
	_ maa.CustomActionRunner      = (*SettleAction)(nil)
	_ maa.CustomRecognitionRunner = (*FinishedRecognition)(nil)
)

func (a *SlidingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", component).Msg("custom action arg is nil")
		return false
	}

	mu.Lock()
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

	qty := (rest + price - 1) / price
	if qty < 1 {
		qty = 1
	}

	err = ctx.OverridePipeline(map[string]any{
		sliding: map[string]any{"attach": map[string]any{"TargetQuantity": qty}},
	})
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to override btwsell pipeline")
		return false
	}

	mu.Lock()
	sellPrice = price
	mu.Unlock()

	log.Info().
		Str("component", component).
		Int("price", price).
		Int("qty", qty).
		Int("remaining", rest).
		Msg("btwsell planned")
	return true
}

func (a *SettleAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", component).Msg("custom action arg is nil")
		return false
	}

	detail, err := recogtarget.SelectDetail(ctx, arg.CurrentTaskName, arg.RecognitionDetail)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("select detail failed")
		return false
	}
	qty, err := ocrnum.Extract(detail)
	if err != nil || qty <= 0 {
		log.Error().Err(err).Str("component", component).Msg("quantity parse failed")
		return false
	}

	mu.Lock()
	price := sellPrice
	if price <= 0 {
		mu.Unlock()
		log.Error().Str("component", component).Msg("sell price not set before accounting")
		return false
	}
	remaining -= qty * price
	left := remaining
	mu.Unlock()

	log.Info().
		Str("component", component).
		Int("price", price).
		Int("qty", qty).
		Int("remaining", left).
		Msg("btwsell accounted")
	return true
}

func (r *FinishedRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	mu.Lock()
	defer mu.Unlock()

	if remaining > 0 {
		log.Info().
			Str("component", component).
			Int("remaining", remaining).
			Msg("btwsell ongoing")
		return nil, false
	}
	remaining = target
	log.Info().
		Str("component", component).
		Msg("btwsell finished")
	return &maa.CustomRecognitionResult{}, true
}
