package restaurant

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// DecisionAction consumes Pipeline observations and updates menu/dispatch decisions.
// It never captures images, runs recognitions, clicks, or advances the UI itself.
type DecisionAction struct{}

var _ maa.CustomActionRunner = &DecisionAction{}

type decisionParam struct {
	Step      string `json:"step"`
	Kind      string `json:"kind"`
	Category  string `json:"category"`
	MaxRounds int    `json:"max_rounds"`
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
	PendingID    string     `json:"pending_id"`
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
	menuRoutes         = []string{"RestaurantMenuStapleComplete", "RestaurantMenuVegetableComplete", "RestaurantMenuDishesComplete", "RestaurantMenuDrinksComplete", "RestaurantMenuClickCard", "RestaurantMenuReadCard"}
	runtimeRoutes      = []string{"RestaurantRuntimeSwitchKitchen1", "RestaurantRuntimeSwitchKitchen2", "RestaurantRuntimeSwitchKitchen3", "RestaurantRuntimeNextKitchen", "RestaurantRuntimeClickDish1", "RestaurantRuntimeClickDish2", "RestaurantRuntimeClickDish3", "RestaurantRuntimeClickDish4", "RestaurantRuntimeClickDish5", "RestaurantRuntimeClickDish6", "RestaurantRuntimeIdle"}
	restaurantData     menuCatalog
	restaurantDataOnce sync.Once
	restaurantDataErr  error
)

// Run applies one decision to the current task's attach state and preset routes.
func (a *DecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		return false
	}
	var param decisionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", "RestaurantDecisionAction").Msg("cannot parse restaurant decision parameters")
		return false
	}
	raw, err := ctx.GetNodeJSON("RestaurantState")
	if err != nil {
		log.Error().Err(err).Str("component", "RestaurantDecisionAction").Str("step", param.Step).Msg("cannot read restaurant state node")
		return false
	}
	var node struct {
		Attach struct {
			State session `json:"state"`
		} `json:"attach"`
	}
	if err = json.Unmarshal([]byte(raw), &node); err != nil {
		log.Error().Err(err).Str("component", "RestaurantDecisionAction").Str("step", param.Step).Msg("cannot parse restaurant state")
		return false
	}
	state := &node.Attach.State
	patch := make(map[string]any)
	observed := indexObservations(arg.RecognitionDetail)
	switch param.Step {
	case "initialize":
		if param.MaxRounds < 1 || param.MaxRounds > 1000 {
			err = fmt.Errorf("maximum restaurant rounds must be between 1 and 1000")
			break
		}
		patch["RestaurantRoundGate"] = map[string]any{"max_hit": param.MaxRounds}
	case "menu_page":
		err = readMenuPage(state, param.Kind, param.Category, patch)
	case "menu_card":
		err = chooseMenuCard(state, observed, patch)
	case "menu_confirm":
		if state.Menu.PendingID == "" {
			return false
		}
		rememberMenuItem(&state.Menu, state.Menu.PendingID)
		state.Menu.PageSelected++
		state.Menu.PendingID = ""
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
		log.Error().Err(err).Str("component", "RestaurantDecisionAction").Str("step", param.Step).Msg("restaurant decision failed")
		return false
	}
	patch["RestaurantState"] = map[string]any{"attach": map[string]any{"state": state}}
	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", "RestaurantDecisionAction").Msg("cannot store restaurant decision")
		return false
	}
	return true
}

func loadRestaurantData() (menuCatalog, error) {
	restaurantDataOnce.Do(func() {
		restaurantDataErr = resource.ReadJsonResource("data/Restaurant/restaurant.json", &restaurantData)
		if restaurantDataErr == nil && (len(restaurantData.Dishes) == 0 || len(restaurantData.Drinks) == 0) {
			restaurantDataErr = fmt.Errorf("restaurant data is missing dishes or drinks")
		}
	})
	return restaurantData, restaurantDataErr
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

// CombinedResult children omit Hit in maa-framework-go beta.19. The restaurant
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
	parsed, _, err := iconrecognition.ParseRecognitionDetail(result)
	if err != nil {
		return "", err
	}
	if parsed.Error != nil && parsed.Error.Code != iconrecognition.ErrorCodeNoMatch {
		return "", fmt.Errorf("%s: %s", name, parsed.Error.Message)
	}
	if !parsed.Matched || len(parsed.Matches) == 0 {
		return "", nil
	}
	return parsed.Matches[0].ItemID, nil
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
		iconrecognition.WithItemFilters(filters.Restaurant.Any, filters.Normal.Usable),
		iconrecognition.WithItemIDs(ids...),
	)}
}

// Menu decisions preserve the selected category and UI order; Pipeline selects each card.
func readMenuPage(state *session, kind, category string, patch map[string]any) error {
	validDishCategory := slices.Contains([]string{"staple", "vegetable", "dessert"}, category)
	if (kind != "dish" && kind != "drink") || (kind == "dish" && !validDishCategory) || (kind == "drink" && category != "") || state.Menu.PendingID != "" {
		return fmt.Errorf("invalid menu page state")
	}
	cards := []maa.Rect{{552, 134, 307, 93}, {870, 134, 307, 93}, {552, 239, 307, 93}}
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
	if menu.Index < 0 || menu.Index >= len(menu.Cards) || menu.PendingID != "" {
		return fmt.Errorf("menu candidate state is missing")
	}
	id, err := readIcon(observed, "RestaurantMenuCardIcon")
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("menu card icon is unrecognized")
	}
	if observed.hit("RestaurantMenuCardSelected") {
		rememberMenuItem(menu, id)
		menu.PageSelected++
		menu.Index++
		return nextMenuCard(state, patch)
	}
	if slices.Contains(menu.Dishes, id) || slices.Contains(menu.Drinks, id) {
		return fmt.Errorf("verified menu selection %q disappeared", id)
	}
	menu.PendingID = id
	iconCandidates(patch, "RestaurantMenuPendingIcon", []string{id})
	route(patch, menuRoutes, "RestaurantMenuClickCard")
	return nil
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
			for table := 1; table <= 6; table++ {
				setTableObservation(patch, table, tableOrder{})
				for _, layout := range []string{"SinglePoint1", "DoublePoint1", "DoublePoint2"} {
					iconCandidates(patch, fmt.Sprintf("RestaurantTable%d%sIcon", table, layout), all)
				}
			}
			menu.Cards = nil
		} else {
			complete := map[string]string{
				"staple":    "RestaurantMenuStapleComplete",
				"vegetable": "RestaurantMenuVegetableComplete",
				"dessert":   "RestaurantMenuDishesComplete",
			}[menu.Category]
			if complete == "" {
				return fmt.Errorf("invalid dish category %q", menu.Category)
			}
			route(patch, menuRoutes, complete)
			return nil
		}
		route(patch, menuRoutes, "RestaurantMenuDrinksComplete")
		return nil
	}
	if menu.Index >= len(menu.Cards) {
		return fmt.Errorf("not enough available %s menu cards on the current page: selected %d, required %d", menu.Kind, selected, required)
	}
	data, err := loadRestaurantData()
	if err != nil {
		return err
	}
	var ids []string
	if menu.Kind == "dish" {
		ids = data.Dishes
	} else {
		ids = data.Drinks
	}
	iconCandidates(patch, "RestaurantMenuCardIcon", ids)
	patch["RestaurantMenuCardAnchor"] = map[string]any{
		"recognition": map[string]any{
			"type":  "DirectHit",
			"param": map[string]any{"roi": menu.Cards[menu.Index]},
		},
	}
	route(patch, menuRoutes, "RestaurantMenuReadCard")
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
	for number := 1; number <= 3; number++ {
		count := -1
		for value := 0; value <= 3; value++ {
			if observed.hit(fmt.Sprintf("RestaurantKitchen%dCount%d", number, value)) {
				count = value
				break
			}
		}
		if count < 0 {
			return 0, false
		}
		if kitchen == 0 || count < minimum {
			kitchen, minimum = number, count
		}
	}
	return kitchen, minimum == 3
}

// Filed tables run only Dining OCR; unfiled tables scan for one- or two-point orders.
func setTableObservation(patch map[string]any, table int, order tableOrder) {
	prefix := fmt.Sprintf("RestaurantTable%d", table)
	nodes := []string{"RestaurantObservationEmpty"}
	if len(order.Items) > 0 {
		nodes = []string{prefix + "Dining", "RestaurantObservationEmpty"}
	} else {
		nodes = []string{prefix + "SingleOrder", prefix + "DoubleOrder", "RestaurantObservationEmpty"}
	}
	patch[prefix+"Observation"] = map[string]any{"any_of": nodes}
}

func readTableOrder(observed observations, table int) ([]string, error) {
	prefix := fmt.Sprintf("RestaurantTable%d", table)
	layout, count := "Single", 1
	if observed.hit(prefix + "DoubleOrder") {
		layout, count = "Double", 2
	} else if !observed.hit(prefix + "SingleOrder") {
		return nil, nil
	}
	items := make([]string, 0, count)
	for point := 1; point <= count; point++ {
		id, err := readIcon(observed, fmt.Sprintf("%s%sPoint%dIcon", prefix, layout, point))
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
		prefix := fmt.Sprintf("RestaurantTable%d", table)
		if len(order.Items) > 0 {
			if observed.hit(prefix + "Dining") {
				for _, id := range order.Items {
					if cached, dish := current.Cache[id]; dish {
						current.Cache[id] = max(cached-1, 0)
					}
				}
				log.Debug().Int("table", table).Strs("items", order.Items).Msg("dining table cleared; order deducted from cache")
				*order = tableOrder{}
				setTableObservation(patch, table, *order)
			}
			continue
		}
		items, err := readTableOrder(observed, table)
		if err != nil {
			return err
		}
		if len(items) > 0 {
			*order = tableOrder{Items: items}
			setTableObservation(patch, table, *order)
			log.Debug().Int("table", table).Strs("items", items).Msg("table order filed")
		}
	}
	return nil
}

// A waving marker takes precedence over the two-portion pre-stock limit.
// Otherwise, choose the first under-stocked dish in selected menu order.
func chooseDish(state *session, observed observations) (string, int) {
	for slot := 1; slot <= 6; slot++ {
		prefix := fmt.Sprintf("RestaurantRecipe%d", slot)
		if !observed.hit(prefix + "RestockMarker") {
			continue
		}
		if slot > len(state.Menu.Dishes) {
			return "", 0
		}
		return state.Menu.Dishes[slot-1], slot
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
		route(patch, runtimeRoutes, "RestaurantRuntimeIdle")
		return nil
	}
	current.PendingID = id
	target := "RestaurantKitchenPanelReady"
	name := "RestaurantRuntimeNextKitchen"
	if kitchen != 0 {
		target = fmt.Sprintf("RestaurantKitchen%dSelected", kitchen)
		name = ""
		if !observed.hit(target) {
			name = fmt.Sprintf("RestaurantRuntimeSwitchKitchen%d", kitchen)
		}
	}
	patch["RestaurantRuntimeTargetKitchen"] = map[string]any{"all_of": []string{target}}
	route(patch, runtimeRoutes, name)
	// The runtime recipe card order matches the selected menu order; enable its fixed slot click.
	for cardSlot := 1; cardSlot <= 6; cardSlot++ {
		patch[fmt.Sprintf("RestaurantRuntimeClickDish%d", cardSlot)] = map[string]any{"enabled": cardSlot == slot}
	}
	return nil
}
