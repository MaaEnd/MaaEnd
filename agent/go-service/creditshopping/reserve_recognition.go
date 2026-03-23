package creditshopping

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &reserveCreditRecognition{}

type reserveCreditRecognition struct{}

// Run checks whether current credits are below the configured reserve threshold.
func (r *reserveCreditRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	threshold, err := parseReserveCreditThreshold(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse reserve threshold")
		return nil, false
	}

	if threshold <= 0 {
		log.Debug().
			Str("component", "CreditShopping").
			Int("reserve_credit_threshold", threshold).
			Msg("reserve threshold disabled")
		return nil, false
	}

	detail, err := ctx.RunRecognition("CreditShoppingReserveCreditOCRInternal", arg.Img)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Msg("failed to run reserve credit ocr")
		return nil, false
	}

	credit, err := extractReserveCredit(detail)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Msg("failed to parse reserve credit ocr result")
		return nil, false
	}

	action := "pass"
	if credit >= threshold {
		action = "ignore"
	}

	log.Info().
		Str("component", "CreditShopping").
		Int("credit", credit).
		Int("reserve_credit_threshold", threshold).
		Str("result", action).
		Msgf("识别到%d,目标%d,%s", credit, threshold, action)

	if action == "ignore" {
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]int{
		"credit":    credit,
		"threshold": threshold,
	})

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func parseReserveCreditThreshold(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 300, nil
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return 0, err
	}

	value, ok := params["threshold"]
	if !ok {
		return 300, nil
	}

	threshold, err := parseFlexibleInt(value)
	if err != nil {
		return 0, fmt.Errorf("threshold: %w", err)
	}

	if threshold < 0 {
		return 0, fmt.Errorf("must be non-negative")
	}

	return threshold, nil
}

func extractReserveCredit(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}

	if best := detail.Results.Best; best != nil {
		if ocrResult, ok := best.AsOCR(); ok {
			return parseOCRCreditValue(ocrResult.Text)
		}
	}

	for _, result := range detail.Results.All {
		if ocrResult, ok := result.AsOCR(); ok {
			return parseOCRCreditValue(ocrResult.Text)
		}
	}

	return 0, fmt.Errorf("no ocr result found")
}

func parseOCRCreditValue(text string) (int, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return 0, fmt.Errorf("ocr text is empty")
	}

	var digits strings.Builder
	for _, ch := range cleaned {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}

	if digits.Len() == 0 {
		return 0, fmt.Errorf("ocr text %q contains no digits", cleaned)
	}

	value, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0, err
	}

	return value, nil
}

func parseFlexibleInt(value any) (int, error) {
	switch v := value.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}
