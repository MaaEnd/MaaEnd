package giftoperator

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &StatusRecognition{}

// StatusRecognition verifies the gift screen before reading trust and daily limit.
// Generic recipients must match the runtime portrait and the previously read name.
type StatusRecognition struct{}

type giftStatus struct {
	CompletedRings *int   `json:"completed_rings"`
	Preview        bool   `json:"preview,omitempty"`
	Trust          *int   `json:"trust"`
	Limited        *bool  `json:"limited"`
	Name           string `json:"name,omitempty"`
	IdentityMatch  bool   `json:"identity_match"`
}

// Run validates recipient identity and returns stable gift-screen status.
func (r *StatusRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	s, err := loadSession(ctx)
	if err != nil {
		return nil, false
	}
	status, box, err := readGiftStatus(ctx, arg.Img, s, arg.CurrentTaskName == "GiftOperatorSendVerify", false)
	if err != nil || status == nil {
		return nil, false
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: box, Detail: string(encoded)}, true
}

// readGiftStatus shares identity checks while keeping preview separate from the
// actual before/after observations used to count successful recipients.
func readGiftStatus(runner portraitRunner, img image.Image, s session, verify, allowSelection bool) (*giftStatus, maa.Rect, error) {
	ui, err := runner.RunRecognition("GiftOperatorGiftUI", img)
	if err != nil || ui == nil || !ui.Hit {
		return nil, maa.Rect{}, err
	}
	if !allowSelection {
		selected, err := runner.RunRecognition("GiftOperatorGiftSelectedLabelColor", img)
		if err != nil || (selected != nil && selected.Hit) {
			return nil, maa.Rect{}, err
		}
	}
	status := giftStatus{IdentityMatch: true}
	if s.Generic {
		portrait, ok := pendingPortrait(s)
		if !ok {
			return nil, maa.Rect{}, nil
		}
		if err := restorePortrait(runner, portrait); err != nil {
			log.Error().Err(err).Str("component", "GiftOperatorGiftStatusRecognition").Msg("restore runtime portrait failed")
			return nil, maa.Rect{}, nil
		}
		name, err := runner.RunRecognition("GiftOperatorGiftName", img)
		if err != nil || name == nil || !name.Hit || len(name.CombinedResult) != 2 {
			return nil, maa.Rect{}, nil
		}
		// CombinedResult children do not populate Hit; the parent And already succeeded.
		nameText, ok := ocrResultText(name.CombinedResult[1])
		if !ok {
			return nil, maa.Rect{}, nil
		}
		status.Name = normalizeName(nameText)
		if status.Name == "" {
			return nil, maa.Rect{}, nil
		}
		match, err := runner.RunRecognitionDirect(maa.RecognitionTypeTemplateMatch, &maa.TemplateMatchParam{
			ROI:      maa.NewTargetRect(maa.Rect{1170, 155, 80, 75}),
			Template: giftPortraitTemplates(portrait.Template), Threshold: []float64{0.8},
			Method: maa.TemplateMatchMethodCCOEFF_NORMED,
		}, img)
		if err != nil {
			log.Error().Err(err).Str("component", "GiftOperatorGiftStatusRecognition").Msg("match runtime gift portrait failed")
			return nil, maa.Rect{}, nil
		}
		status.IdentityMatch = match != nil && match.Hit && (portrait.Name == "" || portrait.Name == status.Name)
		log.Info().Str("component", "GiftOperatorGiftStatusRecognition").Str("operator", s.Pending).
			Str("name", status.Name).Bool("identity_match", status.IdentityMatch).Msg("verified gift recipient identity")
		if !status.IdentityMatch {
			if verify {
				return nil, maa.Rect{}, nil
			}
			return &status, name.Box, nil
		}
	}
	detail, err := runner.RunRecognition("GiftOperatorGiftTrust", img)
	if err != nil {
		return nil, maa.Rect{}, err
	}
	text, ok := bestOCRText(detail)
	if !ok {
		return nil, maa.Rect{}, nil
	}
	trust, err := parseTrust(text)
	if err != nil {
		log.Warn().Err(err).Str("component", "GiftOperatorGiftStatusRecognition").Str("text", text).Msg("parse gift trust failed")
		return nil, maa.Rect{}, nil
	}
	limit, err := runner.RunRecognition("GiftOperatorDailyLimitText", img)
	if err != nil {
		return nil, maa.Rect{}, nil
	}
	limited := limit != nil && limit.Hit
	status.Trust, status.Limited = &trust, &limited
	if limited {
		rings := 3
		status.CompletedRings = &rings
	} else {
		tier, err := runner.RunRecognition("GiftOperatorGiftTier", img)
		if err != nil {
			return nil, maa.Rect{}, err
		}
		text, ok := bestOCRText(tier)
		if ok {
			rings, err := parseGiftTier(text)
			if err == nil {
				status.CompletedRings = &rings
			}
		}
		// Max total trust is terminal evidence even when its tier is absent.
		if status.CompletedRings == nil && trust != 200 {
			return nil, maa.Rect{}, nil
		}
	}
	return &status, detail.Box, nil
}

func bestOCRText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || !detail.Hit {
		return "", false
	}
	return ocrResultText(detail)
}

// ocrResultText reads the best OCR result after its recognition hit is established.
func ocrResultText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || detail.Algorithm != "OCR" {
		return "", false
	}
	var result struct {
		Best *maa.OCRResult `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &result); err != nil || result.Best == nil {
		return "", false
	}
	return result.Best.Text, true
}

// parseGiftTier maps the remaining daily gain tier to already completed rings.
// Unknown OCR is never treated as zero progress or daily-full evidence.
func parseGiftTier(text string) (int, error) {
	text = strings.ToLower(strings.Join(strings.Fields(text), ""))
	switch text {
	case "高", "high", "高い", "높음":
		return 0, nil
	case "中", "medium", "moderate", "보통":
		return 1, nil
	case "低", "low", "低い", "낮음":
		return 2, nil
	default:
		return 0, fmt.Errorf("unknown daily gift tier %q", text)
	}
}

func parseTrust(text string) (int, error) {
	text = strings.Join(strings.Fields(text), "")
	text = strings.ReplaceAll(text, "％", "%")
	if !strings.HasSuffix(text, "%") {
		return 0, fmt.Errorf("trust has no percent sign")
	}
	value, err := strconv.Atoi(strings.TrimSuffix(text, "%"))
	if err != nil || value < 0 || value > 200 {
		return 0, fmt.Errorf("invalid trust percentage %q", text)
	}
	return value, nil
}
