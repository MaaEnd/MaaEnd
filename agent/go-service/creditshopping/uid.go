package creditshopping

import (
	"crypto/sha256"
	"fmt"
	"image"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var uidDigitRe = regexp.MustCompile(`\d+`)

func uidFromImage(ctx *maa.Context, img image.Image) string {
	param := maa.OCRParam{
		ROI:      targetRect(maa.Rect{60, 690, 120, 25}),
		Expected: []string{".*"},
		OnlyRec:  true,
		OrderBy:  maa.OCROrderByLength,
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param, img)
	if err != nil || detail == nil || !detail.Hit {
		log.Debug().Str("component", component).Msg("uid ocr miss")
		return "unknown"
	}
	text := bestOCRText(detail)
	return hashUIDDigits(extractDigits(text))
}

func bestOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	if detail.Results.Best != nil {
		if o, ok := detail.Results.Best.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	for _, r := range detail.Results.Filtered {
		if r == nil {
			continue
		}
		if o, ok := r.AsOCR(); ok {
			return strings.TrimSpace(o.Text)
		}
	}
	return ""
}

func extractDigits(s string) string {
	parts := uidDigitRe.FindAllString(s, -1)
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}

func hashUIDDigits(numericPart string) string {
	if numericPart == "" {
		return "unknown"
	}
	h := sha256.Sum256([]byte(numericPart + "CreditShopping"))
	return fmt.Sprintf("%x", h)[:16]
}
