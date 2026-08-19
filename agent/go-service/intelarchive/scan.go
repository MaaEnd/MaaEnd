package intelarchive

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	itemTextNode          = "IntelArchiveRecognitionItemText"
	itemSecretNode        = "IntelArchiveRecognitionItemSecretWithoutPageCount"
	itemOCRIndex          = 4
	itemSecretBoxIndex    = 4
	detailRecognitionNode = "IntelArchiveRecognitionDetailTitleText"
	truncatedItemNode     = "IntelArchiveResolveTrunc"
	secretUnlockID        = "nar_digital_map02_13003_1"
	secretUnlockName      = "文明保护协定"
)

type scanItemsParam struct {
	FileCategory string `json:"file_category"`
}

var listFileCategory string

func parseScanFileCategory(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var params scanItemsParam
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		log.Error().Err(err).Str("component", component).Str("custom_recognition_param", raw).Msg("failed to parse file_category")
		return ""
	}
	return strings.TrimSpace(params.FileCategory)
}

var _ maa.CustomRecognitionRunner = &ScanItemsRecognition{}
var _ maa.CustomRecognitionRunner = &ScanDetailRecognition{}
var _ maa.CustomActionRunner = &ResolveTruncAction{}

type truncatedItem struct {
	Text string `json:"text"`
	Box  []int  `json:"box"` // [x, y, w, h]
}

// ScanItemsRecognition scans the current list screen: named cards are matched against the catalog,
// multi-page / truncated cards are returned for ResolveTruncAction to open.
// Secret unnamed cards force-unlock a hardcoded redacted page ID.
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
			return false
		}
	}
	return true
}

func (r *ScanItemsRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil || ctx == nil {
		return nil, false
	}

	titles := recognizeFiltered(ctx, arg, itemTextNode, itemOCRIndex)
	secret := recognizeFiltered(ctx, arg, itemSecretNode, itemSecretBoxIndex)
	fileCategory := parseScanFileCategory(arg.CustomRecognitionParam)
	listFileCategory = fileCategory

	idx, err := loadCatalogIndex()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load catalog")
		return nil, false
	}

	truncated := make([]truncatedItem, 0, len(secret)+len(titles))
	if len(secret) > 0 {
		if fileCategory == "" || fileCategory == "digital" {
			if err := unlockKnown(ctx, []string{secretUnlockID}, map[string]string{secretUnlockID: secretUnlockName}); err != nil {
				log.Error().Err(err).Str("component", component).Msg("secret force unlock failed")
				return nil, false
			}
		} else {
			truncated = append(truncated, secret...)
		}
	}
	names := make([]string, 0, len(titles))
	for _, item := range titles {
		query, trunc := stripTrailingEllipsis(item.Text)
		lookup := query
		if lookup == "" {
			lookup = item.Text
		}
		if idx.shouldOpenFromList(lookup, fileCategory) {
			truncated = append(truncated, item)
			continue
		}
		if trunc && query != "" {
			if matched, _ := idx.matchOCR(query, fileCategory); len(matched) > 0 {
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
	if err := unlockByNames(ctx, names, fileCategory); err != nil {
		log.Error().Err(err).Str("component", component).Msg("catalog match or persist failed")
		return nil, false
	}

	detailBytes, _ := json.Marshal(map[string][]truncatedItem{"truncated": truncated})
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailBytes)}, true
}

func recognizeFiltered(ctx *maa.Context, arg *maa.CustomRecognitionArg, node string, index int) []truncatedItem {
	rec, err := ctx.RunRecognition(node, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("node", node).Msg("item recognition failed")
		return nil
	}
	return filteredItems(rec, index)
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
	items := filteredItems(rec, 2)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Text)
	}
	if err := unlockByNames(ctx, names, listFileCategory); err != nil {
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

// stripTrailingEllipsis 去掉标题后缀省略号（含 OCR 成 `.` / `..` / `…` 的情况）。中间的点不剥。
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
	if ellipsis == 0 && dots == 0 {
		return s, false
	}
	return strings.TrimSpace(string(runes[:end])), true
}

func filteredItems(detail *maa.RecognitionDetail, index int) []truncatedItem {
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
		log.Error().Err(err).Str("component", component).Msg("filtered detail json parse failed")
		return nil
	}
	items := make([]truncatedItem, 0, len(payload.Filtered))
	for _, item := range payload.Filtered {
		text := strings.TrimSpace(item.Text)
		if text == "" && (len(item.Box) != 4 || item.Box[2] <= 0 || item.Box[3] <= 0) {
			continue
		}
		items = append(items, truncatedItem{Text: text, Box: item.Box})
	}
	return items
}

func unlockByNames(ctx *maa.Context, names []string, fileCategory string) error {
	idx, err := loadCatalogIndex()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(names))
	idToName := make(map[string]string)
	for _, name := range names {
		if name == "" {
			continue
		}
		matched, full := idx.matchOCR(name, fileCategory)
		if len(matched) == 0 {
			log.Info().Str("component", component).Str("ocr", name).Str("file_category", fileCategory).Msg("catalog lookup miss")
			maafocus.Print(ctx, i18n.T("intelarchive.item_not_found", name))
			continue
		}
		log.Info().Str("component", component).Str("ocr", name).Str("full_name", full).Str("file_category", fileCategory).Strs("item_id", matched).Msg("catalog lookup hit")
		for _, id := range matched {
			ids = append(ids, id)
			if idToName[id] == "" {
				idToName[id] = full
			}
		}
	}
	return unlockKnown(ctx, ids, idToName)
}

func unlockKnown(ctx *maa.Context, ids []string, idToName map[string]string) error {
	uid, err := accountUID(ctx)
	if err != nil {
		return err
	}
	added, err := unlockItems(uid, ids)
	if err != nil {
		return err
	}
	for _, id := range added {
		name := idToName[id]
		if name == "" {
			name = id
		}
		maafocus.Print(ctx, i18n.T("intelarchive.item_unlocked", name))
	}
	return nil
}

func accountUID(ctx *maa.Context) (string, error) {
	if ctx == nil || ctx.GetTasker() == nil {
		return "", fmt.Errorf("nil context or tasker")
	}
	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		return "", fmt.Errorf("nil controller")
	}
	uid, err := captureuid.Capture(ctx, ctrl, true, true, true, captureuid.OutputTypeMasked)
	if err != nil {
		return "", fmt.Errorf("capture uid: %w", err)
	}
	if uid == "" {
		return "", fmt.Errorf("captured uid is empty")
	}
	return uid, nil
}
