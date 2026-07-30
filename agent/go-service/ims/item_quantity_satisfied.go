package ims

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentItemQuantitySatisfied = "ItemQuantitySatisfied"

var _ maa.CustomRecognitionRunner = &ItemQuantitySatisfied{}

// itemQuantitySatisfiedParam is custom_recognition_param for ItemQuantitySatisfied.
type itemQuantitySatisfiedParam struct {
	Item     string `json:"item"`
	Quantity int    `json:"quantity"`
}

// ItemQuantitySatisfied reports whether cached item quantity is >= required (R1).
// Read-only; does not check readiness — combine with ItemDataReady via And when needed.
type ItemQuantitySatisfied struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *ItemQuantitySatisfied) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().
			Str("component", componentItemQuantitySatisfied).
			Msg("got nil custom recognition arg")
		return nil, false
	}

	params, err := parseItemQuantitySatisfiedParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	current := globalCache.quantity(params.Item)
	if current < params.Quantity {
		log.Info().
			Str("component", componentItemQuantitySatisfied).
			Str("reason", "insufficient").
			Str("item", params.Item).
			Int("current", current).
			Int("required", params.Quantity).
			Msg("item quantity not satisfied")
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"satisfied": true,
		"item":      params.Item,
		"current":   current,
		"required":  params.Quantity,
	})
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func parseItemQuantitySatisfiedParam(raw string) (itemQuantitySatisfiedParam, error) {
	var params itemQuantitySatisfiedParam
	if strings.TrimSpace(raw) == "" {
		return itemQuantitySatisfiedParam{}, fmt.Errorf("custom_recognition_param is required")
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return itemQuantitySatisfiedParam{}, err
	}
	params.Item = strings.TrimSpace(params.Item)
	if params.Item == "" {
		return itemQuantitySatisfiedParam{}, fmt.Errorf("item is required")
	}
	if params.Quantity < 0 {
		return itemQuantitySatisfiedParam{}, fmt.Errorf("quantity must be >= 0, got %d", params.Quantity)
	}
	return params, nil
}
