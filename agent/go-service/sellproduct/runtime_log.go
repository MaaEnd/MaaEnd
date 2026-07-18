package sellproduct

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func printRuntimeLocationEntered(ctx *maa.Context, location string) {
	maafocus.Print(ctx, i18n.T("sellproduct.runtime.location_entered", runtimeLocationName(location)))
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

func printRuntimeOperatorScanFailed(ctx *maa.Context, location string, usage string) {
	maafocus.Print(ctx, runtimeOperatorScanFailedMessage(location, usage))
}

func runtimeOperatorScanFailedMessage(location string, usage string) string {
	if usage == operatorActionUsageAll {
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

func printRuntimeItemSwitched(ctx *maa.Context, location string, itemID string) {
	maafocus.Print(ctx, runtimeItemSwitchedMessage(location, itemID))
}

func runtimeItemSwitchedMessage(location string, itemID string) string {
	return i18n.T(
		"sellproduct.runtime.item_switched",
		runtimeItemName(itemID),
		runtimeLocationName(location),
	)
}

func runtimeUsageName(usage string) string {
	return i18n.T("sellproduct.runtime.usage." + usage)
}

func runtimeOperatorName(candidate operatorCandidate) string {
	return candidate.DisplayName
}

func runtimeLocationName(location string) string {
	data, err := loadSellProductSelectionDataCached()
	if err == nil {
		if entry, ok := data.Locations[location]; ok {
			return localizedSelectionName(entry.Names, location)
		}
	}
	return location
}

func runtimeItemName(itemID string) string {
	data, err := loadSellProductSelectionDataCached()
	if err == nil {
		if entry, ok := data.Items[itemID]; ok {
			return localizedSelectionName(entry.Names, itemID)
		}
	}
	return itemID
}
