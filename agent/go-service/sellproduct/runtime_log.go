package sellproduct

import (
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type runtimeLocationPlanItem struct {
	Name            string
	ReserveQuantity int
}

type runtimeLocationPlan struct {
	LocationName       string
	TargetOperator     string
	RestoreOperator    string
	Items              []runtimeLocationPlanItem
	ExcludedOutOfStock []string
	ExcludedByUser     []string
}

func printRuntimeLocationEntered(ctx *maa.Context, location string) {
	maafocus.Print(ctx, i18n.T("sellproduct.runtime.location_entered", runtimeLocationName(location)))
}

func printRuntimeOnlyPreferredEnabled(ctx *maa.Context) {
	maafocus.Print(ctx, runtimeOnlyPreferredEnabledMessage())
}

func runtimeOnlyPreferredEnabledMessage() string {
	return i18n.T("sellproduct.runtime.only_preferred_enabled")
}

func printRuntimeLocationPlan(ctx *maa.Context, location string) error {
	plan, err := buildRuntimeLocationPlan(location)
	if err != nil {
		return err
	}
	maafocus.Print(ctx, runtimeLocationPlanMessage(plan))
	return nil
}

func buildRuntimeLocationPlan(location string) (runtimeLocationPlan, error) {
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		return runtimeLocationPlan{}, fmt.Errorf("load operator ownership: %w", err)
	}

	targetSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: location,
	})
	if err != nil {
		return runtimeLocationPlan{}, fmt.Errorf("resolve target operator: %w", err)
	}
	targetCandidates := candidatesForOwnership(targetSelection, ownership)

	restoreSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageRestore,
		Location: location,
	})
	if err != nil {
		return runtimeLocationPlan{}, fmt.Errorf("resolve restore operator: %w", err)
	}
	if len(targetCandidates) > 0 {
		restoreSelection.TargetAssignments[location] = targetCandidates[0]
	}
	restoreCandidates := candidatesForOwnership(restoreSelection, ownership)

	groupsByLocation, err := loadItemPriorityGroupsFunc()
	if err != nil {
		return runtimeLocationPlan{}, fmt.Errorf("load item priorities: %w", err)
	}
	policy := priorityPolicySnapshot()
	groups := prioritizeItemGroups(groupsByLocation[location], policy.Preferred, policy.OnlyPreferred)
	reserveRules := reserveRulesSnapshot()
	items, excludedOutOfStock, excludedByUser := buildRuntimeLocationPlanItems(
		groups,
		reserveRules,
		priorityOutOfStockSnapshot(),
	)

	plan := runtimeLocationPlan{
		LocationName:       runtimeLocationName(location),
		Items:              items,
		ExcludedOutOfStock: excludedOutOfStock,
		ExcludedByUser:     excludedByUser,
	}
	if len(targetCandidates) > 0 {
		plan.TargetOperator = runtimeOperatorName(targetCandidates[0])
	}
	if len(restoreCandidates) > 0 {
		plan.RestoreOperator = runtimeOperatorName(restoreCandidates[0])
	}
	return plan, nil
}

func buildRuntimeLocationPlanItems(
	groups []itemPriorityGroup,
	reserveRules map[string]int,
	outOfStock map[string]struct{},
) ([]runtimeLocationPlanItem, []string, []string) {
	items := make([]runtimeLocationPlanItem, 0, len(groups))
	excludedOutOfStock := make([]string, 0, len(outOfStock))
	excludedByUser := make([]string, 0)
	for _, group := range groups {
		if reserveRules[group.ItemID] == reserveBlacklistQuantity {
			excludedByUser = append(excludedByUser, group.DisplayName)
			continue
		}
		if _, unavailable := outOfStock[group.ItemID]; unavailable {
			excludedOutOfStock = append(excludedOutOfStock, group.DisplayName)
			continue
		}
		items = append(items, runtimeLocationPlanItem{
			Name:            group.DisplayName,
			ReserveQuantity: reserveRules[group.ItemID],
		})
	}
	return items, excludedOutOfStock, excludedByUser
}

func runtimeLocationPlanMessage(plan runtimeLocationPlan) string {
	itemNames := make([]string, 0, len(plan.Items))
	reserveDescriptions := make([]string, 0, len(plan.Items))
	for _, item := range plan.Items {
		itemNames = append(itemNames, item.Name)
		if item.ReserveQuantity > 0 {
			reserveDescriptions = append(reserveDescriptions, i18n.T(
				"sellproduct.runtime.plan.reserve_rule",
				item.Name,
				item.ReserveQuantity,
			))
		}
	}

	itemOrder := runtimePlanTextOrNone(strings.Join(itemNames, " → "))
	excludedOutOfStock := runtimePlanTextOrNone(strings.Join(plan.ExcludedOutOfStock, i18n.Separator()))
	excludedByUser := runtimePlanTextOrNone(strings.Join(plan.ExcludedByUser, i18n.Separator()))
	reservePlan := i18n.T("sellproduct.runtime.plan.no_reserve")
	if len(reserveDescriptions) > 0 {
		reservePlan = strings.Join(reserveDescriptions, i18n.Separator())
	}
	return i18n.T(
		"sellproduct.runtime.location_plan",
		runtimePlanTextOrNone(plan.LocationName),
		runtimePlanTextOrNone(plan.TargetOperator),
		runtimePlanTextOrNone(plan.RestoreOperator),
		itemOrder,
		excludedOutOfStock,
		excludedByUser,
		reservePlan,
	)
}

func runtimePlanTextOrNone(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return i18n.T("sellproduct.runtime.plan.none")
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

func printRuntimeItemOutOfStock(ctx *maa.Context, location string, itemID string) {
	maafocus.Print(ctx, runtimeItemOutOfStockMessage(location, itemID))
}

func runtimeItemOutOfStockMessage(location string, itemID string) string {
	return i18n.T(
		"sellproduct.runtime.item_out_of_stock",
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
