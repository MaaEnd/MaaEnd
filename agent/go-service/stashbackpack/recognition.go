package stashbackpack

import (
	"encoding/json"
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

func buildFinderOverride(item snapshotItem, param nextItemParam) map[string]any {
	override := make(map[string]any, len(param.BagNodes)+len(param.RepoNodes))
	filters := iconrecognition.StorageFilter()
	for _, node := range param.BagNodes {
		override[node] = map[string]any{
			"custom_recognition_param": iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs(item.ItemID),
				iconrecognition.WithItemRecheckFilters(filters.Normal.Any),
				iconrecognition.WithDeduplicate(true),
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
