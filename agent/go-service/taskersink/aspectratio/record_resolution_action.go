package aspectratio

import (
	"encoding/json"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/exprcoord"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &RecordResolutionAndClickAction{}

var resolutionPattern = regexp.MustCompile(`(?i)(\d{3,4})\D+(\d{3,4})`)

type recordResolutionAndClickParam struct {
	OCRROI        []any    `json:"ocr_roi"`
	Target        []any    `json:"target"`
	LabelROI      []any    `json:"label_roi,omitempty"`
	LabelExpected []string `json:"label_expected,omitempty"`
}

// RecordResolutionAndClickAction records the current resolution before opening the dropdown.
type RecordResolutionAndClickAction struct{}

func (a *RecordResolutionAndClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params recordResolutionAndClickParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to parse params")
		return false
	}

	controller := ctx.GetTasker().GetController()
	recordOriginal := defaultChecker != nil && defaultChecker.shouldRecordOriginalResolution()
	requireImage := recordOriginal || len(params.LabelROI) > 0
	w, h, img, ok := actionImageSize(controller, requireImage)
	if !ok {
		log.Error().Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to get image size")
		return false
	}

	if recordOriginal {
		ocrROI, err := exprcoord.ResolveRect(params.OCRROI, w, h)
		if err != nil {
			log.Error().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to resolve ocr_roi")
			return false
		}
		text, originalW, originalH, ok := recognizeResolution(ctx, img, ocrROI)
		if !ok {
			log.Warn().Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to record original resolution")
			return false
		}
		defaultChecker.saveOriginalResolution(text, originalW, originalH)
		log.Info().
			Str("component", "AspectRatioRecordResolutionAndClick").
			Str("resolution", text).
			Int("width", originalW).
			Int("height", originalH).
			Msg("recorded original resolution")
	}

	target, err := exprcoord.ResolveRect(params.Target, w, h)
	if err != nil {
		log.Error().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to resolve target")
		return false
	}
	if len(params.LabelROI) > 0 {
		labelY, ok := actionBoxCenterY(arg.Box)
		if !ok {
			labelY, ok = recognizeLabelCenterY(ctx, img, params.LabelROI, params.LabelExpected, w, h)
		}
		if !ok {
			log.Warn().Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to align target with label")
			return false
		}
		target[1] = labelY - target.Height()/2
	}

	_, err = ctx.RunActionDirect(maa.ActionTypeClick, &maa.ClickParam{Target: maa.NewTargetRect(target)}, target, nil)
	if err != nil {
		log.Error().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("click action failed")
		return false
	}
	return true
}

func actionImageSize(controller *maa.Controller, requireImage bool) (int, int, image.Image, bool) {
	img, err := controller.CacheImage()
	if err == nil && img != nil {
		bounds := img.Bounds()
		return bounds.Dx(), bounds.Dy(), img, true
	}
	if requireImage {
		log.Warn().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to get cached image")
		return 0, 0, nil, false
	}
	w, h, err := controller.GetResolution()
	if err != nil {
		log.Warn().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to get resolution")
		return 0, 0, nil, false
	}
	return int(w), int(h), nil, true
}

func recognizeResolution(ctx *maa.Context, img image.Image, roi maa.Rect) (string, int, int, bool) {
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, maa.OCRParam{
		ROI:      maa.NewTargetRect(roi),
		Expected: []string{`\d{3,4}.*\d{3,4}`},
		OnlyRec:  true,
	}, img)
	if err != nil || detail == nil || !detail.Hit {
		if err != nil {
			log.Warn().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("resolution OCR failed")
		}
		return "", 0, 0, false
	}

	for _, text := range resolutionTexts(detail) {
		width, height, ok := parseResolutionText(text)
		if ok {
			return fmt.Sprintf("%dx%d", width, height), width, height, true
		}
	}
	return "", 0, 0, false
}

func actionBoxCenterY(box maa.Rect) (int, bool) {
	if box.Width() <= 0 || box.Height() <= 0 {
		return 0, false
	}
	return box.Y() + box.Height()/2, true
}

func recognizeLabelCenterY(ctx *maa.Context, img image.Image, rawROI []any, expected []string, w, h int) (int, bool) {
	roi, err := exprcoord.ResolveRect(rawROI, w, h)
	if err != nil {
		log.Warn().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("failed to resolve label_roi")
		return 0, false
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, maa.OCRParam{
		ROI:      maa.NewTargetRect(roi),
		Expected: expected,
	}, img)
	if err != nil || detail == nil || !detail.Hit {
		if err != nil {
			log.Warn().Err(err).Str("component", "AspectRatioRecordResolutionAndClick").Msg("label OCR failed")
		}
		return 0, false
	}
	box := detail.Box
	return box.Y() + box.Height()/2, true
}

func resolutionTexts(detail *maa.RecognitionDetail) []string {
	if detail == nil || detail.Results == nil {
		return nil
	}
	texts := make([]string, 0)
	for _, bucket := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.Filtered, detail.Results.All} {
		for _, result := range bucket {
			if result == nil {
				continue
			}
			ocr, ok := result.AsOCR()
			if !ok || ocr == nil || strings.TrimSpace(ocr.Text) == "" {
				continue
			}
			texts = append(texts, ocr.Text)
		}
	}
	return texts
}

func parseResolutionText(text string) (int, int, bool) {
	match := resolutionPattern.FindStringSubmatch(text)
	if len(match) != 3 {
		return 0, 0, false
	}
	width, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	height, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}
