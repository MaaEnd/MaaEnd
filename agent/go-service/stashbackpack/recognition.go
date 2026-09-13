package stashbackpack

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type nextItemParam struct {
	BagNodes  []string `json:"bag_nodes,omitempty"`
	RepoNodes []string `json:"repo_nodes,omitempty"`
}

type categoryParam struct {
	Category string `json:"category"`
}

type depotParam struct {
	Depot string `json:"depot"`
}

// NextItemRecognition exposes the queue head and injects its item filters into finder nodes.
type NextItemRecognition struct{}

var _ maa.CustomRecognitionRunner = &NextItemRecognition{}

// Run returns no match when the queue is exhausted; otherwise it configures the current item finders.
func (r *NextItemRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("next item recognition received nil context or arg")
		return nil, false
	}
	var param nextItemParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse next item params")
		return nil, false
	}
	item, ok := globalState.currentTarget()
	if !ok {
		return nil, false
	}
	if err := ctx.OverridePipeline(buildFinderOverride(item, param)); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", item.ItemID).
			Msg("failed to configure current item finders")
		return nil, false
	}
	detail, err := json.Marshal(item)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to serialize current item")
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detail)}, true
}

// BagPageRecognition 一次识别当前页全部剩余目标，并按网格顺序逐个返回缓存结果。
type BagPageRecognition struct{}

var _ maa.CustomRecognitionRunner = &BagPageRecognition{}

func (r *BagPageRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", componentName).Msg("bag page recognition received nil context, arg, or image")
		return nil, false
	}
	if match, ok := globalState.nextBagPageMatch(); ok {
		return bagPageRecognitionResult(match)
	}

	itemIDs := globalState.bagRecognitionItemIDs()
	if len(itemIDs) == 0 {
		return nil, false
	}
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:               maa.NewTargetRect(arg.Roi),
			CustomRecognition: iconrecognition.CustomRecognitionName,
			CustomRecognitionParam: iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs(itemIDs...),
				iconrecognition.WithItemRecheckFilters(iconrecognition.ItemFilter("Normal:*")),
				iconrecognition.WithDeduplicate(false),
				iconrecognition.WithDebug(true),
			),
		},
		arg.Img,
	)
	if err != nil {
		globalState.markBagPageRecognitionFailed()
		log.Error().Err(err).Str("component", componentName).Int("item_id_count", len(itemIDs)).
			Msg("failed to recognize remaining backpack targets on current page")
		return nil, false
	}
	parsed, _, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		globalState.markBagPageRecognitionFailed()
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse backpack page recognition")
		return nil, false
	}

	matches := make([]bagPageMatch, 0, len(parsed.Matches))
	if parsed.Error != nil {
		if parsed.Error.Code != iconrecognition.ErrorCodeNoMatch {
			globalState.markBagPageRecognitionFailed()
			log.Error().Str("component", componentName).Str("error_code", string(parsed.Error.Code)).
				Str("error_message", parsed.Error.Message).Msg("backpack page recognition failed")
			return nil, false
		}
	} else {
		for _, item := range parsed.Matches {
			row := item.CellBox.Y()
			column := item.CellBox.X()
			if item.Row != nil {
				row = *item.Row
			}
			if item.Column != nil {
				column = *item.Column
			}
			matches = append(matches, bagPageMatch{
				ItemID:       item.ItemID,
				CategoryType: item.CategoryType,
				Row:          row,
				Column:       column,
				CellBox:      item.CellBox,
			})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Row != matches[j].Row {
			return matches[i].Row < matches[j].Row
		}
		return matches[i].Column < matches[j].Column
	})

	confirmed, failed, skipped := globalState.updateBagPageMatches(matches)
	for _, clicked := range confirmed {
		event := log.Info().Str("component", componentName).
			Str("item_id", clicked.Item.ItemID).Str("category_type", clicked.Item.CategoryType)
		if clicked.Reason != "" {
			event = event.Str("reason", clicked.Reason)
		}
		event.Msg("verified stored backpack item by current-page count")
	}
	for _, clicked := range failed {
		log.Warn().Str("component", componentName).Str("item_id", clicked.Item.ItemID).
			Str("category_type", clicked.Item.CategoryType).
			Int("attempt_count", clicked.Attempts).Int("max_attempts", bagStoreMaxAttempts).
			Msg("backpack item count did not decrease after Shift+Click; queued the item for retry")
	}
	for _, clicked := range skipped {
		log.Warn().Str("component", componentName).Str("item_id", clicked.Item.ItemID).
			Str("category_type", clicked.Item.CategoryType).
			Int("row", clicked.Item.Row).Int("column", clicked.Item.Column).
			Int("attempt_count", clicked.Attempts).Int("max_attempts", bagStoreMaxAttempts).
			Msg("backpack item count did not decrease after Shift+Click; attempt limit reached, skipped target")
	}
	log.Info().Str("component", componentName).Int("item_id_count", len(itemIDs)).
		Int("match_count", len(matches)).Int("confirmed_count", len(confirmed)).Int("retry_count", len(failed)).
		Int("skipped_count", len(skipped)).
		Msg("recognized remaining backpack targets on current page")

	match, ok := globalState.nextBagPageMatch()
	if !ok {
		return nil, false
	}
	return bagPageRecognitionResult(match)
}

func bagPageRecognitionResult(match bagPageMatch) (*maa.CustomRecognitionResult, bool) {
	detail, err := json.Marshal(match)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", match.ItemID).
			Msg("failed to serialize backpack page match")
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: match.CellBox, Detail: string(detail)}, true
}

// BagTargetsExhaustedRecognition 仅在页缓存、待验证点击和目标队列均为空时命中。
type BagTargetsExhaustedRecognition struct{}

var _ maa.CustomRecognitionRunner = &BagTargetsExhaustedRecognition{}

func (r *BagTargetsExhaustedRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !globalState.bagTargetsExhausted() {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// BagPageFailedRecognition 将页级识别的真实错误导向 Pipeline 失败节点。
type BagPageFailedRecognition struct{}

var _ maa.CustomRecognitionRunner = &BagPageFailedRecognition{}

func (r *BagPageFailedRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !globalState.bagPageRecognitionFailed() {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func buildFinderOverride(item snapshotItem, param nextItemParam) map[string]any {
	override := make(map[string]any, len(param.BagNodes)+len(param.RepoNodes))
	for _, node := range param.BagNodes {
		override[node] = map[string]any{
			"custom_recognition_param": iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs(item.ItemID),
				iconrecognition.WithItemRecheckFilters(iconrecognition.ItemFilter("Normal:*")),
				iconrecognition.WithDeduplicate(true),
				iconrecognition.WithDebug(true),
			),
		}
	}
	repoFilter := iconrecognition.ItemFilter("Normal:" + item.CategoryType)
	for _, node := range param.RepoNodes {
		override[node] = map[string]any{
			"custom_recognition_param": iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs(item.ItemID),
				iconrecognition.WithItemRecheckFilters(repoFilter),
				iconrecognition.WithDeduplicate(true),
				iconrecognition.WithDebug(true),
			),
		}
	}
	return override
}

// DepotRecognition matches the Depot selected by the preceding StashBackpack task.
type DepotRecognition struct{}

var _ maa.CustomRecognitionRunner = &DepotRecognition{}

// Run lets later tasks reuse the batch-scoped Depot without exposing another user option.
func (r *DepotRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		return nil, false
	}
	var param depotParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse depot params")
		return nil, false
	}
	if !globalState.depotIs(param.Depot) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// NothingStoredRecognition matches when no stored record is waiting for retrieval.
type NothingStoredRecognition struct{}

var _ maa.CustomRecognitionRunner = &NothingStoredRecognition{}

// Run lets the retrieval task finish immediately when nothing was manually stored.
func (r *NothingStoredRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || globalState.storedCount() != 0 {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// HasStoredRecognition matches when at least one stored record waits for retrieval;
// it lets retrieval bypass the complete-snapshot gate.
type HasStoredRecognition struct{}

var _ maa.CustomRecognitionRunner = &HasStoredRecognition{}

func (r *HasStoredRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || globalState.storedCount() == 0 {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// RepoItemCountRecognition records how many cells of the current item exist on the
// current Depot page before its stack is transferred back to the backpack.
type RepoItemCountRecognition struct{}

var _ maa.CustomRecognitionRunner = &RepoItemCountRecognition{}

func (r *RepoItemCountRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	count, ok := scanRepoItemCells(ctx, arg)
	if !ok {
		return nil, false
	}
	item, _ := globalState.currentTarget()
	globalState.noteRepoItemCount(storedItem{ItemID: item.ItemID, CategoryType: item.CategoryType}, count)
	log.Info().Str("component", componentName).Str("item_id", item.ItemID).
		Int("baseline_count", count).Msg("recorded depot cell count before retrieval transfer")
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// RepoItemMovedRecognition judges the transfer by requiring the current Depot page
// to hold one fewer cell of the item than before the transfer; this stays correct
// when several stacks of the same item exist.
type RepoItemMovedRecognition struct{}

var _ maa.CustomRecognitionRunner = &RepoItemMovedRecognition{}

func (r *RepoItemMovedRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	count, ok := scanRepoItemCells(ctx, arg)
	if !ok {
		return nil, false
	}
	item, _ := globalState.currentTarget()
	moved, baseline := globalState.repoItemMoved(storedItem{ItemID: item.ItemID, CategoryType: item.CategoryType}, count)
	log.Info().Str("component", componentName).Str("item_id", item.ItemID).
		Int("baseline_count", baseline).Int("current_count", count).Bool("moved", moved).
		Msg("compared depot cell count after retrieval transfer")
	if !moved {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// scanRepoItemCells counts the current item's cells on the Depot page image.
func scanRepoItemCells(ctx *maa.Context, arg *maa.CustomRecognitionArg) (int, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", componentName).Msg("repo cell scan received nil context, arg, or image")
		return 0, false
	}
	item, ok := globalState.currentTarget()
	if !ok {
		return 0, false
	}
	filter := iconrecognition.ItemFilter("Normal:" + item.CategoryType)
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:               maa.NewTargetRect(arg.Roi),
			CustomRecognition: iconrecognition.CustomRecognitionName,
			CustomRecognitionParam: iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs(item.ItemID),
				iconrecognition.WithItemFilters(filter),
				iconrecognition.WithItemRecheckFilters(filter),
				iconrecognition.WithDeduplicate(false),
				iconrecognition.WithDebug(true),
			),
		},
		arg.Img,
	)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", item.ItemID).
			Msg("failed to scan depot grid for the current item")
		return 0, false
	}
	parsed, _, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", item.ItemID).
			Msg("failed to parse depot grid recognition")
		return 0, false
	}
	if parsed.Error != nil && parsed.Error.Code != iconrecognition.ErrorCodeNoMatch {
		log.Error().Str("component", componentName).Str("item_id", item.ItemID).
			Str("error_code", string(parsed.Error.Code)).Str("error_message", parsed.Error.Message).
			Msg("depot grid recognition returned an error")
		return 0, false
	}
	return len(parsed.Matches), true
}

// TargetCategoryRecognition matches when the current queue item belongs to the requested Depot category.
type TargetCategoryRecognition struct{}

var _ maa.CustomRecognitionRunner = &TargetCategoryRecognition{}

// Run lets Pipeline select a public Depot category node without moving business flow into Go.
func (r *TargetCategoryRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		return nil, false
	}
	var param categoryParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse target category params")
		return nil, false
	}
	item, ok := globalState.currentTarget()
	if !ok || item.CategoryType != param.Category {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// FullCompleteRecognition matches only after a complete S0/S1 full-task snapshot pair is available.
type FullCompleteRecognition struct{}

var _ maa.CustomRecognitionRunner = &FullCompleteRecognition{}

// Run rejects interrupted or partial full-task snapshots.
func (r *FullCompleteRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		return nil, false
	}
	if !globalState.fullComplete() {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

// PlatformSupportedRecognition matches when storage is supported by the current controller.
type PlatformSupportedRecognition struct{}

var _ maa.CustomRecognitionRunner = &PlatformSupportedRecognition{}

// Run keeps embedded stashing on the same supported platforms as the standalone task.
func (r *PlatformSupportedRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !isSupportedControllerType(pienv.ControllerType()) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func isSupportedControllerType(controllerType string) bool {
	switch strings.ToLower(strings.TrimSpace(controllerType)) {
	case "win32", "adb":
		return true
	default:
		return false
	}
}
