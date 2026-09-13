package batchdeletefriends

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// InactiveRecognition is a stub: parses min_days and always misses until OCR is implemented.
type InactiveRecognition struct{}

var _ maa.CustomRecognitionRunner = &InactiveRecognition{}

type inactiveRecognitionParam struct {
	MinDays any `json:"min_days"`
}

// Run always returns miss. Fill OCR / day parsing later; min_days is wired from the task option.
func (r *InactiveRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().
			Str("component", "BatchDeleteFriendsInactiveRecognition").
			Msg("nil context or arg")
		return nil, false
	}

	var params inactiveRecognitionParam
	if raw := arg.CustomRecognitionParam; raw != "" {
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			log.Error().
				Err(err).
				Str("component", "BatchDeleteFriendsInactiveRecognition").
				Msg("failed to parse params")
			return nil, false
		}
	}

	minDays, err := parseMinDays(params.MinDays)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "BatchDeleteFriendsInactiveRecognition").
			Interface("min_days", params.MinDays).
			Msg("invalid min_days")
		return nil, false
	}

	log.Info().
		Str("component", "BatchDeleteFriendsInactiveRecognition").
		Int("min_days", minDays).
		Msg("stub: last-login OCR not implemented, always miss")

	return nil, false
}

func parseMinDays(v any) (int, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("min_days is missing")
	case float64:
		return int(x), nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, err
		}
		return int(n), nil
	case string:
		var n int
		if _, err := fmt.Sscanf(x, "%d", &n); err != nil {
			return 0, fmt.Errorf("parse min_days string: %w", err)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported min_days type %T", v)
	}
}
