package operator

import (
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
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
		LocationName: runtimeLocationName(location),
	}
	if len(targetCandidates) > 0 {
		plan.TargetOperator = runtimeOperatorName(targetCandidates[0])
	}
	if len(restoreCandidates) > 0 {
		plan.RestoreOperator = runtimeOperatorName(restoreCandidates[0])
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
		runtimeOperatorName(candidate),
		runtimeLocationName(location),
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
		runtimeOperatorName(candidate),
		runtimeUsageName(usage),
		runtimeLocationName(location),
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
		runtimeLocationName(location),
		runtimeOperatorName(candidate),
	)
}

func printRuntimeOperatorUnavailable(ctx *maa.Context, location string, usage string) {
	maafocus.Print(ctx, i18n.T(
		"sellproduct.runtime.operator_unavailable",
		runtimeLocationName(location),
		runtimeUsageName(usage),
	))
}

func printRuntimeOperatorCacheStatus(ctx *maa.Context, status operatorCacheStatus) {
	maafocus.Print(ctx, runtimeOperatorCacheStatusMessage(status))
}

func printRuntimeOperatorCacheRescan(ctx *maa.Context, candidate operatorCandidate) {
	maafocus.Print(ctx, i18n.T("sellproduct.runtime.operator_cache_rescan", runtimeOperatorName(candidate)))
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
		runtimeLocationName(location),
		runtimeUsageName(usage),
	)
}

func printRuntimeRestoreSkipped(ctx *maa.Context, location string) {
	maafocus.Print(ctx, i18n.T("sellproduct.runtime.restore_skipped", runtimeLocationName(location)))
}

func runtimeUsageName(usage string) string {
	return i18n.T("sellproduct.runtime.usage." + usage)
}

func runtimeOperatorName(candidate operatorCandidate) string {
	return candidate.DisplayName
}

func runtimeLocationName(location string) string {
	data, err := loadOperatorSelectionDataFunc()
	if err == nil {
		if name := strings.TrimSpace(data.LocationNames[location]); name != "" {
			return name
		}
	}
	return location
}
