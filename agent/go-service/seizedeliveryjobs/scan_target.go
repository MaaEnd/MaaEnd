package seizedeliveryjobs

import (
	"encoding/json"
	"fmt"

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

	// First call: scan every active reward tier and merge.
	// SeizeDeliveryJobsFindTarget is an Or that short-circuits to the first matched tier — right for
	// the grab path (one commission, highest price), wrong for a full scan. So read the active tier
	// node names from FindTarget's any_of (reflects the selected reward option) and run each template
	// directly; each template's own CombinedResult is its 5 flat leaves.
	tiers, err := readRewardTierNodes(ctx)
	if err != nil || len(tiers) == 0 {
		log.Error().Err(err).
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Msg("read reward tier nodes")
		return nil, false
	}

	var items []deliveryJobItem
	for _, tier := range tiers {
		d, err := ctx.RunRecognition(tier, arg.Img)
		if err != nil {
			log.Error().Err(err).
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_target").
				Str("tier", tier).
				Msg("run recognition")
			continue
		}
		if d == nil {
			log.Warn().
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_target").
				Str("tier", tier).
				Msg("recognition returned nil detail")
			continue
		}
		if !d.Hit {
			continue // this tier matched nothing on the current list
		}
		tierItems, ok := parseTierLeaves(d.CombinedResult, tier)
		if !ok {
			continue
		}
		log.Debug().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Str("tier", tier).
			Int("count", len(tierItems)).
			Msg("tier scanned")
		items = append(items, tierItems...) // any_of order → higher reward tier first
	}

	if len(items) == 0 {
		log.Warn().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Strs("tiers", tiers).
			Msg("recognition miss")
		return nil, false
	}
	scannedJobItems = items

	origins := make([]string, 0, len(items))
	for _, it := range items {
		origins = append(origins, it.OriginText)
	}
	log.Info().
		Str("component", "SeizeDeliveryJobs").
		Str("step", "scan_target").
		Strs("tiers", tiers).
		Int("item_count", len(items)).
		Strs("origins", origins).
		Msg("scanned job items")

	return &maa.CustomRecognitionResult{
		Box: arg.Roi,
	}, true
}

// readRewardTierNodes returns the active reward-tier template node names from
// SeizeDeliveryJobsFindTarget's any_of (overridden by the selected reward option).
// Only node-name references are collected; inline recognition objects are skipped.
func readRewardTierNodes(ctx *maa.Context) ([]string, error) {
	raw, err := ctx.GetNodeJSON("SeizeDeliveryJobsFindTarget")
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil, err
	}
	// any_of sits at the node top level in pipeline V1, or under recognition.param in V2.
	var anyOfRaw json.RawMessage
	if recRaw, ok := top["recognition"]; ok {
		var rec map[string]json.RawMessage
		if json.Unmarshal(recRaw, &rec) == nil { // V2: recognition is an object {type, param}
			if pRaw, ok := rec["param"]; ok {
				var p map[string]json.RawMessage
				if json.Unmarshal(pRaw, &p) == nil {
					anyOfRaw = p["any_of"]
				}
			}
			if len(anyOfRaw) == 0 {
				anyOfRaw = rec["any_of"]
			}
		}
	}
	if len(anyOfRaw) == 0 {
		anyOfRaw = top["any_of"] // V1 top-level
	}
	if len(anyOfRaw) == 0 {
		return nil, fmt.Errorf("no any_of found in SeizeDeliveryJobsFindTarget")
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(anyOfRaw, &elems); err != nil {
		return nil, err
	}
	var names []string
	for _, e := range elems {
		var s string
		if json.Unmarshal(e, &s) == nil {
			names = append(names, s) // node-name reference
			continue
		}
		// Inline recognition objects are valid per protocol but not used as tier names here.
		var obj map[string]json.RawMessage
		if json.Unmarshal(e, &obj) == nil {
			continue // inline recognition — skip silently
		}
		// Anything else (number, bool, null, malformed) is a config error worth surfacing.
		log.Warn().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Str("element", string(e)).
			Msg("any_of element is neither a node name nor an inline recognition; skipping")
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no tier node names found in SeizeDeliveryJobsFindTarget.any_of")
	}
	return names, nil
}

// parseTierLeaves parses a reward-tier template's CombinedResult (5 flat leaves:
// WulingToken, RewardOcr, OriginOcr, AcceptOcr, ViewLocationOcr — index 0 skipped) into items.
// tier is included in diagnostics for traceability.
func parseTierLeaves(combined []*maa.RecognitionDetail, tier string) ([]deliveryJobItem, bool) {
	if len(combined) < 5 {
		log.Warn().
			Str("component", "SeizeDeliveryJobs").
			Str("step", "scan_target").
			Str("tier", tier).
			Int("combined_len", len(combined)).
			Msg("tier CombinedResult has fewer than 5 leaves")
		return nil, false
	}
	// len >= 5 guarantees the slice slots exist, but each *RecognitionDetail could be nil if a tier
	// returns a sparse/partial CombinedResult; guard before dereferencing.
	for _, c := range combined[:5] {
		if c == nil {
			log.Warn().
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_target").
				Str("tier", tier).
				Msg("tier CombinedResult contains a nil leaf")
			return nil, false
		}
	}
	var details [4]filteredDetail
	subNames := [4]string{"reward", "origin", "accept", "view_location"}
	for i := range details {
		if err := json.Unmarshal([]byte(combined[i+1].DetailJson), &details[i]); err != nil {
			log.Error().Err(err).
				Str("component", "SeizeDeliveryJobs").
				Str("step", "scan_target").
				Str("tier", tier).
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
				Str("tier", tier).
				Msg("recognition count mismatch")
			return nil, false
		}
	}

	items := make([]deliveryJobItem, 0, n)
	for i := range details[0].Filtered {
		items = append(items, deliveryJobItem{
			RewardBox:       details[0].Filtered[i].Box,
			OriginText:      details[1].Filtered[i].Text,
			AcceptBox:       details[2].Filtered[i].Box,
			ViewLocationBox: details[3].Filtered[i].Box,
		})
	}
	return items, true
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
