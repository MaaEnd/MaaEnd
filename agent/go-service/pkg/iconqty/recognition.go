// Package iconqty runs IconRecognition on an item grid and OCRs stack
// quantities from each match cell_box. Shared by IMS SyncItemData (A2) and
// AddItemData (A3).
package iconqty

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	recognitionName = "IconRecognition"

	// GridValuables is IconRecognition grid_type for 贵重品库.
	GridValuables = "valuables"
	// GridRewards is IconRecognition grid_type for 奖励界面.
	GridRewards = "rewards"
)

// Default ROIs from docs/zh_cn/developers/components/icon-recognition.md (1280x720).
var (
	defaultValuablesROIWin32 = []int{24, 76, 950, 570}
	defaultValuablesROIADB   = []int{100, 85, 790, 540}
	defaultRewardsROIWin32   = []int{39, 82, 1205, 511}
	defaultRewardsROIADB     = []int{178, 140, 935, 440}
)

// Request is the IconRecognition scan parameter for RecognizeQuantities.
type Request struct {
	GridType    string
	ROI         []int
	ItemFilters []string
	Deduplicate bool
}

// Match is one IconRecognition hit.
type Match struct {
	ItemID  string
	CellBox maa.Rect
	ItemBox maa.Rect
	Score   float64
	hasCell bool
	hasItem bool
}

// CellOK reports whether CellBox is valid.
func (m Match) CellOK() bool { return m.hasCell }

// ItemOK reports whether ItemBox is valid.
func (m Match) ItemOK() bool { return m.hasItem }

type recognitionMatchJSON struct {
	ItemID  string `json:"item_id"`
	CellBox *struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"cell_box"`
	ItemBox *struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"item_box"`
	Score float64 `json:"score"`
}

type recognitionDetailJSON struct {
	Matched bool                   `json:"matched"`
	Matches []recognitionMatchJSON `json:"matches"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func isADBController() bool {
	return strings.EqualFold(strings.TrimSpace(pienv.ControllerType()), "Adb")
}

// DefaultROI returns the reference ROI for gridType on the current controller.
func DefaultROI(gridType string) []int {
	adb := isADBController()
	switch strings.TrimSpace(gridType) {
	case GridValuables:
		if adb {
			return append([]int(nil), defaultValuablesROIADB...)
		}
		return append([]int(nil), defaultValuablesROIWin32...)
	case GridRewards:
		if adb {
			return append([]int(nil), defaultRewardsROIADB...)
		}
		return append([]int(nil), defaultRewardsROIWin32...)
	default:
		return nil
	}
}

// DefaultItemFilters returns IconRecognition default item_filters for gridType
// when the caller omits filters (see icon-recognition docs).
func DefaultItemFilters(gridType string) []string {
	switch strings.TrimSpace(gridType) {
	case GridValuables:
		return []string{"ValuableDepot:*"}
	case GridRewards:
		return []string{"Isolate:*", "ValuableDepot:*"}
	default:
		return nil
	}
}

// NormalizeStringList trims, rejects empties/duplicates, and returns a copy.
func NormalizeStringList(values []string, label string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, fmt.Errorf("%s contains empty value", label)
		}
		if _, dup := seen[v]; dup {
			return nil, fmt.Errorf("%s contains duplicate value: %s", label, v)
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

func normalizeROI(roi []int, gridType string) ([]int, error) {
	if len(roi) == 0 {
		roi = DefaultROI(gridType)
	}
	if len(roi) != 4 {
		return nil, fmt.Errorf("roi must have 4 ints [x,y,w,h]")
	}
	if roi[2] <= 0 || roi[3] <= 0 {
		return nil, fmt.Errorf("roi width and height must be positive")
	}
	out := make([]int, 4)
	copy(out, roi)
	return out, nil
}

// Recognize runs one IconRecognition pass and returns all matches.
func Recognize(ctx *maa.Context, img image.Image, req Request) ([]Match, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	gridType := strings.TrimSpace(req.GridType)
	if gridType == "" {
		return nil, fmt.Errorf("grid_type is required")
	}
	roi, err := normalizeROI(req.ROI, gridType)
	if err != nil {
		return nil, err
	}
	filters, err := NormalizeStringList(req.ItemFilters, "item_filters")
	if err != nil {
		return nil, err
	}

	customParam := map[string]any{
		"grid_type":   gridType,
		"deduplicate": req.Deduplicate,
	}
	if len(filters) > 0 {
		customParam["item_filters"] = filters
	}

	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:                    maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]}),
			CustomRecognition:      recognitionName,
			CustomRecognitionParam: customParam,
		},
		img,
	)
	if err != nil {
		return nil, fmt.Errorf("run IconRecognition: %w", err)
	}

	parsed, err := parseRecognitionDetail(detail)
	if err != nil {
		return nil, err
	}
	if parsed.Error != nil && parsed.Error.Code != "" && parsed.Error.Code != "no_match" {
		return nil, fmt.Errorf("IconRecognition %s: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if !parsed.Matched || len(parsed.Matches) == 0 {
		return nil, nil
	}

	out := make([]Match, 0, len(parsed.Matches))
	for _, m := range parsed.Matches {
		hit := Match{
			ItemID: strings.TrimSpace(m.ItemID),
			Score:  m.Score,
		}
		if m.CellBox != nil && m.CellBox.Width > 0 && m.CellBox.Height > 0 {
			hit.CellBox = maa.Rect{m.CellBox.X, m.CellBox.Y, m.CellBox.Width, m.CellBox.Height}
			hit.hasCell = true
		}
		if m.ItemBox != nil && m.ItemBox.Width > 0 && m.ItemBox.Height > 0 {
			hit.ItemBox = maa.Rect{m.ItemBox.X, m.ItemBox.Y, m.ItemBox.Width, m.ItemBox.Height}
			hit.hasItem = true
		}
		out = append(out, hit)
	}
	return out, nil
}

func parseRecognitionDetail(detail *maa.RecognitionDetail) (recognitionDetailJSON, error) {
	var out recognitionDetailJSON
	if detail == nil {
		return out, fmt.Errorf("recognition detail is nil")
	}
	raw := extractCustomDetailJSON(detail)
	if strings.TrimSpace(raw) == "" {
		return out, fmt.Errorf("IconRecognition detail is empty")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return recognitionDetailJSON{}, fmt.Errorf("unmarshal IconRecognition detail: %w", err)
	}
	return out, nil
}

func extractCustomDetailJSON(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}
	if detail.Results != nil {
		if best := detail.Results.Best; best != nil {
			if custom, ok := best.AsCustom(); ok && custom != nil && strings.TrimSpace(custom.Detail) != "" {
				return custom.Detail
			}
		}
		for _, result := range detail.Results.All {
			if result == nil {
				continue
			}
			if custom, ok := result.AsCustom(); ok && custom != nil && strings.TrimSpace(custom.Detail) != "" {
				return custom.Detail
			}
		}
	}
	if strings.TrimSpace(detail.DetailJson) == "" {
		return ""
	}
	var wrapped struct {
		Best *struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
		All []struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"all"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &wrapped); err != nil {
		return detail.DetailJson
	}
	if wrapped.Best != nil && len(wrapped.Best.Detail) > 0 {
		return string(wrapped.Best.Detail)
	}
	if len(wrapped.All) > 0 && len(wrapped.All[0].Detail) > 0 {
		return string(wrapped.All[0].Detail)
	}
	return detail.DetailJson
}
