package trialofswordmancy

import (
	"strconv"

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
// 所以由 Pipeline 在每轮任务开始时探测一次放弃弹窗（AbandProbe max_hit=1），
// RecognizeAband 识别当前弹窗文本并缓存次数；之后总成识别直接读缓存，
// 每次真实放弃（Decide 路由到 Abandon）时缓存减一，直到下次任务运行探测覆盖。
//
// 生命周期：
//   - 进程内初始化为 -1（未知）。
//   - 每轮任务运行的探测写入真实值。
//   - 每次真实放弃减一（跨日残局那局不减，见 action.go DecideAction）。
//
// MaaFramework 保证任务回调单线程同步执行，无需加锁。
var abandCount = -1 // -1 = 未知，需从放弃弹窗识别

func getAband() int {
	return abandCount
}

func setAband(n int) {
	abandCount = n
}
