// Package iconqty runs IconRecognition on an item grid and OCRs stack
// quantities from each match cell_box. Shared by IMS SyncItemData (A2) and
// AddItemData (A3). Recognition params and detail parsing go through
// pkg/iconrecognition; this package keeps quantity OCR and default ROIs.
package iconqty

import (
	"fmt"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	// GridValuables is IconRecognition grid_type for 贵重品库.
	GridValuables = string(iconrecognition.GridTypeValuables)
	// GridRewards is IconRecognition grid_type for 奖励界面.
	GridRewards = string(iconrecognition.GridTypeRewards)
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
	ItemIDs     []string
	Deduplicate bool
}

// Match is one IconRecognition hit used by quantity OCR.
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

func isADBController() bool {
	return strings.EqualFold(strings.TrimSpace(pienv.ControllerType()), "Adb")
}

// DefaultROI returns the reference ROI for gridType on the current controller.
func DefaultROI(gridType string) []int {
	adb := isADBController()
	switch iconrecognition.GridType(strings.TrimSpace(gridType)) {
	case iconrecognition.GridTypeValuables:
		if adb {
			return append([]int(nil), defaultValuablesROIADB...)
		}
		return append([]int(nil), defaultValuablesROIWin32...)
	case iconrecognition.GridTypeRewards:
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
	filters := iconrecognition.StorageFilter()
	switch iconrecognition.GridType(strings.TrimSpace(gridType)) {
	case iconrecognition.GridTypeValuables:
		return []string{string(filters.ValuableDepot.Any)}
	case iconrecognition.GridTypeRewards:
		return []string{string(filters.Isolate.Any), string(filters.ValuableDepot.Any)}
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

func itemFiltersFromStrings(values []string) []iconrecognition.ItemFilter {
	out := make([]iconrecognition.ItemFilter, len(values))
	for i, value := range values {
		out[i] = iconrecognition.ItemFilter(value)
	}
	return out
}

func matchFrom(m iconrecognition.Match) Match {
	hit := Match{
		ItemID:  strings.TrimSpace(m.ItemID),
		CellBox: m.CellBox,
		ItemBox: m.ItemBox,
		Score:   m.Score,
	}
	if m.CellBox[2] > 0 && m.CellBox[3] > 0 {
		hit.hasCell = true
	}
	if m.ItemBox[2] > 0 && m.ItemBox[3] > 0 {
		hit.hasItem = true
	}
	return hit
}

func recognitionParams(gridType string, itemIDs, filters []string, deduplicate bool) iconrecognition.Params {
	options := []iconrecognition.Option{
		iconrecognition.WithGridType(iconrecognition.GridType(gridType)),
		iconrecognition.WithDeduplicate(deduplicate),
	}
	if len(itemIDs) > 0 {
		options = append(options, iconrecognition.WithItemIDs(itemIDs...))
	}
	if len(filters) > 0 {
		options = append(options, iconrecognition.WithItemFilters(itemFiltersFromStrings(filters)...))
	}
	return iconrecognition.NewParams(options...)
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
	itemIDs, err := NormalizeStringList(req.ItemIDs, "item_ids")
	if err != nil {
		return nil, err
	}

	parsed, _, err := iconrecognition.Run(
		ctx,
		img,
		maa.Rect{roi[0], roi[1], roi[2], roi[3]},
		recognitionParams(gridType, itemIDs, filters, req.Deduplicate),
	)
	if err != nil {
		return nil, err
	}
	if parsed.Error != nil && parsed.Error.Code != "" && parsed.Error.Code != iconrecognition.ErrorCodeNoMatch {
		return nil, fmt.Errorf("IconRecognition %s: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if !parsed.Matched || len(parsed.Matches) == 0 {
		return nil, nil
	}

	out := make([]Match, 0, len(parsed.Matches))
	for _, m := range parsed.Matches {
		out = append(out, matchFrom(m))
	}
	return out, nil
}
