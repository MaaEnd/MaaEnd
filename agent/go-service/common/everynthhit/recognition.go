package everynthhit

import (
	"encoding/json"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const recognitionName = "EveryNthHit"

var _ maa.CustomRecognitionRunner = &EveryNthHit{}

// recognitionParam 是 EveryNthHit 的 custom_recognition_param。
type recognitionParam struct {
	// Key 是计数器名字，不同 key 之间互不影响。
	Key string `json:"key"`
	// Every 保留原始 JSON 类型，由 parseEvery 同时接受整数与数字字符串。
	Every json.RawMessage `json:"every"`
}

// EveryNthHit 每被评估 every 次命中一次，用于「第 N 次走到该节点才继续」的场景。
type EveryNthHit struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *EveryNthHit) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().
			Str("component", recognitionName).
			Msg("got nil custom recognition arg")
		return nil, false
	}

	var param recognitionParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", recognitionName).
			Str("param", arg.CustomRecognitionParam).
			Msg("failed to parse custom_recognition_param")
		return nil, false
	}
	if param.Key == "" {
		log.Error().
			Str("component", recognitionName).
			Str("param", arg.CustomRecognitionParam).
			Msg("custom_recognition_param.key is required")
		return nil, false
	}

	every, ok := parseEvery(param.Every)
	if !ok || every <= 0 {
		log.Warn().
			Str("component", recognitionName).
			Str("key", param.Key).
			Str("every", string(param.Every)).
			Msg("custom_recognition_param.every must be a positive integer")
		return nil, false
	}

	if !Advance(param.Key, every) {
		return nil, false
	}

	log.Debug().
		Str("component", recognitionName).
		Str("key", param.Key).
		Int("every", every).
		Msg("every-nth hit reached")
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// parseEvery 从 every 的原始 JSON 中解析次数，同时接受 JSON 数字与数字字符串。
//
// 数字先格式化成十进制字符串再走 Atoi，因此 3 与 3.0 等价、3.5 之类的小数会被
// 拒绝；布尔、数组等非数字类型同样返回 false。这里只做语法解析，非正数由 Run
// 的 every <= 0 判断拦下，避免两处各写一套范围检查。
func parseEvery(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}

	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return atoiEvery(strconv.FormatFloat(number, 'f', -1, 64))
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return atoiEvery(strings.TrimSpace(text))
	}

	return 0, false
}

// atoiEvery 把十进制字符串转成正整数；空串、小数、非数字一律视为非法。
func atoiEvery(text string) (int, bool) {
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return value, true
}
