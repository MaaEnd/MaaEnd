package stashbackpack

import (
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/autoalt"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	operationReset             = "reset"
	operationBeginSnapshot     = "begin_snapshot"
	operationAppendSnapshot    = "append_snapshot_page"
	operationCompleteFull      = "complete_full"
	operationPrepareSnapshot   = "prepare_snapshot"
	operationPrepareDifference = "prepare_difference"
	operationPrepareRestore    = "prepare_restore"
	operationAdvanceRestore    = "advance_restore_page"
	operationConsumeTarget     = "consume_target"
	operationSetDepot          = "set_depot"

	verifySourceItemNode = "StashBackpackVerifySourceItem"
)

// 720p 物品格点击区域向中心收缩，避免点击到格子边缘。
var shiftClickTargetOffset = maa.Rect{26, 25, -52, -50}

type stateActionParam struct {
	Operation       string   `json:"operation"`
	Snapshot        string   `json:"snapshot,omitempty"`
	MinuendSnapshot string   `json:"minuend_snapshot,omitempty"`
	Subtrahend      string   `json:"subtrahend_snapshot,omitempty"`
	Categories      []string `json:"categories,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Depot           string   `json:"depot,omitempty"`
}

// StateAction manages ordered backpack snapshots and item queues for StashBackpack Pipeline nodes.
type StateAction struct{}

var _ maa.CustomActionRunner = &StateAction{}

// Run applies one state operation. UI navigation and item movement remain in Pipeline.
func (a *StateAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
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
		globalState.reset()
	case operationBeginSnapshot:
		err = globalState.beginSnapshot(param.Snapshot)
	case operationAppendSnapshot:
		err = appendRecognitionPage(arg, param)
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
	case operationAdvanceRestore:
		err = globalState.advanceRestorePage()
	case operationConsumeTarget:
		var item snapshotItem
		var ok bool
		item, ok = globalState.consumeTarget()
		if !ok {
			err = fmt.Errorf("no current target")
		}
		if err == nil {
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

// ShiftClickAction records the source cell for local verification, then transfers the item with Shift+Click.
type ShiftClickAction struct{}

var _ maa.CustomActionRunner = &ShiftClickAction{}

// Run patches the source-cell verifier before clicking the center of the recognized item cell.
func (a *ShiftClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("shift click action received nil context or arg")
		return false
	}
	if arg.Box[2] <= 0 || arg.Box[3] <= 0 {
		log.Error().Str("component", componentName).Interface("box", arg.Box).Msg("shift click source box is invalid")
		return false
	}
	if err := ctx.OverridePipeline(map[string]any{
		verifySourceItemNode: map[string]any{
			"roi": []int{arg.Box[0], arg.Box[1], arg.Box[2], arg.Box[3]},
		},
	}); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to configure source item verifier")
		return false
	}

	clickArg := *arg
	clickArg.Box = maa.Rect{
		arg.Box[0] + shiftClickTargetOffset[0],
		arg.Box[1] + shiftClickTargetOffset[1],
		arg.Box[2] + shiftClickTargetOffset[2],
		arg.Box[3] + shiftClickTargetOffset[3],
	}
	if clickArg.Box[2] <= 0 || clickArg.Box[3] <= 0 {
		log.Error().Str("component", componentName).Interface("box", clickArg.Box).Msg("shift click target box is invalid")
		return false
	}
	return (&autoalt.AutoShiftClickAction{}).Run(ctx, &clickArg)
}

func appendRecognitionPage(arg *maa.CustomActionArg, param stateActionParam) error {
	parsed, _, err := iconrecognition.ParseRecognitionDetail(arg.RecognitionDetail)
	if err != nil {
		return fmt.Errorf("parse snapshot page: %w", err)
	}
	positioned := make([]snapshotItemWithPosition, 0, len(parsed.Matches))
	for _, match := range parsed.Matches {
		row := match.CellBox.Y()
		column := match.CellBox.X()
		if match.Row != nil {
			row = *match.Row
		}
		if match.Column != nil {
			column = *match.Column
		}
		positioned = append(positioned, snapshotItemWithPosition{
			ItemID: match.ItemID, CategoryType: match.CategoryType, Row: row, Column: column, CellBox: match.CellBox,
		})
	}
	total, err := globalState.appendSnapshotPage(param.Snapshot, positioned)
	if err != nil {
		return err
	}
	log.Info().Str("component", componentName).Str("snapshot", param.Snapshot).
		Int("page_count", len(positioned)).Int("snapshot_count", total).Msg("appended backpack snapshot page")
	return nil
}
