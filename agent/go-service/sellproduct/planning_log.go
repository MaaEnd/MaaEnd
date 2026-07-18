package sellproduct

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const planningLogActionName = "SellProductLogPlan"

type planningOperatorEntry struct {
	Location        string `json:"location"`
	LocationName    string `json:"location_name"`
	TargetOperator  string `json:"target_operator"`
	RestoreOperator string `json:"restore_operator"`
}

type planningItemEntry struct {
	Priority int    `json:"priority"`
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
}

type planningLocationItems struct {
	Location     string              `json:"location"`
	LocationName string              `json:"location_name"`
	Items        []planningItemEntry `json:"items"`
}

type planningReserveRule struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type planningLogSnapshot struct {
	Operators      []planningOperatorEntry
	ItemPriorities []planningLocationItems
	ReserveRules   []planningReserveRule
}

// PlanningLogAction 输出正式售卖前使用的干员、货品优先级和保留数量规划。
// 结构化日志供排障，节点通知供 UI 展示；Pipeline 决定调用时机，本动作不接管售卖流程。
type PlanningLogAction struct{}

var _ maa.CustomActionRunner = (*PlanningLogAction)(nil)

func (a *PlanningLogAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	snapshot, err := buildPlanningLogSnapshot()
	if err != nil {
		log.Error().Err(err).Str("component", planningLogActionName).Msg("failed to build sell product plan")
		return true
	}
	if !operatorSessionClaimPlanningLog() {
		return true
	}
	log.Info().
		Str("component", planningLogActionName).
		Interface("operator_plan", snapshot.Operators).
		Interface("item_priorities", snapshot.ItemPriorities).
		Interface("reserve_rules", snapshot.ReserveRules).
		Msg("sell product plan ready")
	printPlanningFocus(ctx, snapshot)
	return true
}

func buildPlanningLogSnapshot() (planningLogSnapshot, error) {
	operatorData, err := loadOperatorSelectionDataFunc()
	if err != nil {
		return planningLogSnapshot{}, fmt.Errorf("load operator selection data: %w", err)
	}
	if operatorData == nil {
		return planningLogSnapshot{}, fmt.Errorf("load operator selection data: result is nil")
	}
	ownership, err := loadOperatorOwnershipForSelection()
	if err != nil {
		return planningLogSnapshot{}, fmt.Errorf("load operator ownership: %w", err)
	}
	itemPriorities, err := loadItemPriorityGroupsFunc()
	if err != nil {
		return planningLogSnapshot{}, fmt.Errorf("load item priorities: %w", err)
	}
	return composePlanningLogSnapshot(
		operatorData,
		ownership,
		operatorSessionSnapshot(),
		itemPriorities,
		priorityItemsSnapshot(),
		reserveRulesSnapshot(),
	), nil
}

func composePlanningLogSnapshot(
	operatorData *operatorSelectionData,
	ownership operatorOwnership,
	session operatorSessionState,
	itemPriorities map[string][]itemPriorityGroup,
	preferredItems []string,
	reserveRules map[string]int,
) planningLogSnapshot {
	locations := orderedActiveLocations(operatorData.LocationOrder, session.ActiveLocations)
	snapshot := planningLogSnapshot{
		Operators:      make([]planningOperatorEntry, 0, len(locations)),
		ItemPriorities: make([]planningLocationItems, 0, len(locations)),
	}
	for _, location := range locations {
		selection := &operatorSelectionParam{
			Location:                   location,
			Candidates:                 operatorData.TargetCandidates[location],
			TargetCandidatesByLocation: operatorData.TargetCandidates,
			RestoreGroups:              operatorData.RestoreGroups,
			KnownOperators:             operatorData.KnownOperators,
			ActiveLocations:            session.ActiveLocations,
			CompletedRestoreLocations:  session.CompletedRestoreLocations,
			TargetAssignments:          session.TargetAssignments,
			LockedRestoreAssignments:   session.LockedRestoreAssignments,
			ExcludedOperators:          session.ExcludedOperators,
		}
		selection.Usage = operatorActionUsageTarget
		target := firstOperatorName(candidatesForOwnership(selection, ownership))
		selection.Usage = operatorActionUsageRestore
		restore := firstOperatorName(candidatesForOwnership(selection, ownership))
		locationName := operatorData.LocationNames[location]
		if locationName == "" {
			locationName = location
		}
		snapshot.Operators = append(snapshot.Operators, planningOperatorEntry{
			Location:        location,
			LocationName:    locationName,
			TargetOperator:  target,
			RestoreOperator: restore,
		})

		groups := prioritizeItemGroups(itemPriorities[location], preferredItems)
		items := make([]planningItemEntry, 0, len(groups))
		for index, group := range groups {
			items = append(items, planningItemEntry{
				Priority: index + 1,
				ItemID:   group.ItemID,
				Name:     itemPriorityGroupName(group),
			})
		}
		snapshot.ItemPriorities = append(snapshot.ItemPriorities, planningLocationItems{
			Location:     location,
			LocationName: locationName,
			Items:        items,
		})
	}

	itemNames := planningItemNames(itemPriorities)
	reserveItemIDs := make([]string, 0, len(reserveRules))
	for itemID := range reserveRules {
		reserveItemIDs = append(reserveItemIDs, itemID)
	}
	sort.Strings(reserveItemIDs)
	snapshot.ReserveRules = make([]planningReserveRule, 0, len(reserveItemIDs))
	for _, itemID := range reserveItemIDs {
		snapshot.ReserveRules = append(snapshot.ReserveRules, planningReserveRule{
			ItemID:   itemID,
			Name:     itemNames[itemID],
			Quantity: reserveRules[itemID],
		})
	}
	return snapshot
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func orderedActiveLocations(order []string, active map[string]struct{}) []string {
	result := make([]string, 0, len(active))
	seen := make(map[string]struct{}, len(active))
	for _, location := range order {
		if _, enabled := active[location]; !enabled {
			continue
		}
		result = append(result, location)
		seen[location] = struct{}{}
	}
	remaining := make(map[string]struct{})
	for location := range active {
		if _, exists := seen[location]; !exists {
			remaining[location] = struct{}{}
		}
	}
	return append(result, sortedStringSet(remaining)...)
}

func firstOperatorName(candidates []operatorCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	if candidates[0].DisplayName != "" {
		return candidates[0].DisplayName
	}
	if candidates[0].CacheName != "" {
		return candidates[0].CacheName
	}
	return candidates[0].Name
}

func itemPriorityGroupName(group itemPriorityGroup) string {
	if group.DisplayName != "" {
		return group.DisplayName
	}
	return firstString(group.Candidates)
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func planningItemNames(groupsByLocation map[string][]itemPriorityGroup) map[string]string {
	result := make(map[string]string)
	for _, groups := range groupsByLocation {
		for _, group := range groups {
			if _, exists := result[group.ItemID]; !exists {
				result[group.ItemID] = itemPriorityGroupName(group)
			}
		}
	}
	return result
}

func printPlanningFocus(ctx *maa.Context, snapshot planningLogSnapshot) {
	for _, message := range planningFocusMessages(snapshot) {
		maafocus.Print(ctx, message)
	}
}

func planningFocusMessages(snapshot planningLogSnapshot) []string {
	messages := []string{i18n.T("sellproduct.plan.ready", len(snapshot.Operators))}
	itemsByLocation := make(map[string]planningLocationItems, len(snapshot.ItemPriorities))
	for _, locationItems := range snapshot.ItemPriorities {
		itemsByLocation[locationItems.Location] = locationItems
	}

	for _, operator := range snapshot.Operators {
		locationItems := itemsByLocation[operator.Location]
		itemNames := make([]string, 0, len(locationItems.Items))
		itemIDs := make(map[string]struct{}, len(locationItems.Items))
		for _, item := range locationItems.Items {
			itemNames = append(itemNames, item.Name)
			itemIDs[item.ItemID] = struct{}{}
		}
		reserves := make([]string, 0, len(snapshot.ReserveRules))
		for _, rule := range snapshot.ReserveRules {
			if _, available := itemIDs[rule.ItemID]; !available {
				continue
			}
			reserves = append(reserves, fmt.Sprintf("%s ≥ %d", rule.Name, rule.Quantity))
		}
		messages = append(messages, i18n.T(
			"sellproduct.plan.location",
			fallbackPlanningText(operator.LocationName),
			fallbackPlanningText(operator.TargetOperator),
			fallbackPlanningText(operator.RestoreOperator),
			fallbackPlanningList(itemNames, " → "),
			fallbackPlanningList(reserves, i18n.Separator()),
		))
	}
	return messages
}

func fallbackPlanningText(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return i18n.T("sellproduct.plan.none")
}

func fallbackPlanningList(values []string, separator string) string {
	if len(values) == 0 {
		return i18n.T("sellproduct.plan.none")
	}
	return strings.Join(values, separator)
}
