package autostockpile

import (
	"image"
	"regexp"
	"strconv"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	overflowCurrentMaxRe = regexp.MustCompile(`(\d+)/(\d+)`)
	overflowPlusRe       = regexp.MustCompile(`\+(\d+)`)
)

func runOverflowDetailOCR(ctx *maa.Context, img image.Image) (current int, max int, plus int, ok bool) {
	detail, err := ctx.RunRecognition(overflowQuotaNodeName, img, nil)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "overflow_quota_ocr").
			Msg("failed to run overflow quota ocr")
		return 0, 0, 0, false
	}

	current, max, ok = parseOverflowCurrentMax(ocrTextCandidates(detail, ocrTextPolicyFilteredOnly))
	if !ok {
		return 0, 0, 0, false
	}

	plus, ok = runOverflowQuotaAdditionOCR(ctx, img)
	if !ok {
		return 0, 0, 0, false
	}

	return current, max, plus, true
}

func parseOverflowCurrentMax(texts []string) (current int, max int, ok bool) {
	for _, text := range texts {
		if match := overflowCurrentMaxRe.FindStringSubmatch(text); len(match) == 3 {
			cur, curErr := strconv.Atoi(match[1])
			maxValue, maxErr := strconv.Atoi(match[2])
			if curErr == nil && maxErr == nil {
				return cur, maxValue, true
			}
		}
	}

	return 0, 0, false
}

func runOverflowQuotaAdditionOCR(ctx *maa.Context, img image.Image) (plus int, ok bool) {
	detail, err := ctx.RunRecognition(overflowQuotaAdditionNodeName, img, nil)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "overflow_quota_addition_ocr").
			Msg("failed to run overflow quota addition ocr")
		return 0, false
	}

	plus, ok = parseOverflowPlus(ocrTextCandidates(detail, ocrTextPolicyFilteredOnly))
	if !ok {
		log.Warn().
			Str("component", autoStockpileComponent).
			Str("step", "overflow_quota_addition_ocr_parse").
			Msg("failed to parse overflow quota addition")
		return 0, false
	}

	return plus, true
}

func parseOverflowPlus(texts []string) (int, bool) {
	for _, text := range texts {
		if match := overflowPlusRe.FindStringSubmatch(text); len(match) == 2 {
			plusValue, parseErr := strconv.Atoi(match[1])
			if parseErr == nil {
				return plusValue, true
			}
		}
	}

	return 0, false
}

func resolveOverflow(current int, max int, plus int) (overflowDetected bool, overflowAmount int) {
	overflowAmount = current + plus - max
	return overflowAmount > 0, overflowAmount
}

func resolveAbortReasonFromOverflowCurrent(current int) AbortReason {
	if current == 0 {
		return AbortReasonQuotaZeroSkip
	}

	return AbortReasonNone
}
