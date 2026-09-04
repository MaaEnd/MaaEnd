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
		if parsed.Error.Code != iconrecognition.ErrorCodeNoMatch &&
			!(parsed.Error.Code == iconrecognition.ErrorCodeGridDetectionFailed &&
				parsed.Error.Message == emptyGridDetectionErrorMessage) {
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

	confirmed, failed := globalState.updateBagPageMatches(matches)
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
			Msg("backpack item count did not decrease after Shift+Click; queued the item again")
	}
	log.Info().Str("component", componentName).Int("item_id_count", len(itemIDs)).
		Int("match_count", len(matches)).Int("confirmed_count", len(confirmed)).Int("retry_count", len(failed)).
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

// RetrievedItemRecognition verifies that the current page contains one more target item than its in-memory baseline.
type RetrievedItemRecognition struct{}

var _ maa.CustomRecognitionRunner = &RetrievedItemRecognition{}

// Run scans only the current item ID and its exact category, then updates the current page count after an increase.
func (r *RetrievedItemRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", componentName).Msg("retrieved item recognition received nil context, arg, or image")
		return nil, false
	}
	target, ok := globalState.currentTarget()
	if !ok {
		return nil, false
	}
	filter := iconrecognition.ItemFilter("Normal:" + target.CategoryType)
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:               maa.NewTargetRect(arg.Roi),
			CustomRecognition: iconrecognition.CustomRecognitionName,
			CustomRecognitionParam: iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs(target.ItemID),
				iconrecognition.WithItemFilters(filter),
				iconrecognition.WithItemRecheckFilters(filter),
				iconrecognition.WithDeduplicate(false),
				iconrecognition.WithDebug(true),
			),
		},
		arg.Img,
	)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", target.ItemID).
			Msg("failed to scan backpack for retrieved item")
		return nil, false
	}
	parsed, rawDetail, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", target.ItemID).
			Msg("failed to parse retrieved item recognition")
		return nil, false
	}
	baselineCount, matched, err := globalState.recordRetrievedItemCount(target.ItemID, len(parsed.Matches))
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("item_id", target.ItemID).
			Msg("failed to compare retrieved item with restore baseline")
		return nil, false
	}
	if !matched {
		return nil, false
	}
	log.Info().Str("component", componentName).Str("item_id", target.ItemID).
		Int("baseline_count", baselineCount).Int("current_count", len(parsed.Matches)).
		Msg("retrieved item count increased on current backpack page")
	return &maa.CustomRecognitionResult{Box: parsed.Matches[0].CellBox, Detail: rawDetail}, true
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

// PlatformSupportedRecognition matches when modifier-click storage is supported by the current controller.
type PlatformSupportedRecognition struct{}

var _ maa.CustomRecognitionRunner = &PlatformSupportedRecognition{}

// Run currently limits the workflow to the Win32 controller implementation.
func (r *PlatformSupportedRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !isSupportedControllerType(pienv.ControllerType()) {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func isSupportedControllerType(controllerType string) bool {
	return strings.EqualFold(strings.TrimSpace(controllerType), "Win32")
}

// SnapshotChangedRecognition matches after the current physical snapshot has been changed by a successful item move.
type SnapshotChangedRecognition struct{}

var _ maa.CustomRecognitionRunner = &SnapshotChangedRecognition{}

// Run lets Pipeline skip a second full scan when no new item was stored.
func (r *SnapshotChangedRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || !globalState.snapshotChanged() {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}
