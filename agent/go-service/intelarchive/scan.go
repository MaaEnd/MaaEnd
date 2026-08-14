package intelarchive

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Pipeline node names for nested recognition invoked by ScanItems.
const itemRecognitionNode = "IntelArchiveRecognitionItemText"

// Leave empty until the detail-resolve pipeline is ready.
const truncatedItemNode = ""

var _ maa.CustomRecognitionRunner = &ScanItems{}
var _ maa.CustomActionRunner = &ResolveTruncatedItems{}

type truncatedItem struct {
	Text string `json:"text"`
	Box  []int  `json:"box"` // [x, y, w, h]
}

type ocrItem struct {
	Text string
	Box  []int
}

// ScanItems scans the current screen, resolves the account UID, matches OCR item
// names against the Intel Archive catalog, and persists newly unlocked item IDs.
type ScanItems struct{}

type ResolveTruncatedItems struct{}

func (a *ResolveTruncatedItems) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	items := parseTruncated(arg.RecognitionDetail)
	if truncatedItemNode == "" || len(items) == 0 {
		return true
	}
	for _, item := range items {
		if _, err := ctx.RunTask(truncatedItemNode); err != nil {
			log.Error().
				Err(err).
				Str("component", component).
				Str("ocr", item.Text).
				Ints("box", item.Box).
				Msg("truncated item pipeline failed")
		}
	}
	return true
}

func parseTruncated(detail *maa.RecognitionDetail) []truncatedItem {
	if detail == nil || detail.DetailJson == "" {
		return nil
	}
	raw := detail.DetailJson
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && len(wrapped.Best.Detail) > 0 {
		raw = string(wrapped.Best.Detail)
		if wrapped.Best.Detail[0] == '"' {
			var s string
			if json.Unmarshal(wrapped.Best.Detail, &s) == nil {
				raw = s
			}
		}
	}
	var payload struct {
		Truncated []truncatedItem `json:"truncated"`
	}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.Truncated
}

func (r *ScanItems) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil || ctx == nil {
		return nil, false
	}
	if itemRecognitionNode == "" {
		log.Error().Str("component", component).Str("step", "scan_items").Msg("item recognition pipeline node is not configured")
		return nil, false
	}

	// UID placeholder; real capture is handled outside this recognition for now.
	uid := "123456789"

	itemDetail, err := ctx.RunRecognition(itemRecognitionNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("item recognition failed")
		return nil, false
	}
	items, err := extractOCRItems(itemDetail)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("item recognition failed")
		return nil, false
	}

	idx, err := loadCatalogIndex()
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("catalog lookup failed")
		return nil, false
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Text)
	}
	log.Info().
		Str("component", component).
		Str("step", "scan_items").
		Str("uid", uid).
		Int("ocr_count", len(names)).
		Strs("ocr_texts", names).
		Msg("scan items ocr texts")

	matchedIDs := make([]string, 0, len(items))
	matchedNames := make([]string, 0, len(items))
	unmatched := make([]string, 0)
	truncated := make([]truncatedItem, 0)
	for _, item := range items {
		name := item.Text
		if name == "" {
			continue
		}
		if strings.Contains(name, "...") || strings.Contains(name, "…") {
			truncated = append(truncated, truncatedItem{Text: name, Box: item.Box})
			continue
		}
		id := ""
		fullName := ""
		for std, itemID := range idx.NameToID {
			if strings.HasPrefix(std, name) {
				id = itemID
				fullName = std
				break
			}
		}
		if id == "" {
			log.Info().
				Str("component", component).
				Str("step", "scan_items").
				Str("uid", uid).
				Str("ocr", name).
				Bool("matched", false).
				Msg("catalog lookup miss")
			maafocus.Print(ctx, i18n.T("intelarchive.item_not_found", name))
			unmatched = append(unmatched, name)
			continue
		}
		log.Info().
			Str("component", component).
			Str("step", "scan_items").
			Str("uid", uid).
			Str("ocr", name).
			Bool("matched", true).
			Str("full_name", fullName).
			Str("item_id", id).
			Msg("catalog lookup hit")
		maafocus.Print(ctx, i18n.T("intelarchive.item_unlocked", fullName))
		matchedIDs = append(matchedIDs, id)
		matchedNames = append(matchedNames, fullName)
	}

	added, err := unlockItems(uid, matchedIDs)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Str("step", "scan_items").
			Str("uid", uid).
			Msg("persist unlocked failed")
		return nil, false
	}

	log.Info().
		Str("component", component).
		Str("step", "scan_items").
		Str("uid", uid).
		Int("ocr_count", len(names)).
		Int("matched_count", len(matchedIDs)).
		Int("unmatched_count", len(unmatched)).
		Int("truncated_count", len(truncated)).
		Int("added_count", len(added)).
		Strs("ocr_texts", names).
		Strs("matched_names", matchedNames).
		Strs("matched_ids", matchedIDs).
		Strs("unmatched", unmatched).
		Interface("truncated", truncated).
		Strs("added", added).
		Msg("scan items finished")

	detailBytes, _ := json.Marshal(map[string][]truncatedItem{"truncated": truncated})
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailBytes),
	}, true
}

// extractOCRItems reads OCR filtered text+box from CombinedResult[4].DetailJson.
// IntelArchiveRecognitionItemText.all_of has 5 entries; the inline OCR is index 4.
func extractOCRItems(detail *maa.RecognitionDetail) ([]ocrItem, error) {
	if detail == nil {
		return nil, nil
	}
	if !detail.Hit {
		log.Warn().Str("component", component).Str("step", "scan_items").Msg("recognition not hit")
		return nil, nil
	}
	if len(detail.CombinedResult) < 5 || detail.CombinedResult[4] == nil {
		log.Warn().Str("component", component).Str("step", "scan_items").Msg("recognition miss combined result")
		return nil, nil
	}

	var detailJSON struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail.CombinedResult[4].DetailJson), &detailJSON); err != nil {
		return nil, err
	}

	items := make([]ocrItem, 0, len(detailJSON.Filtered))
	for _, item := range detailJSON.Filtered {
		if text := strings.TrimSpace(item.Text); text != "" {
			items = append(items, ocrItem{Text: text, Box: item.Box})
		}
	}
	return items, nil
}
