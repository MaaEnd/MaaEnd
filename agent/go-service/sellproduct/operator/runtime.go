package operator

import (
	"fmt"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/selectiondata"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// LocationPlan 表示据点运行时计划中的干员部分。
type LocationPlan struct {
	LocationName    string
	TargetOperator  string
	RestoreOperator string
}

// BuildLocationPlan 构建当前据点计划中的干员部分。
func BuildLocationPlan(location string) (LocationPlan, error) {
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		return LocationPlan{}, fmt.Errorf("load operator ownership: %w", err)
	}

	targetSelection, err := resolveOperatorSelectionParam(&operatorRecognitionParam{
		Usage:    operatorUsageTarget,
		Location: location,
	})
	if err != nil {
		return LocationPlan{}, fmt.Errorf("resolve target operator: %w", err)
	}
	targetCandidates := candidatesForOwnership(targetSelection, ownership)

	restoreSelection, err := resolveOperatorSelectionParam(&operatorRecognitionParam{
		Usage:    operatorUsageRestore,
		Location: location,
	})
	if err != nil {
		return LocationPlan{}, fmt.Errorf("resolve restore operator: %w", err)
	}
	if len(targetCandidates) > 0 {
		restoreSelection.TargetAssignments[location] = targetCandidates[0]
	}
	restoreCandidates := candidatesForOwnership(restoreSelection, ownership)

	plan := LocationPlan{
		LocationName: selectiondata.LocationName(location),
	}
	if len(targetCandidates) > 0 {
		plan.TargetOperator = selectiondata.OperatorName(targetCandidates[0].Name)
	}
	if len(restoreCandidates) > 0 {
		plan.RestoreOperator = selectiondata.OperatorName(restoreCandidates[0].Name)
	}
	return plan, nil
}

func printRuntimeOperatorAssignment(
	ctx *maa.Context,
	location string,
	usage string,
	candidate operatorCandidate,
	changed bool,
) {
	maafocus.Print(ctx, runtimeOperatorAssignmentMessage(location, usage, candidate, changed))
}

func runtimeOperatorAssignmentMessage(
	location string,
	usage string,
	candidate operatorCandidate,
	changed bool,
) string {
	key := "sellproduct.runtime.operator_kept"
	if changed {
		key = "sellproduct.runtime.operator_switched"
	}
	return i18n.T(
		key,
		runtimeUsageName(usage),
		selectiondata.OperatorName(candidate.Name),
		selectiondata.LocationName(location),
	)
}

func printRuntimeOperatorConflict(
	ctx *maa.Context,
	location string,
	usage string,
	candidate operatorCandidate,
) {
	maafocus.Print(ctx, i18n.T(
		"sellproduct.runtime.operator_conflict",
		selectiondata.OperatorName(candidate.Name),
		runtimeUsageName(usage),
		selectiondata.LocationName(location),
	))
}

func printRuntimeOperatorReplanned(
	ctx *maa.Context,
	location string,
	usage string,
	candidate operatorCandidate,
) {
	maafocus.Print(ctx, runtimeOperatorReplannedMessage(location, usage, candidate))
}

func runtimeOperatorReplannedMessage(location string, usage string, candidate operatorCandidate) string {
	return i18n.T(
		"sellproduct.runtime.operator_replanned",
		runtimeUsageName(usage),
		selectiondata.LocationName(location),
		selectiondata.OperatorName(candidate.Name),
	)
}

func printRuntimeOperatorUnavailable(ctx *maa.Context, location string, usage string) {
	maafocus.Print(ctx, i18n.T(
		"sellproduct.runtime.operator_unavailable",
		selectiondata.LocationName(location),
		runtimeUsageName(usage),
	))
}

func printRuntimeOperatorCacheStatus(ctx *maa.Context, status operatorCacheStatus) {
	maafocus.Print(ctx, runtimeOperatorCacheStatusMessage(status))
}

func printRuntimeOperatorCacheRescan(ctx *maa.Context, candidate operatorCandidate) {
	maafocus.Print(ctx, i18n.T(
		"sellproduct.runtime.operator_cache_rescan",
		selectiondata.OperatorName(candidate.Name),
	))
}

func runtimeOperatorCacheStatusMessage(status operatorCacheStatus) string {
	if !status.Ready {
		return i18n.T("sellproduct.runtime.operator_cache_scanning")
	}
	return i18n.T(
		"sellproduct.runtime.operator_cache_loaded",
		runtimeLocalCacheUpdatedAt(status.UpdatedAt),
	)
}

func runtimeLocalCacheUpdatedAt(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return i18n.T("sellproduct.runtime.operator_cache_time_unknown")
	}
	return updatedAt.Local().Format("2006-01-02 15:04:05")
}

func printRuntimeOperatorScanFailed(ctx *maa.Context, location string, usage string) {
	maafocus.Print(ctx, runtimeOperatorScanFailedMessage(location, usage))
}

func runtimeOperatorScanFailedMessage(location string, usage string) string {
	if usage == operatorUsageAll {
		return i18n.T("sellproduct.runtime.operator_cache_scan_failed")
	}
	return i18n.T(
		"sellproduct.runtime.operator_scan_failed",
		selectiondata.LocationName(location),
		runtimeUsageName(usage),
	)
}

func printRuntimeRestoreSkipped(ctx *maa.Context, location string) {
	maafocus.Print(ctx, i18n.T("sellproduct.runtime.restore_skipped", selectiondata.LocationName(location)))
}

func runtimeUsageName(usage string) string {
	return i18n.T("sellproduct.runtime.usage." + usage)
}
