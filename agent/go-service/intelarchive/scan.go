package intelarchive

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Pipeline node names are left empty until the corresponding recognition nodes are ready.
const (
	uidRecognitionNode  = ""
	itemRecognitionNode = "IntelArchiveRecognitionItemText"
)

var _ maa.CustomRecognitionRunner = &ScanItems{}

// ScanItems scans the current screen, resolves the account UID, matches OCR item
// names against the Intel Archive catalog, and persists newly unlocked item IDs.
type ScanItems struct{}

func (r *ScanItems) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil || ctx == nil {
		return nil, false
	}
	if uidRecognitionNode == "" {
		log.Error().Str("component", component).Str("step", "scan_items").Msg("uid recognition pipeline node is not configured")
		return nil, false
	}
	if itemRecognitionNode == "" {
		log.Error().Str("component", component).Str("step", "scan_items").Msg("item recognition pipeline node is not configured")
		return nil, false
	}

	// uidDetail, err := ctx.RunRecognition(uidRecognitionNode, arg.Img)
	// if err != nil {
	// 	log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("uid recognition failed")
	// 	return nil, false
	// }
	// uidTexts, err := extractFilteredTexts(uidDetail)
	// if err != nil {
	// 	log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("uid recognition failed")
	// 	return nil, false
	// }
	uid := "123456789"
	// for _, text := range uidTexts {
	// 	if text = strings.TrimSpace(text); text != "" {
	// 		uid = text
	// 		break
	// 	}
	// }
	// if uid == "" {
	// 	log.Error().Str("component", component).Str("step", "scan_items").Msg("uid recognition failed")
	// 	return nil, false
	// }

	itemDetail, err := ctx.RunRecognition(itemRecognitionNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("item recognition failed")
		return nil, false
	}
	names, err := extractFilteredTexts(itemDetail)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("item recognition failed")
		return nil, false
	}

	idx, err := loadCatalogIndex()
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("step", "scan_items").Msg("catalog lookup failed")
		return nil, false
	}

	matchedIDs := make([]string, 0, len(names))
	unmatched := make([]string, 0)
	for _, name := range names {
		name = strings.TrimSpace(name)
		name = strings.TrimSuffix(name, "...")
		name = strings.TrimSuffix(name, "…")
		name = strings.TrimSpace(name)
		if name == "" {
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
			maafocus.Print(ctx, i18n.T("intelarchive.item_not_found", name))
			unmatched = append(unmatched, name)
			continue
		}
		maafocus.Print(ctx, i18n.T("intelarchive.item_unlocked", fullName))
		matchedIDs = append(matchedIDs, id)
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
		Int("added_count", len(added)).
		Strs("unmatched", unmatched).
		Strs("added", added).
		Msg("scan items finished")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom":"intelarchive_scan_items"}`,
	}, true
}

// extractFilteredTexts reads OCR filtered texts from CombinedResult[4].DetailJson.
// IntelArchiveRecognitionItemText.all_of has 5 entries; the inline OCR is index 4.
func extractFilteredTexts(detail *maa.RecognitionDetail) ([]string, error) {
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
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail.CombinedResult[4].DetailJson), &detailJSON); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(detailJSON.Filtered))
	for _, item := range detailJSON.Filtered {
		if text := strings.TrimSpace(item.Text); text != "" {
			names = append(names, text)
		}
	}
	return names, nil
}
