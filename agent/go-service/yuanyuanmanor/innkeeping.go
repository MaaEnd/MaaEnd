package yuanyuanmanor

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// DecisionAction consumes Pipeline observations and updates dispatch decisions.
// It never captures images, runs recognitions, clicks, or advances the UI itself.
type DecisionAction struct{}

var _ maa.CustomActionRunner = &DecisionAction{}

type decisionParam struct {
	Step string `json:"step"`
}

type session struct {
	Runtime *runtimeState `json:"runtime,omitempty"`
}

type runtimeState struct {
	Cache     map[string]int `json:"cache"`
	Tables    [6]tableOrder  `json:"tables"`
	PendingID string         `json:"pending_id,omitempty"`
}

type tableOrder struct {
	Items []string `json:"items,omitempty"`
}

// Run applies one decision to the current task's attach state and preset routes.
func (a *DecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		return false
	}
	var param decisionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", "YuanyuanManorInnkeepingDecide").Msg("cannot parse innkeeping decision parameters")
		return false
	}
	raw, err := ctx.GetNodeJSON(nodeRestaurantState)
	if err != nil {
		log.Error().Err(err).Str("component", "YuanyuanManorInnkeepingDecide").Str("step", param.Step).Msg("cannot read innkeeping state node")
		return false
	}
	var node struct {
		Attach struct {
			State session `json:"state"`
		} `json:"attach"`
	}
	if err = json.Unmarshal([]byte(raw), &node); err != nil {
		log.Error().Err(err).Str("component", "YuanyuanManorInnkeepingDecide").Str("step", param.Step).Msg("cannot parse innkeeping state")
		return false
	}
	state := &node.Attach.State
	patch := make(map[string]any)
	observed := indexObservations(arg.RecognitionDetail)
	switch param.Step {
	case "runtime":
		err = decideRuntime(state, observed, patch)
	case "dispatched":
		if state.Runtime == nil || state.Runtime.PendingID == "" {
			return false
		}
		current := state.Runtime
		current.Cache[current.PendingID]++
		log.Debug().Str("dish_id", current.PendingID).Int("cached", current.Cache[current.PendingID]).Msg("dispatched dish counted in cache")
		current.PendingID = ""
	default:
		return false
	}
	if err != nil {
		log.Error().Err(err).Str("component", "YuanyuanManorInnkeepingDecide").Str("step", param.Step).Msg("innkeeping decision failed")
		return false
	}
	patch[nodeRestaurantState] = map[string]any{"attach": map[string]any{"state": state}}
	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", "YuanyuanManorInnkeepingDecide").Msg("cannot store innkeeping decision")
		return false
	}
	return true
}

type observations map[string]*maa.RecognitionDetail

// Index only this callback's results, preserving the previous depth-first lookup order.
func indexObservations(detail *maa.RecognitionDetail) observations {
	indexed := make(observations)
	var visit func(*maa.RecognitionDetail)
	visit = func(current *maa.RecognitionDetail) {
		if current == nil {
			return
		}
		if _, exists := indexed[current.Name]; !exists {
			indexed[current.Name] = current
		}
		for _, child := range current.CombinedResult {
			visit(child)
		}
	}
	visit(detail)
	return indexed
}

// CombinedResult children omit Hit in maa-framework-go beta.19. The innkeeping
// nodes queried here return nonempty boxes on a hit; failed matches have no box.
// Read that result locally instead of fetching their Raw and Draws images again.
func (o observations) hit(name string) bool {
	result := o[name]
	return result != nil && result.Box[2] > 0 && result.Box[3] > 0
}

func readIcon(observed observations, name string) (string, error) {
	result := observed[name]
	if result == nil {
		return "", fmt.Errorf("missing icon observation %s", name)
	}
	parsed, err := iconrecognition.NewRecognitionDetail(result)
	if err != nil {
		return "", err
	}
	matches, matchErr := parsed.All().Matches()
	var detailErr *iconrecognition.DetailError
	errors.As(matchErr, &detailErr)
	// no_match 表示识别正常完成但没有候选达到阈值，调用方按「当前界面没有该图标」处理。
	if detailErr != nil && detailErr.Code == iconrecognition.ErrorCodeNoMatch {
		return "", nil
	}
	if matchErr != nil {
		return "", fmt.Errorf("%s: %w", name, matchErr)
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0].ItemID, nil
}

func route(patch map[string]any, routes []string, selected string) {
	for _, name := range routes {
		patch[name] = map[string]any{"enabled": name == selected}
	}
}

func iconCandidates(patch map[string]any, name string, ids []string) {
	filters := iconrecognition.StorageFilter()
	patch[name] = map[string]any{"custom_recognition_param": iconrecognition.NewParams(
		iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI),
		iconrecognition.WithItemFilters(filters.YuanyuanManor.Any, filters.Normal.Usable),
		iconrecognition.WithItemIDs(ids...),
	)}
}

// Zero means the dots are unreadable. A full set of three queues waits instead
// of using the E-key route reserved for recognition failures.
func chooseKitchen(observed observations) (kitchen int, full bool) {
	minimum := 3
	for index, counts := range nodeKitchenCounts {
		count := -1
		for value, countNode := range counts {
			if observed.hit(countNode) {
				count = value
				break
			}
		}
		if count < 0 {
			return 0, false
		}
		number := index + 1
		if kitchen == 0 || count < minimum {
			kitchen, minimum = number, count
		}
	}
	return kitchen, minimum == 3
}

// Filed tables run only Dining OCR; unfiled tables scan for one- or two-point orders.
func setTableObservation(patch map[string]any, tableNode tableNodeGroup, order tableOrder) {
	nodes := []string{nodeObservationEmpty}
	if len(order.Items) > 0 {
		nodes = []string{tableNode.dining, nodeObservationEmpty}
	} else {
		nodes = []string{tableNode.singleOrder, tableNode.doubleOrder, nodeObservationEmpty}
	}
	patch[tableNode.observation] = map[string]any{"any_of": nodes}
}

// Runtime uses the menu recorded during menu design; table icons only match those items.
func newRuntimeState(patch map[string]any) *runtimeState {
	dishes, drinks := innkeepingState.DishIDs(), innkeepingState.DrinkIDs()
	current := &runtimeState{Cache: make(map[string]int, len(dishes))}
	for _, id := range dishes {
		current.Cache[id] = 0
	}
	all := slices.Concat(dishes, drinks)
	for _, tableNode := range tableNodes {
		setTableObservation(patch, tableNode, tableOrder{})
		for _, iconNode := range tableNode.iconNodes() {
			iconCandidates(patch, iconNode, all)
		}
	}
	return current
}

func readTableOrder(observed observations, tableNode tableNodeGroup) ([]string, error) {
	var iconNodes []string
	if observed.hit(tableNode.doubleOrder) {
		iconNodes = []string{tableNode.doublePoint1Icon, tableNode.doublePoint2Icon}
	} else if observed.hit(tableNode.singleOrder) {
		iconNodes = []string{tableNode.singlePoint1Icon}
	} else {
		return nil, nil
	}
	items := make([]string, 0, len(iconNodes))
	for _, iconNode := range iconNodes {
		id, err := readIcon(observed, iconNode)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, nil
		}
		items = append(items, id)
	}
	return items, nil
}

func observeTables(current *runtimeState, observed observations, patch map[string]any) error {
	for index := range current.Tables {
		order := &current.Tables[index]
		table := index + 1
		tableNode := tableNodes[index]
		if len(order.Items) > 0 {
			if observed.hit(tableNode.dining) {
				for _, id := range order.Items {
					if cached, dish := current.Cache[id]; dish {
						current.Cache[id] = max(cached-1, 0)
					}
				}
				log.Debug().Int("table", table).Strs("items", order.Items).Msg("dining table cleared; order deducted from cache")
				*order = tableOrder{}
				setTableObservation(patch, tableNode, *order)
			}
			continue
		}
		items, err := readTableOrder(observed, tableNode)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			*order = tableOrder{Items: items}
			setTableObservation(patch, tableNode, *order)
			log.Debug().Int("table", table).Strs("items", items).Msg("table order filed")
		}
	}
	return nil
}

// A waving marker takes precedence over the two-portion pre-stock limit.
// Otherwise, choose the first under-stocked dish in selected menu order.
func chooseDish(state *session, observed observations) (string, int) {
	dishes := innkeepingState.DishIDs()
	for index, markerNode := range nodeRecipeRestockMarkers {
		if !observed.hit(markerNode) {
			continue
		}
		slot := index + 1
		if slot > len(dishes) {
			return "", 0
		}
		return dishes[index], slot
	}
	for index, id := range dishes {
		if state.Runtime.Cache[id] < 2 {
			return id, index + 1
		}
	}
	return "", 0
}

func decideRuntime(state *session, observed observations, patch map[string]any) error {
	if state.Runtime == nil {
		state.Runtime = newRuntimeState(patch)
	}
	current := state.Runtime
	if err := observeTables(current, observed, patch); err != nil {
		return err
	}
	id, slot := chooseDish(state, observed)
	kitchen, full := chooseKitchen(observed)
	if id == "" || full {
		route(patch, runtimeRoutes, nodeRuntimeIdle)
		return nil
	}
	current.PendingID = id
	target := nodeKitchenPanelReady
	name := nodeRuntimeNextKitchen
	if kitchen != 0 {
		target = nodeKitchenSelected[kitchen-1]
		name = ""
		if !observed.hit(target) {
			name = nodeRuntimeSwitchKitchens[kitchen-1]
		}
	}
	patch[nodeRuntimeTargetKitchen] = map[string]any{"all_of": []string{target}}
	route(patch, runtimeRoutes, name)
	// The runtime recipe card order matches the selected menu order; enable its fixed slot click.
	for index, clickNode := range nodeRuntimeClickDishes {
		patch[clickNode] = map[string]any{"enabled": index+1 == slot}
	}
	return nil
}
