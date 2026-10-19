package everynthhit

import (
	"encoding/json"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const recognitionName = "EveryNthHit"

type recognitionParam struct {
	Key   string          `json:"key"`
	Every json.RawMessage `json:"every"`
}

// EveryNthHit hits once every N counted calls that share the same key, where N
// is the every parameter. A hit clears the counter of the key, so the next
// cycle needs N more counted calls, and the call right after a hit is not
// counted. EveryNthHitReset clears the counter of the key and likewise leaves
// the next call uncounted; the task-end fallback sink clears every counter
// together with those pending uncounted calls.
type EveryNthHit struct{}

var _ maa.CustomRecognitionRunner = &EveryNthHit{}

// Run reports a hit when the counted calls of the key reach every, and a miss
// otherwise. The call right after a hit or a reset is not counted. Invalid
// parameters never panic and never hit.
func (*EveryNthHit) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", recognitionName).Msg("custom recognition arg is nil")
		return nil, false
	}

	var param recognitionParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil || param.Key == "" {
		log.Error().
			Err(err).
			Str("component", recognitionName).
			Str("param", arg.CustomRecognitionParam).
			Msg("invalid parameters")
		return nil, false
	}

	every, ok := parseEvery(param.Every)
	if !ok || every <= 0 {
		log.Warn().
			Str("component", recognitionName).
			Str("key", param.Key).
			Str("every", string(param.Every)).
			Msg("invalid every, treat as miss")
		return nil, false
	}

	if !Advance(param.Key, every) {
		return nil, false
	}

	log.Debug().
		Str("component", recognitionName).
		Str("key", param.Key).
		Int("every", every).
		Msg("every nth hit")
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// parseEvery accepts both a JSON number and a numeric string, because the value
// may come from an option placeholder and lose its original type.
func parseEvery(raw json.RawMessage) (int, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}

	var text string
	switch v := value.(type) {
	case string:
		text = strings.TrimSpace(v)
	case float64:
		text = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return 0, false
	}

	every, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return every, true
}
