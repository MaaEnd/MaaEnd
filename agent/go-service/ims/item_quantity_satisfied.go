package ims

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentItemQuantitySatisfied = "ItemQuantitySatisfied"
	// Avoid flooding the UI when Pipeline scans many ItemQuantitySatisfied
	// nodes in one dispatch next-list; identical lines share a throttle window.
	itemQuantityFocusThrottle = 10 * time.Second
)

var _ maa.CustomRecognitionRunner = &ItemQuantitySatisfied{}

// itemQuantitySatisfiedParam is custom_recognition_param for ItemQuantitySatisfied.
type itemQuantitySatisfiedParam struct {
	// Expression is a boolean expression over cached item quantities.
	// Placeholders use {ITEM_ID}, same arithmetic/compare/logic as ExpressionRecognition.
	// When ReportOnly is true, expression must contain exactly one {ITEM_ID}.
	Expression string `json:"expression"`
	// NotifyUI when true prints the resolved expression to UI Focus.
	// Default false (omit or false) to avoid flooding dispatch-style next scans.
	// Ignored when ReportOnly is true (report mode always announces).
	NotifyUI bool `json:"notify_ui"`
	// ReportOnly announces one cached item quantity and always hits.
	// Expression must reference exactly one item; multi-item expressions are rejected.
	ReportOnly bool `json:"report_only"`
}

// ItemQuantitySatisfied reports whether cached item quantities meet an expression (R1).
// Read-only; does not check readiness — combine with ItemDataReady via And when needed.
// With report_only=true it only announces one item's quantity and always returns true.
type ItemQuantitySatisfied struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *ItemQuantitySatisfied) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
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

	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Msg("failed to hydrate ims cache")
		return nil, false
	}

	if params.ReportOnly {
		return r.runReportOnly(ctx, arg, params)
	}

	resolvedExpression, values, err := boolexpr.ResolvePlaceholders(
		params.Expression,
		func(itemID string) (int, error) {
			return globalCache.quantity(itemID), nil
		},
	)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Msg("failed to resolve expression values")
		return nil, false
	}

	result, err := boolexpr.Evaluate(resolvedExpression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Msg("failed to evaluate expression")
		return nil, false
	}

	matched, ok := result.(bool)
	if !ok {
		log.Error().
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Interface("result", result).
			Msg("expression result must be boolean")
		return nil, false
	}

	if params.NotifyUI {
		focusKey := "ims.expression_ok"
		if !matched {
			focusKey = "ims.expression_short"
		}
		maafocus.PrintThrottle(
			ctx,
			itemQuantityFocusThrottle,
			i18n.T(focusKey, resolvedExpression),
		)
	}

	log.Info().
		Str("component", componentItemQuantitySatisfied).
		Str("expression", params.Expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Bool("matched", matched).
		Msg("item expression evaluated")

	if !matched {
		return nil, false
	}

	detailJSON, err := json.Marshal(map[string]any{
		"satisfied":           true,
		"expression":          params.Expression,
		"resolved_expression": resolvedExpression,
		"values":              values,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Msg("failed to marshal detail")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func (r *ItemQuantitySatisfied) runReportOnly(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	params itemQuantitySatisfiedParam,
) (*maa.CustomRecognitionResult, bool) {
	itemID, err := singleItemIDFromExpression(params.Expression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Str("expression", params.Expression).
			Msg("report_only requires exactly one item placeholder")
		return nil, false
	}

	qty := globalCache.quantity(itemID)
	displayName := iconqty.ItemDisplayName(itemID)
	maafocus.PrintThrottle(
		ctx,
		itemQuantityFocusThrottle,
		i18n.T("ims.item_current", displayName, qty),
	)

	log.Info().
		Str("component", componentItemQuantitySatisfied).
		Str("expression", params.Expression).
		Str("item_id", itemID).
		Str("item_name", displayName).
		Int("quantity", qty).
		Bool("report_only", true).
		Msg("item quantity reported")

	detailJSON, err := json.Marshal(map[string]any{
		"satisfied":   true,
		"report_only": true,
		"expression":  params.Expression,
		"item_id":     itemID,
		"quantity":    qty,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentItemQuantitySatisfied).
			Msg("failed to marshal detail")
		return nil, false
	}
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

	params.Expression = strings.TrimSpace(params.Expression)
	if params.Expression == "" {
		return itemQuantitySatisfiedParam{}, fmt.Errorf("expression is required")
	}
	if params.ReportOnly {
		if _, err := singleItemIDFromExpression(params.Expression); err != nil {
			return itemQuantitySatisfiedParam{}, err
		}
	}
	return params, nil
}

// singleItemIDFromExpression requires expression to contain exactly one {ITEM_ID}.
func singleItemIDFromExpression(expression string) (string, error) {
	matches := boolexpr.PlaceholderPattern.FindAllStringSubmatch(expression, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("report_only expression must contain one {ITEM_ID}")
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("report_only expression must contain exactly one {ITEM_ID}, got %d", len(matches))
	}
	itemID := strings.TrimSpace(matches[0][1])
	if itemID == "" {
		return "", fmt.Errorf("report_only item id must not be empty")
	}
	return itemID, nil
}
