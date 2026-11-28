package yuanyuanmanor

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// DecisionAction consumes Pipeline observations and updates menu/dispatch decisions.
// It never captures images, runs recognitions, clicks, or advances the UI itself.
type DecisionAction struct{}

var _ maa.CustomActionRunner = &DecisionAction{}

type decisionParam struct {
	Step     string `json:"step"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
}

type session struct {
	Menu    menuState     `json:"menu"`
	Runtime *runtimeState `json:"runtime,omitempty"`
}

type menuState struct {
	Dishes       []string   `json:"dishes"`
	Drinks       []string   `json:"drinks"`
	Kind         string     `json:"kind"`
	Category     string     `json:"category"`
	PageSelected int        `json:"page_selected"`
	Cards        []maa.Rect `json:"cards"`
	Index        int        `json:"index"`
	ClickingID   string     `json:"clicking_id,omitempty"`
}

type runtimeState struct {
	Cache     map[string]int `json:"cache"`
	Tables    [6]tableOrder  `json:"tables"`
	PendingID string         `json:"pending_id,omitempty"`
}

type tableOrder struct {
	Items []string `json:"items,omitempty"`
}

var (
	innkeepingData     menuCatalog
	innkeepingDataOnce sync.Once
	innkeepingDataErr  error
)

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
	case "menu_page":
		err = readMenuPage(state, param.Kind, param.Category, patch)
	case "menu_card":
		err = chooseMenuCard(state, observed, patch)
	case "menu_advance":
		if state.Menu.ClickingID == "" || !observed.hit(nodeMenuSelectedCircle) {
			return false
		}
		rememberMenuItem(&state.Menu, state.Menu.ClickingID)
		state.Menu.ClickingID = ""
		state.Menu.PageSelected++
		state.Menu.Index++
		err = nextMenuCard(state, patch)
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

func loadInnkeepingData() (menuCatalog, error) {
	innkeepingDataOnce.Do(func() {
		innkeepingDataErr = resource.ReadJsonResource("data/YuanyuanManor/inn_items.json", &innkeepingData)
		if innkeepingDataErr == nil && (len(innkeepingData.Dishes) == 0 || len(innkeepingData.Drinks) == 0) {
			innkeepingDataErr = fmt.Errorf("innkeeping data is missing dishes or drinks")
		}
	})
	return innkeepingData, innkeepingDataErr
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

func iconCandidates(patch map[string]any, name string, ids []string, options ...iconrecognition.Option) {
	filters := iconrecognition.StorageFilter()
	options = append([]iconrecognition.Option{
		iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI),
		iconrecognition.WithItemFilters(filters.YuanyuanManor.Any, filters.Normal.Usable),
		iconrecognition.WithItemIDs(ids...),
	}, options...)
	patch[name] = map[string]any{"custom_recognition_param": iconrecognition.NewParams(
		options...,
	)}
}

// Menu decisions preserve the selected category and UI order; Pipeline selects each card.
func readMenuPage(state *session, kind, category string, patch map[string]any) error {
	validDishCategory := slices.Contains([]string{"staple", "vegetable", "dessert"}, category)
	if (kind != "dish" && kind != "drink") || (kind == "dish" && !validDishCategory) || (kind == "drink" && category != "") || state.Menu.ClickingID != "" {
		return fmt.Errorf("invalid menu page state")
	}
	cards := make([]maa.Rect, 0, 8)
	for _, y := range []int{134, 239, 344, 450} {
		cards = append(cards, maa.Rect{552, y, 307, 93}, maa.Rect{870, y, 307, 93})
	}
	state.Menu.Kind, state.Menu.Category, state.Menu.PageSelected = kind, category, 0
	state.Menu.Cards, state.Menu.Index = cards, 0
	return nextMenuCard(state, patch)
}

func rememberMenuItem(menu *menuState, id string) {
	if menu.Kind == "dish" {
		if !slices.Contains(menu.Dishes, id) {
			menu.Dishes = append(menu.Dishes, id)
		}
	} else if !slices.Contains(menu.Drinks, id) {
		menu.Drinks = append(menu.Drinks, id)
	}
}

func chooseMenuCard(state *session, observed observations, patch map[string]any) error {
	menu := &state.Menu
	if menu.Index < 0 || menu.Index >= len(menu.Cards) || menu.ClickingID != "" {
		return fmt.Errorf("menu candidate state is missing")
	}
	id, err := readIcon(observed, nodeMenuCardIcon)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("menu card icon is unrecognized")
	}
	if slices.Contains(menu.Dishes, id) || slices.Contains(menu.Drinks, id) {
		return fmt.Errorf("verified menu selection %q disappeared", id)
	}
	quantity, err := readMenuCardQuantity(observed)
	if err != nil || quantity < 10 {
		if err != nil {
			log.Debug().Err(err).Str("item_id", id).Int("card", menu.Index+1).Msg("skipping menu card with unreadable quantity")
		}
		menu.Index++
		return nextMenuCard(state, patch)
	}
	card := menu.Cards[menu.Index]
	menu.ClickingID = id
	route(patch, menuRoutes, nodeMenuClickCard)
	patch[nodeMenuAdvanceCard] = map[string]any{"enabled": true}
	patch[nodeMenuClickCard] = map[string]any{
		"enabled": true,
		"target":  []int{card[0] + 230, card[1] + 46},
	}
	return nil
}

func readMenuCardQuantity(observed observations) (int, error) {
	detail := observed[nodeMenuCardQuantity]
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("missing menu card quantity OCR")
	}
	for _, results := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.Filtered, detail.Results.All} {
		for _, result := range results {
			if result == nil {
				continue
			}
			text, ok := result.AsOCR()
			if ok && text != nil && text.Text != "" {
				quantity, err := ocrnum.Parse(text.Text)
				if err == nil {
					return quantity, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("menu card quantity OCR has no usable number")
}

func nextMenuCard(state *session, patch map[string]any) error {
	menu := &state.Menu
	selected, required := menu.PageSelected, 2
	if menu.Kind == "drink" {
		selected, required = len(menu.Drinks), 3
	}
	if selected >= required {
		if menu.Kind == "drink" {
			state.Runtime = &runtimeState{Cache: make(map[string]int, len(menu.Dishes))}
			for _, id := range menu.Dishes {
				state.Runtime.Cache[id] = 0
			}
			all := slices.Concat(menu.Dishes, menu.Drinks)
			for _, tableNode := range tableNodes {
				setTableObservation(patch, tableNode, tableOrder{})
				for _, iconNode := range tableNode.iconNodes() {
					iconCandidates(patch, iconNode, all)
				}
			}
			menu.Cards = nil
		} else {
			complete := map[string]string{
				"staple":    nodeMenuStapleComplete,
				"vegetable": nodeMenuVegetableComplete,
				"dessert":   nodeMenuDishesComplete,
			}[menu.Category]
			if complete == "" {
				return fmt.Errorf("invalid dish category %q", menu.Category)
			}
			route(patch, menuRoutes, complete)
			return nil
		}
		route(patch, menuRoutes, nodeMenuDrinksComplete)
		return nil
	}
	if menu.Index >= len(menu.Cards) {
		route(patch, menuRoutes, nodeMenuInsufficient)
		return nil
	}
	data, err := loadInnkeepingData()
	if err != nil {
		return err
	}
	var ids []string
	if menu.Kind == "dish" {
		ids = data.Dishes
	} else {
		ids = data.Drinks
	}
	iconCandidates(patch, nodeMenuCardIcon, ids, iconrecognition.WithThreshold(0.75))
	card := menu.Cards[menu.Index]
	patch[nodeMenuCardQuantity] = map[string]any{
		"roi": []int{card[0] + 4, card[1] + 66, card[2] - 184, card[3] - 72},
	}
	patch[nodeMenuCardAnchor] = map[string]any{
		"recognition": map[string]any{
			"type":  "DirectHit",
			"param": map[string]any{"roi": menu.Cards[menu.Index]},
		},
	}
	route(patch, menuRoutes, nodeMenuReadCard)
	return nil
}

// Only item IDs are required; cooks and cooking times are deliberately absent.
type menuCatalog struct {
	Dishes []string `json:"dishes"`
	Drinks []string `json:"drinks"`
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
	for index, markerNode := range nodeRecipeRestockMarkers {
		if !observed.hit(markerNode) {
			continue
		}
		slot := index + 1
		if slot > len(state.Menu.Dishes) {
			return "", 0
		}
		return state.Menu.Dishes[index], slot
	}
	for index, id := range state.Menu.Dishes {
		if state.Runtime.Cache[id] < 2 {
			return id, index + 1
		}
	}
	return "", 0
}

func decideRuntime(state *session, observed observations, patch map[string]any) error {
	current := state.Runtime
	if current == nil {
		return fmt.Errorf("runtime cache is missing")
	}
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
