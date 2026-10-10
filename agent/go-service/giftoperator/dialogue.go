package giftoperator

import (
	"encoding/json"
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &DialogueRecognition{}

// DialogueRecognition reads visible interaction names without an operator list.
// Portrait verification in the gift screen still decides whether gifting is safe.
type DialogueRecognition struct{}

// Run chooses a visible interaction name that has not already been handled.
func (r *DialogueRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	s, err := loadSession(ctx)
	if err != nil || !s.Generic || s.Pending == "" {
		return nil, false
	}
	result, err := recognizeDialogue(ctx, arg.Img, s)
	if err != nil {
		log.Error().Err(err).Str("component", "GiftOperatorDialogueRecognition").Msg("read interaction names failed")
		return nil, false
	}
	return result, result != nil
}

func recognizeDialogue(runner recognitionRunner, img image.Image, s session) (*maa.CustomRecognitionResult, error) {
	chat, err := runner.RunRecognition("GiftOperatorChat", img)
	if err != nil || chat == nil || !chat.Hit || chat.Results == nil {
		return nil, err
	}
	for _, result := range chat.Results.Filtered {
		icon, ok := result.AsTemplateMatch()
		if !ok || icon == nil {
			continue
		}
		// Same anchor and offset as GiftOperatorName; use every visible chat icon.
		roi := maa.Rect{icon.Box[0] + 30, icon.Box[1], icon.Box[2] + 65, icon.Box[3]}
		name, err := runner.RunRecognition("GiftOperatorName", img, map[string]any{
			"GiftOperatorName": map[string]any{"roi": roi, "roi_offset": maa.Rect{}, "expected": []string{".+"}},
		})
		if err != nil {
			return nil, err
		}
		if name == nil || !name.Hit || name.Results == nil || name.Results.Best == nil {
			continue
		}
		ocr, ok := name.Results.Best.AsOCR()
		if !ok || ocr == nil {
			continue
		}
		text := normalizeName(ocr.Text)
		if text == "" || blockedDialogueName(s, text) {
			continue
		}
		detail, err := json.Marshal(map[string]string{"name": text})
		if err != nil {
			return nil, err
		}
		return &maa.CustomRecognitionResult{Box: ocr.Box, Detail: string(detail)}, nil
	}
	return nil, nil
}

func blockedDialogueName(s session, name string) bool {
	for _, previous := range s.AvoidNames {
		if normalizeName(previous) == name {
			return true
		}
	}
	for _, portrait := range s.Portraits {
		if (containsOperator(s.Completed, portrait.Operator) || containsOperator(s.Excluded, portrait.Operator)) &&
			(normalizeName(portrait.Name) == name || normalizeName(portrait.DialogueName) == name) {
			return true
		}
	}
	return false
}

func normalizeName(name string) string { return strings.Join(strings.Fields(name), " ") }
