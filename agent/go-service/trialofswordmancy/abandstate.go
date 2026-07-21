package trialofswordmancy

import (
	"strconv"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &AbandRecognition{}

// AbandRecognition 从当前截图中已显示的放弃确认文本识别剩余放弃次数。
// 它只读取文本并更新缓存，不负责打开弹窗、等待界面或关闭弹窗。
type AbandRecognition struct{}

// Run 识别放弃确认弹窗文本，并将剩余放弃次数写入总成识别使用的缓存。
func (r *AbandRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", component).Msg("aband recognition arg or image is nil")
		return nil, false
	}

	count := 0
	text := ""
	exhausted, ok := recognizeAbandExhausted(ctx, arg)
	if !ok {
		return nil, false
	}
	if !exhausted {
		var ok bool
		text, ok = ocrNodeText(ctx, arg.Img, nodeAbandPopup)
		if !ok {
			log.Warn().Str("component", component).Msg("aband popup OCR failed")
			return nil, false
		}

		var parsed bool
		count, parsed = parseFirstInt(text)
		if !parsed {
			log.Warn().Str("component", component).Str("ocr", text).Msg("failed to parse remaining aband count")
			return nil, false
		}
	}

	setAband(count)
	log.Info().Str("component", component).Int("aband", count).Str("ocr", text).Msg("remaining aband count recognized")

	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: strconv.Itoa(count)}, true
}

func recognizeAbandExhausted(ctx *maa.Context, arg *maa.CustomRecognitionArg) (bool, bool) {
	detail, err := ctx.RunRecognition(nodeAbandExhausted, arg.Img, nil)
	if err != nil || detail == nil {
		log.Warn().Err(err).Str("component", component).Msg("aband exhausted ColorMatch failed")
		return false, false
	}
	return detail.Hit, true
}

// 剩余放弃次数的持久化（唯一带状态的字段）。
//
// 为何这一项要持久化、不能像其它字段那样每步从截图读：界面上不直接显示剩余放弃次数，
// 只有点击「放弃」后弹出的确认框里才写（「本日剩余放弃次数x次」/「已用完」）。
// 所以由 Pipeline 负责打开和关闭放弃弹窗，RecognizeAband 只识别当前弹窗文本并缓存次数；
// 之后总成识别直接读缓存。
//
// 生命周期：
//   - 进程内初始化为 -1（未知）。
//   - 路由到 放弃(Abandon) 或 开始演算(Calculate) 时重置为 -1——前者因为放弃会扣 1 次（缓存失效），
//     后者作为回合结束的统一兜底，下回合重新识别。
var (
	abandMu    sync.Mutex
	abandCount = -1 // -1 = 未知，需从放弃弹窗识别
)

func getAband() int {
	abandMu.Lock()
	defer abandMu.Unlock()
	return abandCount
}

func setAband(n int) {
	abandMu.Lock()
	defer abandMu.Unlock()
	abandCount = n
}

// resetAband 把缓存的剩余放弃次数置为 -1（未知），等待 RecognizeAband 重新识别。
func resetAband() {
	abandMu.Lock()
	defer abandMu.Unlock()
	abandCount = -1
}
