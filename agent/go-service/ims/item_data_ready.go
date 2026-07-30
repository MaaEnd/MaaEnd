package ims

import (
	"encoding/json"
	"fmt"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentItemDataReady = "ItemDataReady"
	defaultRefreshDays     = 7
)

var _ maa.CustomRecognitionRunner = &ItemDataReady{}

// itemDataReadyParam is custom_recognition_param for ItemDataReady.
//
// refresh_days:
//   - omitted → default 7
//   - 1 / 7 / 30 → TTL in days
//   - 0 → never expire after a successful sync
type itemDataReadyParam struct {
	RefreshDays *int `json:"refresh_days"`
}

// ItemDataReady reports whether IMS inventory cache is ready for use (R2).
type ItemDataReady struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *ItemDataReady) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().
			Str("component", componentItemDataReady).
			Msg("got nil custom recognition arg")
		return nil, false
	}

	params, err := parseItemDataReadyParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemDataReady).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	refreshDays, err := resolveRefreshDays(params.RefreshDays)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemDataReady).
			Msg("invalid refresh_days")
		return nil, false
	}

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemDataReady).
			Msg("failed to hydrate ims cache")
		return nil, false
	}

	hasData, lastSync := globalCache.snapshot()
	ready, reason := evaluateReady(hasData, lastSync, refreshDays, time.Now())
	if !ready {
		log.Info().
			Str("component", componentItemDataReady).
			Str("reason", reason).
			Bool("has_data", hasData).
			Time("last_sync", lastSync).
			Int("refresh_days", refreshDays).
			Msg("item data not ready")
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"ready":        true,
		"has_data":     hasData,
		"last_sync":    lastSync.UTC().Format(time.RFC3339),
		"refresh_days": refreshDays,
	})
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func parseItemDataReadyParam(raw string) (itemDataReadyParam, error) {
	var params itemDataReadyParam
	if raw == "" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return itemDataReadyParam{}, err
	}
	return params, nil
}

func resolveRefreshDays(v *int) (int, error) {
	if v == nil {
		return defaultRefreshDays, nil
	}
	switch *v {
	case 0, 1, 7, 30:
		return *v, nil
	default:
		return 0, fmt.Errorf("refresh_days must be 0, 1, 7, or 30, got %d", *v)
	}
}

// evaluateReady returns whether cache is usable and a short reason when not.
// refreshDays 0 means never expire after hasData is true.
func evaluateReady(hasData bool, lastSync time.Time, refreshDays int, now time.Time) (bool, string) {
	if !hasData {
		return false, "no_data"
	}
	if refreshDays == 0 {
		return true, ""
	}
	if lastSync.IsZero() {
		return false, "no_data"
	}
	ttl := time.Duration(refreshDays) * 24 * time.Hour
	if now.Sub(lastSync) > ttl {
		return false, "stale"
	}
	return true, ""
}
