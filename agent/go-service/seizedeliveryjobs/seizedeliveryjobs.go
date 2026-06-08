package seizedeliveryjobs

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type deliveryJobItem struct {
	RewardBox       []int  `json:"reward_box"`
	OriginText      string `json:"origin_text"`
	AcceptBox       []int  `json:"accept_box"`
	ViewLocationBox []int  `json:"view_location_box"`
}

// filteredDetail holds the parsed OCR sub-recognition result.
// The Text field is only populated for origin (index 1); others leave it zero.
type filteredDetail struct {
	Filtered []struct {
		Box   []int   `json:"box"`
		Score float64 `json:"score"`
		Text  string  `json:"text"`
	} `json:"filtered"`
}

var (
	scannedJobItems []deliveryJobItem
	currentIndex    int
)

// boxToRect converts a [x, y, w, h] box slice to maa.Rect.
func boxToRect(box []int) maa.Rect {
	return maa.Rect{box[0], box[1], box[2], box[3]}
}

// SeizeDeliveryJobsResetScanStateAction resets scan state (items + index).
// Used by both EndpointMatched and ScanExhausted nodes.
type SeizeDeliveryJobsResetScanStateAction struct{}

func (a *SeizeDeliveryJobsResetScanStateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	scannedJobItems = nil
	currentIndex = 0
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "reset_scan_state").
		Msg("scan state cleared")
	return true
}

// SeizeDeliveryJobsScanTargetRecognition scans the delivery job list once and caches OCR results for subsequent ScanTarget iterations.
type SeizeDeliveryJobsScanTargetRecognition struct{}

func (r *SeizeDeliveryJobsScanTargetRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// Subsequent calls: already have scanned data, just hit
	if scannedJobItems != nil {
		log.Debug().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Int("remaining", len(scannedJobItems)-currentIndex).
			Msg("reusing existing scan data")
		return &maa.CustomRecognitionResult{
			Box: arg.Roi,
		}, true
	}

	// First call: OCR scan to build items
	detail, recoErr := ctx.RunRecognition("SeizeDeliveryJobsFindTarget", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("run recognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 5 {
		log.Warn().Str("component", "SeizeDeliveryJobs").Str("step", "scan_target").Msg("recognition miss")
		return nil, false
	}

	// Parse all 4 sub-recognition results (skip index 0 = WulingToken)
	var details [4]filteredDetail
	subNames := [4]string{"reward", "origin", "accept", "view_location"}
	for i := range details {
		if err := json.Unmarshal([]byte(detail.CombinedResult[i+1].DetailJson), &details[i]); err != nil {
			log.Error().Err(err).
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_target").
				Str("sub", subNames[i]).
				Msg("parse detail json")
			return nil, false
		}
	}

	// Verify all sub-results have the same count
	n := len(details[0].Filtered)
	for i := 1; i < 4; i++ {
		if len(details[i].Filtered) != n {
			log.Warn().
				Ints("counts", []int{
					len(details[0].Filtered), len(details[1].Filtered),
					len(details[2].Filtered), len(details[3].Filtered),
				}).
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_target").
				Msg("recognition count mismatch")
			return nil, false
		}
	}

	// Build all items from the filtered results
	items := make([]deliveryJobItem, 0, n)
	for i := range details[0].Filtered {
		items = append(items, deliveryJobItem{
			RewardBox:       details[0].Filtered[i].Box,
			OriginText:      details[1].Filtered[i].Text,
			AcceptBox:       details[2].Filtered[i].Box,
			ViewLocationBox: details[3].Filtered[i].Box,
		})
	}
	scannedJobItems = items

	origins := make([]string, 0, len(items))
	for _, it := range items {
		origins = append(origins, it.OriginText)
	}
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "scan_target").
		Int("item_count", len(items)).
		Strs("origins", origins).
		Msg("scanned job items")

	return &maa.CustomRecognitionResult{
		Box: arg.Roi,
	}, true
}

// SeizeDeliveryJobsScanTargetAction overrides pipeline click targets for the current scanned job item and advances the scan index.
type SeizeDeliveryJobsScanTargetAction struct{}

func (a *SeizeDeliveryJobsScanTargetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// All items exhausted → on_error: ScanExhausted → Refresh
	if scannedJobItems == nil || currentIndex >= len(scannedJobItems) {
		log.Info().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Int("total", len(scannedJobItems)).
			Msg("all items scanned, will refresh")
		return false
	}

	item := scannedJobItems[currentIndex]
	maafocus.Print(ctx, i18n.T("seizedeliveryjobs.checking_job", currentIndex+1, len(scannedJobItems)))

	if len(item.ViewLocationBox) < 4 {
		log.Error().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Int("box_len", len(item.ViewLocationBox)).
			Msg("view location box invalid")
		return false
	}
	if len(item.AcceptBox) < 4 {
		log.Error().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Int("box_len", len(item.AcceptBox)).
			Msg("accept box invalid")
		return false
	}

	viewRect := boxToRect(item.ViewLocationBox)
	acceptRect := boxToRect(item.AcceptBox)

	log.Debug().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "scan_action").
		Int("index", currentIndex).
		Ints("view_location_box", item.ViewLocationBox).
		Ints("accept_box", item.AcceptBox).
		Msg("overriding pipeline targets")

	if err := ctx.OverridePipeline(map[string]any{
		"SeizeDeliveryJobsFoundTargetViewLocationClick": map[string]any{"target": viewRect},
		"SeizeDeliveryJobsAcceptClick":                  map[string]any{"target": acceptRect},
		"SeizeDeliveryJobsRetryClickAccept":             map[string]any{"target": acceptRect},
	}); err != nil {
		log.Error().Err(err).
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_action").
			Int("index", currentIndex).
			Msg("override pipeline failed")
		return false
	}

	currentIndex++
	return true
}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsResetScanStateAction{}
	_ maa.CustomRecognitionRunner = &SeizeDeliveryJobsScanTargetRecognition{}
	_ maa.CustomActionRunner      = &SeizeDeliveryJobsScanTargetAction{}
)
