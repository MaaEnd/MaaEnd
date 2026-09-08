package stashbackpack

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	operationReset             = "reset"
	operationCopySnapshot      = "copy_snapshot"
	operationCompleteFull      = "complete_full"
	operationPrepareSnapshot   = "prepare_snapshot"
	operationPrepareDifference = "prepare_difference"
	operationPrepareRestore    = "prepare_restore"
	operationAdvanceBagPage    = "advance_bag_page"
	operationAdvanceRestore    = "advance_restore_page"
	operationMarkBagClicked    = "mark_bag_item_clicked"
	operationDiscardBagTargets = "discard_bag_targets"
	operationConsumeTarget     = "consume_target"
	operationSetDepot          = "set_depot"
)

type stateActionParam struct {
	Operation       string   `json:"operation"`
	Snapshot        string   `json:"snapshot,omitempty"`
	SourceSnapshot  string   `json:"source_snapshot,omitempty"`
	MinuendSnapshot string   `json:"minuend_snapshot,omitempty"`
	Subtrahend      string   `json:"subtrahend_snapshot,omitempty"`
	Categories      []string `json:"categories,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	ChangesSnapshot bool     `json:"changes_snapshot,omitempty"`
	Depot           string   `json:"depot,omitempty"`
}

// StateAction manages ordered backpack snapshots and item queues for StashBackpack Pipeline nodes.
type StateAction struct{}

var _ maa.CustomActionRunner = &StateAction{}

// Run applies one state operation. UI navigation and item movement remain in Pipeline.
func (a *StateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", componentName).Msg("state action received nil arg")
		return false
	}
	var param stateActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse state action params")
		return false
	}
	var err error
	switch param.Operation {
	case operationReset:
		if ctx == nil {
			err = fmt.Errorf("context is nil")
			break
		}
		quickNode, nodeErr := ctx.GetNode("StashBackpackStashQuick")
		if nodeErr != nil {
			err = nodeErr
			break
		}
		usableNode, nodeErr := ctx.GetNode("StashBackpackManualCategoryUsable")
		if nodeErr != nil {
			err = nodeErr
			break
		}
		manualNode, nodeErr := ctx.GetNode("StashBackpackManualSubTask")
		if nodeErr != nil {
			err = nodeErr
			break
		}
		// 总开关关闭时，隐藏分类中保留的勾选不应阻止补充；互斥以最终配置为准。
		if (manualNode.Enabled == nil || *manualNode.Enabled) &&
			(usableNode.Enabled == nil || *usableNode.Enabled) {
			err = ctx.OverridePipeline(map[string]any{
				"StashBackpackPrepareReplenishTargets": map[string]any{"enabled": false},
			})
			if err != nil {
				break
			}
		}
		globalState.resetForStash(quickNode.Enabled == nil || *quickNode.Enabled)
	case operationCopySnapshot:
		err = globalState.copySnapshot(param.SourceSnapshot, param.Snapshot)
	case operationCompleteFull:
		err = globalState.completeFull()
	case operationPrepareSnapshot:
		var targets []snapshotItem
		targets, err = globalState.prepareSnapshotTargets(param.Snapshot, param.Categories)
		if err == nil {
			log.Info().Str("component", componentName).Str("snapshot", param.Snapshot).
				Int("target_count", len(targets)).Msg("prepared snapshot item targets")
		}
	case operationPrepareDifference:
		var targets []snapshotItem
		targets, err = globalState.prepareDifferenceTargets(param.MinuendSnapshot, param.Subtrahend, param.Categories)
		if err == nil {
			log.Info().Str("component", componentName).
				Str("minuend_snapshot", param.MinuendSnapshot).Str("subtrahend_snapshot", param.Subtrahend).
				Int("target_count", len(targets)).Msg("prepared snapshot difference targets")
		}
	case operationPrepareRestore:
		err = globalState.prepareRestore(param.Snapshot)
	case operationAdvanceBagPage:
		err = globalState.advanceBagPage()
	case operationAdvanceRestore:
		err = globalState.advanceRestorePage()
	case operationMarkBagClicked:
		var item snapshotItem
		var ok bool
		item, ok = globalState.markSelectedBagTargetClicked(param.Reason, param.ChangesSnapshot)
		if !ok {
			err = fmt.Errorf("no selected bag target")
		}
		if err == nil {
			log.Info().Str("component", componentName).Str("item_id", item.ItemID).
				Str("category_type", item.CategoryType).Msg("queued clicked backpack item for page-level verification")
		}
	case operationDiscardBagTargets:
		discarded := globalState.discardRemainingBagTargets()
		for _, item := range discarded {
			event := log.Warn().Str("component", componentName).
				Str("item_id", item.ItemID).Str("category_type", item.CategoryType)
			if param.Reason != "" {
				event = event.Str("reason", param.Reason)
			}
			event.Msg("discarded backpack target after reaching the bottom")
		}
	case operationConsumeTarget:
		var item snapshotItem
		var ok bool
		item, ok = globalState.consumeTarget()
		if !ok {
			err = fmt.Errorf("no current target")
		}
		if err == nil {
			if param.ChangesSnapshot {
				globalState.markSnapshotChanged()
			}
			event := log.Info().Str("component", componentName).
				Str("item_id", item.ItemID).Str("category_type", item.CategoryType)
			if param.Reason != "" {
				event = event.Str("reason", param.Reason)
			}
			if param.Reason == "replenish_repo_not_found" {
				event.Msg("item was not found in Depot and cannot be replenished")
			} else {
				event.Msg("consumed current item target")
			}
		}
	case operationSetDepot:
		err = globalState.setDepot(param.Depot)
	default:
		err = fmt.Errorf("unsupported operation %q", param.Operation)
	}
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("operation", param.Operation).
			Msg("state action failed")
		return false
	}
	return true
}
