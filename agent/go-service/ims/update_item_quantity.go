package ims

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentUpdateItemQuantity = "UpdateItemQuantity"

var _ maa.CustomActionRunner = &UpdateItemQuantity{}

// updateItemQuantityParam is custom_action_param for UpdateItemQuantity.
type updateItemQuantityParam struct {
	Item  string `json:"item"`
	Delta int    `json:"delta"`
}

// UpdateItemQuantity applies a signed delta to one cached item quantity (A1).
// Result is clamped to >= 0. Does not change readiness / last_sync (only A2 marks sync).
type UpdateItemQuantity struct{}

// Run implements maa.CustomActionRunner.
func (a *UpdateItemQuantity) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", componentUpdateItemQuantity).
			Msg("got nil custom action arg")
		return false
	}

	params, err := parseUpdateItemQuantityParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentUpdateItemQuantity).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse params")
		return false
	}

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentUpdateItemQuantity).
			Msg("failed to hydrate ims cache")
		return false
	}

	before, after, clamped, items, lastSync, hasData := globalCache.applyDelta(params.Item, params.Delta)
	if err := persistItemsPreserveSync(items, lastSync, hasData); err != nil {
		log.Error().
			Err(err).
			Str("component", componentUpdateItemQuantity).
			Str("item", params.Item).
			Msg("failed to persist item quantity")
		return false
	}

	log.Info().
		Str("component", componentUpdateItemQuantity).
		Str("item", params.Item).
		Int("delta", params.Delta).
		Int("before", before).
		Int("after", after).
		Bool("clamped", clamped).
		Bool("has_data", hasData).
		Msg("item quantity updated")
	return true
}

func parseUpdateItemQuantityParam(raw string) (updateItemQuantityParam, error) {
	var params updateItemQuantityParam
	if strings.TrimSpace(raw) == "" {
		return updateItemQuantityParam{}, fmt.Errorf("custom_action_param is required")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return updateItemQuantityParam{}, err
	}
	params.Item = strings.TrimSpace(params.Item)
	if params.Item == "" {
		return updateItemQuantityParam{}, fmt.Errorf("item is required")
	}
	return params, nil
}

// persistItemsPreserveSync writes IMS.json without changing readiness semantics.
// When hasData is true, UpdatedAt follows lastSync; otherwise keep existing file UpdatedAt if any.
func persistItemsPreserveSync(items map[string]int, lastSync time.Time, hasData bool) error {
	recordMu.Lock()
	defer recordMu.Unlock()

	updatedAt := lastSync
	if !hasData || updatedAt.IsZero() {
		rec, err := loadRecord()
		if err != nil {
			return err
		}
		if !rec.UpdatedAt.IsZero() {
			updatedAt = rec.UpdatedAt
		}
	}

	copied := make(map[string]int, len(items))
	for k, v := range items {
		copied[k] = v
	}
	if err := saveRecord(recordFile{UpdatedAt: updatedAt.UTC(), Items: copied}); err != nil {
		return err
	}
	hydrated = true
	return nil
}
