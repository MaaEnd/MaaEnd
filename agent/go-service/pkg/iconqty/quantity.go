package iconqty

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Quantity OCR uses Maa roi_offset relative to IconRecognition cell_box.
// Win32: [0, 78, 0, -78]; ADB: [0, 98, 0, -98].
var (
	QuantityROIOffsetWin32 = [4]int{0, 78, 0, -78}
	QuantityROIOffsetADB   = [4]int{0, 98, 0, -98}

	ocrNumericPattern  = regexp.MustCompile(`(?i)[+-]?(?:\d+(?:[.,]\d+)?|[.,]\d+)\s*(?:[a-z]+|万|亿)?`)
	asciiLetterPattern = regexp.MustCompile(`[A-Za-z]+$`)
)

// QuantityHit is one IconRecognition match with OCR'd stack quantity.
type QuantityHit struct {
	ItemID string
	Qty    int
}

func quantityROIOffset() [4]int {
	if isADBController() {
		return QuantityROIOffsetADB
	}
	return QuantityROIOffsetWin32
}

// ApplyROIOffset applies a Maa-style roi_offset [x,y,w,h] to box.
func ApplyROIOffset(box maa.Rect, off [4]int) (maa.Rect, bool) {
	out := maa.Rect{
		box[0] + off[0],
		box[1] + off[1],
		box[2] + off[2],
		box[3] + off[3],
	}
	if out[2] <= 0 || out[3] <= 0 {
		return maa.Rect{}, false
	}
	return out, true
}

// QuantityROIFromCellBox applies the controller-specific quantity roi_offset.
func QuantityROIFromCellBox(cell maa.Rect) (maa.Rect, bool) {
	return ApplyROIOffset(cell, quantityROIOffset())
}

// RecognizeQuantities runs one IconRecognition pass, then OCRs quantity from
// each match cell_box. Returns one hit per match (no ID aggregation) so
// callers can add multiple stacks (A3) or overwrite by ID (A2).
func RecognizeQuantities(ctx *maa.Context, img image.Image, req Request) ([]QuantityHit, error) {
	matches, err := Recognize(ctx, img, req)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	out := make([]QuantityHit, 0, len(matches))
	for _, m := range matches {
		itemID := strings.TrimSpace(m.ItemID)
		if itemID == "" {
			continue
		}
		if !m.CellOK() {
			return nil, fmt.Errorf("IconRecognition match missing cell_box for %s", itemID)
		}
		qtyROI, ok := QuantityROIFromCellBox(m.CellBox)
		if !ok {
			log.Info().
				Str("component", "iconqty").
				Str("item_id", itemID).
				Interface("cell_box", m.CellBox).
				Msg("quantity roi invalid after offset, skip")
			continue
		}
		qty, hit, err := RecognizeQuantityInROI(ctx, img, qtyROI)
		if err != nil {
			return nil, fmt.Errorf("quantity ocr for %s: %w", itemID, err)
		}
		if !hit {
			log.Info().
				Str("component", "iconqty").
				Str("item_id", itemID).
				Msg("icon matched but quantity ocr missed, skip")
			continue
		}
		out = append(out, QuantityHit{ItemID: itemID, Qty: qty})
	}
	return out, nil
}

// RecognizeQuantityInROI OCRs a numeric quantity inside roi.
func RecognizeQuantityInROI(ctx *maa.Context, img image.Image, roi maa.Rect) (int, bool, error) {
	if ctx == nil {
		return 0, false, fmt.Errorf("nil context")
	}
	if img == nil {
		return 0, false, fmt.Errorf("nil image")
	}
	if roi[2] <= 0 || roi[3] <= 0 {
		return 0, false, fmt.Errorf("invalid quantity roi")
	}
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		&maa.OCRParam{
			ROI:      maa.NewTargetRect(roi),
			Expected: []string{`\d`},
			OnlyRec:  true,
		},
		img,
	)
	if err != nil {
		return 0, false, fmt.Errorf("ocr quantity: %w", err)
	}
	if detail == nil || !detail.Hit {
		return 0, false, nil
	}
	qty, err := ExtractOCRQuantity(detail)
	if err != nil {
		return 0, false, nil
	}
	return qty, true, nil
}

// ExtractOCRQuantity parses a numeric quantity from an OCR recognition detail.
func ExtractOCRQuantity(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}
	if best := detail.Results.Best; best != nil {
		if ocrResult, ok := best.AsOCR(); ok {
			return ParseOCRNumericValue(ocrResult.Text)
		}
	}
	for _, result := range detail.Results.All {
		if ocrResult, ok := result.AsOCR(); ok {
			return ParseOCRNumericValue(ocrResult.Text)
		}
	}
	return 0, fmt.Errorf("no ocr result found")
}

// ParseOCRNumericValue parses integers from OCR text (supports k/m/b/万/亿).
func ParseOCRNumericValue(text string) (int, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return 0, fmt.Errorf("ocr text is empty")
	}
	matchIndex := ocrNumericPattern.FindStringIndex(cleaned)
	if matchIndex == nil {
		return 0, fmt.Errorf("ocr text %q contains no numeric value", cleaned)
	}
	match := cleaned[matchIndex[0]:matchIndex[1]]
	numberText, multiplier, err := normalizeOCRNumericToken(match)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(numberText, 64)
	if err != nil {
		return 0, err
	}
	scaled := math.Round(value * multiplier)
	return int(scaled), nil
}

func normalizeOCRNumericToken(token string) (string, float64, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", 0, fmt.Errorf("numeric token is empty")
	}
	multiplier := 1.0
	if strings.HasSuffix(token, "万") {
		multiplier = 1e4
		token = strings.TrimSuffix(token, "万")
	} else if strings.HasSuffix(token, "亿") {
		multiplier = 1e8
		token = strings.TrimSuffix(token, "亿")
	} else if loc := asciiLetterPattern.FindStringIndex(token); loc != nil {
		unit := strings.ToLower(token[loc[0]:loc[1]])
		token = strings.TrimSpace(token[:loc[0]])
		switch unit {
		case "k":
			multiplier = 1e3
		case "m":
			multiplier = 1e6
		case "b":
			multiplier = 1e9
		default:
			return "", 0, fmt.Errorf("unsupported numeric unit %q", unit)
		}
	}
	token = strings.TrimSpace(token)
	token = strings.ReplaceAll(token, ",", "")
	if token == "" {
		return "", 0, fmt.Errorf("numeric token missing digits")
	}
	return token, multiplier, nil
}

// ItemDisplayName resolves a localized display name for an IconRecognition item ID.
func ItemDisplayName(itemID string) string {
	for _, key := range []string{
		"iconRecognition.name." + itemID,
		"ims.item." + itemID,
	} {
		name := i18n.T(key)
		if name != key && strings.TrimSpace(name) != "" {
			return name
		}
	}
	return itemID
}
