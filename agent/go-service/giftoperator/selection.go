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

// SelectionRecognition reads cumulative selected quantities and preview progress.
// It cannot confirm delivery; Pipeline performs every click and confirmation.
type SelectionRecognition struct{}

var _ maa.CustomRecognitionRunner = &SelectionRecognition{}

type selectionStatus struct {
	giftStatus
	SelectedCount *int `json:"selected_count,omitempty"`
}

// Run returns a preview only after its total selected quantity has increased.
func (r *SelectionRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	s, err := loadSession(ctx)
	if err != nil {
		return nil, false
	}
	status, box, err := recognizeGiftSelection(ctx, arg.Img, s)
	if err != nil {
		log.Error().Err(err).Str("component", "GiftOperatorGiftSelectionRecognition").Msg("read selected gift preview failed")
		return nil, false
	}
	if status == nil {
		return nil, false
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: box, Detail: string(encoded)}, true
}

func recognizeGiftSelection(runner portraitRunner, img image.Image, s session) (*selectionStatus, maa.Rect, error) {
	if !s.Prepared || s.Pending == "" || s.Remaining <= 0 {
		return nil, maa.Rect{}, nil
	}
	status, box, err := readGiftStatus(runner, img, s, true, true)
	if err != nil || status == nil {
		return nil, maa.Rect{}, err
	}
	count, hit, err := readSelectedCount(runner, img)
	if err != nil || !hit {
		return nil, maa.Rect{}, err
	}
	if count <= s.SelectedCount {
		return nil, maa.Rect{}, nil
	}
	status.Preview = true
	return &selectionStatus{giftStatus: *status, SelectedCount: &count}, box, nil
}

// readSelectedCount sums every visible selected label, so changing item categories
// cannot reset the observed total. A single unreadable label invalidates the frame.
func readSelectedCount(runner recognitionRunner, img image.Image) (int, bool, error) {
	detail, err := runner.RunRecognition("GiftOperatorGiftSelectedLabelColor", img)
	if err != nil || detail == nil || !detail.Hit {
		return 0, false, err
	}
	if detail.Algorithm != "ColorMatch" {
		return 0, false, fmt.Errorf("selected labels must use ColorMatch")
	}
	var labels struct {
		Filtered []maa.ColorMatchResult `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &labels); err != nil {
		return 0, false, err
	}
	if len(labels.Filtered) == 0 {
		return 0, false, nil
	}
	total := 0
	seen := make(map[maa.Rect]bool)
	for _, label := range labels.Filtered {
		if seen[label.Box] {
			continue
		}
		seen[label.Box] = true
		roi, ok := selectedTextROI(label.Box)
		if !ok || !image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3]).In(img.Bounds()) {
			return 0, false, nil
		}
		number, err := runner.RunRecognition("GiftOperatorGiftSelectedText", img, map[string]any{
			"GiftOperatorGiftSelectedText": map[string]any{
				"roi": roi, "roi_offset": maa.Rect{}, "only_rec": true, "expected": []string{`^[1-9][0-9]*$`},
			},
		})
		if err != nil {
			return 0, false, err
		}
		text, ok := bestOCRText(number)
		if !ok {
			return 0, false, nil
		}
		value, err := parseSelectedCount(text)
		if err != nil {
			log.Warn().Err(err).Str("component", "GiftOperatorGiftSelectionRecognition").Str("text", text).Interface("roi", roi).Msg("selected quantity is unreadable")
			return 0, false, nil
		}
		if total > int(^uint(0)>>1)-value {
			return 0, false, fmt.Errorf("selected quantity sum overflows")
		}
		total += value
	}
	return total, total > 0, nil
}

// selectedTextROI excludes the icon and the yellow frame below the numeric label.
// Win32 native screenshots verified [132,522,68,38] -> [163,522,37,20].
func selectedTextROI(box maa.Rect) (maa.Rect, bool) {
	if box[2] <= 31 || box[3] <= 0 {
		return maa.Rect{}, false
	}
	return maa.Rect{box[0] + 31, box[1], box[2] - 31, min(box[3], 20)}, true
}

func parseSelectedCount(text string) (int, error) {
	text = strings.Join(strings.Fields(text), "")
	if text == "" || text[0] < '1' || text[0] > '9' {
		return 0, fmt.Errorf("selected quantity must be a positive decimal integer")
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("invalid selected quantity %q", text)
		}
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("invalid selected quantity: %w", err)
	}
	return value, nil
}
