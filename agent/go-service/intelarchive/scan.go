package intelarchive

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	itemRecognitionNode   = "IntelArchiveRecognitionItemText"
	detailRecognitionNode = "IntelArchiveRecognitionDetailTitleText"
	truncatedItemNode     = "IntelArchiveResolveTrunc"
	placeholderUID        = "123456789"
)

var _ maa.CustomRecognitionRunner = &ScanItemsRecognition{}
var _ maa.CustomRecognitionRunner = &ScanDetailRecognition{}
var _ maa.CustomActionRunner = &ResolveTruncAction{}

type truncatedItem struct {
	Text string `json:"text"`
	Box  []int  `json:"box"` // [x, y, w, h]
}

// ScanItemsRecognition scans the current list screen and persists newly unlocked item IDs.
type ScanItemsRecognition struct{}

// ScanDetailRecognition OCRs the detail-page title and matches it against the catalog.
type ScanDetailRecognition struct{}

// ResolveTruncAction opens truncated list items and runs the detail-resolve pipeline.
type ResolveTruncAction struct{}

func (a *ResolveTruncAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	items := parseTruncated(arg.RecognitionDetail)
	for _, item := range items {
		if len(item.Box) != 4 || item.Box[2] <= 0 || item.Box[3] <= 0 {
			log.Error().Str("component", component).Str("ocr", item.Text).Ints("box", item.Box).Msg("truncated item box is invalid")
			continue
		}
		if err := ctx.OverridePipeline(map[string]any{
			truncatedItemNode: map[string]any{"target": item.Box},
		}); err != nil {
			log.Error().Err(err).Str("component", component).Str("ocr", item.Text).Ints("box", item.Box).Msg("failed to override truncated item click target")
			continue
		}
		detail, err := ctx.RunTask(truncatedItemNode)
		if err != nil || detail == nil || !detail.Status.Success() {
			log.Error().Err(err).Str("component", component).Str("ocr", item.Text).Ints("box", item.Box).Msg("truncated item pipeline failed")
		}
	}
	return true
}

func (r *ScanItemsRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil || ctx == nil {
		return nil, false
	}

	rec, err := ctx.RunRecognition(itemRecognitionNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("item recognition failed")
		return nil, false
	}
	// IntelArchiveRecognitionItemText.all_of：OCR 在第 5 段。
	items := ocrFiltered(rec, 4)

	idx, err := loadCatalogIndex()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load catalog")
		return nil, false
	}

	truncated := make([]truncatedItem, 0)
	names := make([]string, 0, len(items))
	for _, item := range items {
		query, trunc := stripTrailingEllipsis(item.Text)
		if trunc && query != "" {
			if matched, _ := idx.matchOCR(query); len(matched) > 0 {
				names = append(names, query)
				continue
			}
			truncated = append(truncated, item)
			continue
		}
		if trunc {
			truncated = append(truncated, item)
			continue
		}
		names = append(names, item.Text)
	}
	if err := unlockByNames(ctx, names); err != nil {
		log.Error().Err(err).Str("component", component).Msg("catalog match or persist failed")
		return nil, false
	}

	detailBytes, _ := json.Marshal(map[string][]truncatedItem{"truncated": truncated})
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

func (r *ScanDetailRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil || ctx == nil {
		return nil, false
	}

	rec, err := ctx.RunRecognition(detailRecognitionNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("detail recognition failed")
		return &maa.CustomRecognitionResult{Box: arg.Roi}, true
	}
	// IntelArchiveRecognitionDetailTitleText.all_of：OCR 在第 3 段。
	items := ocrFiltered(rec, 2)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Text)
	}
	if err := unlockByNames(ctx, names); err != nil {
		log.Error().Err(err).Str("component", component).Msg("catalog match or persist failed")
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
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

// stripTrailingEllipsis 去掉标题后缀省略号（含 OCR 成 `..` 的情况）。中间的点不剥。
func stripTrailingEllipsis(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	runes := []rune(s)
	end := len(runes)
	dots := 0
	ellipsis := 0
	for end > 0 {
		switch runes[end-1] {
		case '.', '．', '。':
			end--
			dots++
			continue
		case '…':
			end--
			ellipsis++
			continue
		}
		break
	}
	if ellipsis == 0 && dots < 2 {
		return s, false
	}
	return strings.TrimSpace(string(runes[:end])), true
}

func ocrFiltered(detail *maa.RecognitionDetail, index int) []truncatedItem {
	if detail == nil || !detail.Hit || index < 0 || index >= len(detail.CombinedResult) || detail.CombinedResult[index] == nil {
		return nil
	}
	var payload struct {
		Filtered []struct {
			Box  []int  `json:"box"`
			Text string `json:"text"`
		} `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail.CombinedResult[index].DetailJson), &payload); err != nil {
		log.Error().Err(err).Str("component", component).Msg("ocr detail json parse failed")
		return nil
	}
	items := make([]truncatedItem, 0, len(payload.Filtered))
	for _, item := range payload.Filtered {
		if text := strings.TrimSpace(item.Text); text != "" {
			items = append(items, truncatedItem{Text: text, Box: item.Box})
		}
	}
	return items
}

func unlockByNames(ctx *maa.Context, names []string) error {
	idx, err := loadCatalogIndex()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		matched, full := idx.matchOCR(name)
		if len(matched) == 0 {
			log.Info().Str("component", component).Str("ocr", name).Msg("catalog lookup miss")
			maafocus.Print(ctx, i18n.T("intelarchive.item_not_found", name))
			continue
		}
		log.Info().Str("component", component).Str("ocr", name).Str("full_name", full).Strs("item_id", matched).Msg("catalog lookup hit")
		maafocus.Print(ctx, i18n.T("intelarchive.item_unlocked", full))
		ids = append(ids, matched...)
	}
	_, err = unlockItems(placeholderUID, ids)
	return err
}
