package autodelivery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildDeliveryDepotDataUsesGeneratedTargetsAndOverrides(t *testing.T) {
	t.Parallel()

	catalog := generatedCatalog{Depots: []generatedDepot{
		{ID: "domain_1_lv005_depot_1", Map: "map01", U: 1043.46, V: 804.72},
		{ID: "domain_1_lv006_depot_1", Map: "map01", U: 912.255, V: 263.96},
		{ID: "domain_2_lv002_depot_1", Map: "map02", U: 950.5, V: 1832.77},
	}}
	overridePath := []any{
		map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
		map[string]any{"action": "NAVMESH", "target": []any{952.2, 1832.68}},
	}
	retryPath := []any{
		map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
		[]any{952.2, 1832.68, true},
	}
	config := navigationOverrides{Depots: []depotOverride{
		{SourceID: "domain_1_lv006_depot_1", RetryPath: retryPath},
		{SourceID: "domain_2_lv002_depot_1", Path: overridePath, RetryPath: retryPath},
	}}

	depots, err := buildDepots(catalog, config)
	if err != nil {
		t.Fatalf("buildDeliveryDepotData() error = %v", err)
	}
	if got := depots["domain_1_lv005_depot_1"].Path; !reflect.DeepEqual(got, []any{
		map[string]any{
			"action": "NAVMESH",
			"target": [2]float64{1043.46, 804.72},
		},
	}) {
		t.Fatalf("unexpected generated path: %#v", got)
	}
	if got := depots["domain_2_lv002_depot_1"].Path; !reflect.DeepEqual(got, overridePath) {
		t.Fatalf("unexpected override path: %#v", got)
	}
	if got := depots["domain_2_lv002_depot_1"].RetryPath; !reflect.DeepEqual(got, retryPath) {
		t.Fatalf("unexpected retry path: %#v", got)
	}
	if got := depots["domain_1_lv006_depot_1"].Path; !reflect.DeepEqual(got, []any{
		map[string]any{
			"action": "NAVMESH",
			"target": [2]float64{912.255, 263.96},
		},
	}) {
		t.Fatalf("retry-only override replaced generated path: %#v", got)
	}
	if got := depots["domain_1_lv006_depot_1"].RetryPath; !reflect.DeepEqual(got, retryPath) {
		t.Fatalf("unexpected retry-only path: %#v", got)
	}
}

func TestBuildDeliveryDepotDataRejectsUnknownOverride(t *testing.T) {
	t.Parallel()

	_, err := buildDepots(
		generatedCatalog{Depots: []generatedDepot{
			{ID: "domain_1_lv005_depot_1", Map: "map01", U: 1043.46, V: 804.72},
		}},
		navigationOverrides{Depots: []depotOverride{
			{SourceID: "unknown", Path: []any{map[string]any{"action": "NAVMESH"}}},
		}},
	)
	if err == nil {
		t.Fatal("buildDeliveryDepotData() must reject overrides missing from the generated catalog")
	}
}

func TestBuildDeliveryDepotDataRejectsEmptyOverride(t *testing.T) {
	t.Parallel()

	_, err := buildDepots(
		generatedCatalog{Depots: []generatedDepot{
			{ID: "domain_1_lv005_depot_1", Map: "map01", U: 1043.46, V: 804.72},
		}},
		navigationOverrides{Depots: []depotOverride{
			{SourceID: "domain_1_lv005_depot_1"},
		}},
	)
	if err == nil {
		t.Fatal("buildDeliveryDepotData() must reject overrides without path, retry_path or departure_path")
	}
}

func TestBuildDeliveryDepotDataWithRepositoryData(t *testing.T) {
	t.Parallel()

	var catalog generatedCatalog
	readAutoDeliveryTestJSON(
		t,
		filepath.Join("..", "..", "..", "assets", "data", "AutoDelivery", "delivery_destinations.json"),
		&catalog,
	)
	var config navigationOverrides
	readAutoDeliveryTestJSON(
		t,
		filepath.Join("..", "..", "..", "assets", "data", "AutoDelivery", "overrides.json"),
		&config,
	)

	depots, err := buildDepots(catalog, config)
	if err != nil {
		t.Fatalf("buildDeliveryDepotData() error = %v", err)
	}
	if len(depots) != 5 {
		t.Fatalf("depot count = %d, want 5", len(depots))
	}
	areas, destinations, err := buildDestinations(catalog, config, depots)
	if err != nil {
		t.Fatalf("buildDeliveryDestinationData() error = %v", err)
	}
	if len(areas) != 5 || len(destinations) != 22 {
		t.Fatalf("repository delivery data has %d areas and %d destinations, want 5 and 22", len(areas), len(destinations))
	}
	foundWuling := false
	for _, currentArea := range areas {
		if currentArea.ID != "WulingCity" {
			continue
		}
		foundWuling = true
		if !reflect.DeepEqual(currentArea.Texts, []string{"Wuling City", "武陵城", "무릉성"}) {
			t.Fatalf("Wuling task-detail texts = %#v", currentArea.Texts)
		}
	}
	if !foundWuling {
		t.Fatal("repository delivery data has no WulingCity area")
	}
	matchedArea, _, err := resolveArea("武陵城", areas)
	if err != nil {
		t.Fatalf("resolveArea() error = %v", err)
	}
	if matchedArea.ID != "WulingCity" {
		t.Fatalf("resolveArea() area = %q, want WulingCity", matchedArea.ID)
	}
	wulingDestinations := make([]destination, 0)
	for _, candidate := range destinations {
		if candidate.AreaID == matchedArea.ID {
			wulingDestinations = append(wulingDestinations, candidate)
		}
	}
	recycle, _, err := resolveDestinationText("把货物尽可能完整地送至资源回收站", wulingDestinations)
	if err != nil {
		t.Fatalf("resolveDestinationText() Wuling recycle bin error = %v", err)
	}
	if recycle.ID != "deliver_target_map02_lv002_recycle_01" {
		t.Fatalf("Wuling recycle bin = %q", recycle.ID)
	}
	npc, _, err := resolveDestinationText("把货物尽可能完整地交给苏白易", wulingDestinations)
	if err != nil {
		t.Fatalf("resolveDestinationText() Wuling NPC error = %v", err)
	}
	if npc.ID != "deliver_target_map02_lv002_01" {
		t.Fatalf("Wuling NPC = %q", npc.ID)
	}
	for _, destination := range destinations {
		if destination.DepotID == "" {
			t.Fatalf("destination %q has no depot_id", destination.ID)
		}
	}

	depotOverrides := make(map[string]depotOverride, len(config.Depots))
	for _, override := range config.Depots {
		depotOverrides[override.SourceID] = override
	}

	valleyIVDepotCount := 0
	for _, source := range catalog.Depots {
		depot := depots[source.ID]
		if source.Map != "map01" {
			continue
		}
		valleyIVDepotCount++
		wantPath := []any{
			map[string]any{
				"action": "NAVMESH",
				"target": [2]float64{source.U, source.V},
			},
		}
		override := depotOverrides[source.ID]
		if len(override.Path) != 0 {
			wantPath = override.Path
		}
		if !reflect.DeepEqual(depot.Path, wantPath) {
			t.Fatalf("Valley IV depot %q path = %#v, want %#v", source.ID, depot.Path, wantPath)
		}
		if !reflect.DeepEqual(depot.RetryPath, override.RetryPath) {
			t.Fatalf("Valley IV depot %q retry path = %#v, want %#v", source.ID, depot.RetryPath, override.RetryPath)
		}
	}
	if valleyIVDepotCount != 3 {
		t.Fatalf("Valley IV depot count = %d, want 3", valleyIVDepotCount)
	}

	if len(config.Depots) != 3 {
		t.Fatalf("depot override count = %d, want 3", len(config.Depots))
	}
	for _, override := range config.Depots {
		depot, exists := depots[override.SourceID]
		if !exists {
			t.Fatalf("overridden depot %q is missing", override.SourceID)
		}
		if len(override.Path) != 0 && !reflect.DeepEqual(depot.Path, override.Path) {
			t.Fatalf("overridden depot %q path = %#v, want %#v", override.SourceID, depot.Path, override.Path)
		}
		if !reflect.DeepEqual(depot.RetryPath, override.RetryPath) {
			t.Fatalf("overridden depot %q retry path = %#v, want %#v", override.SourceID, depot.RetryPath, override.RetryPath)
		}
		if !reflect.DeepEqual(depot.DeparturePath, override.DeparturePath) {
			t.Fatalf(
				"overridden depot %q departure path = %#v, want %#v",
				override.SourceID,
				depot.DeparturePath,
				override.DeparturePath,
			)
		}
	}

	if len(config.Destinations) != 6 {
		t.Fatalf("destination override count = %d, want 6", len(config.Destinations))
	}
	destinationByID := make(map[string]destination, len(destinations))
	for _, destination := range destinations {
		destinationByID[destination.ID] = destination
	}
	for _, override := range config.Destinations {
		destination, exists := destinationByID[override.SourceID]
		if !exists {
			t.Fatalf("overridden destination %q is missing", override.SourceID)
		}
		wantPath := append([]any{}, depots[destination.DepotID].DeparturePath...)
		wantPath = append(wantPath, override.Path...)
		if !reflect.DeepEqual(destination.Path, wantPath) {
			t.Fatalf("overridden destination %q path = %#v, want %#v", override.SourceID, destination.Path, wantPath)
		}
	}
}

func TestBuildDepotNavigationOverride(t *testing.T) {
	t.Parallel()

	path := []any{map[string]any{"action": "NAVMESH", "target": [2]float64{1043.46, 804.72}}}
	retryPath := []any{map[string]any{"action": "NAVMESH", "target": [2]float64{1040, 800}}}
	override := buildDepotNavigationOverride(depot{Path: path, RetryPath: retryPath}, true)
	walkNode, ok := override[navigateDepotNode].(map[string]any)
	if !ok {
		t.Fatalf("walk node override has type %T", override[navigateDepotNode])
	}
	if walkNode["custom_action"] != "MapNavigateAction" {
		t.Fatalf("custom action = %v", walkNode["custom_action"])
	}
	param, ok := walkNode["custom_action_param"].(map[string]any)
	if !ok || !reflect.DeepEqual(param["path"], path) {
		t.Fatalf("unexpected navigation parameters: %#v", walkNode["custom_action_param"])
	}
	if param["zip"] != true {
		t.Fatalf("depot navigation zip = %v, want true", param["zip"])
	}
	retryNode, ok := override[retryNavigateDepotNode].(map[string]any)
	if !ok {
		t.Fatalf("retry node override has type %T", override[retryNavigateDepotNode])
	}
	if retryNode["enabled"] != true || retryNode["custom_action"] != "MapNavigateAction" {
		t.Fatalf("unexpected retry node override: %#v", retryNode)
	}
	retryParam, ok := retryNode["custom_action_param"].(map[string]any)
	if !ok || !reflect.DeepEqual(retryParam["path"], retryPath) {
		t.Fatalf("unexpected retry navigation parameters: %#v", retryNode["custom_action_param"])
	}
	if _, exists := retryParam["zip"]; exists {
		t.Fatalf("retry navigation must not inherit the initial zip option: %#v", retryParam)
	}
}

func TestBuildDepotNavigationOverrideDisablesMissingRetryPath(t *testing.T) {
	t.Parallel()

	override := buildDepotNavigationOverride(depot{
		Path: []any{map[string]any{"action": "NAVMESH", "target": [2]float64{1043.46, 804.72}}},
	}, false)
	param := navigationParam(t, override, navigateDepotNode)
	if param["zip"] != false {
		t.Fatalf("depot navigation zip = %v, want false", param["zip"])
	}
	retryNode, ok := override[retryNavigateDepotNode].(map[string]any)
	if !ok || retryNode["enabled"] != false {
		t.Fatalf("unexpected retry node override: %#v", override[retryNavigateDepotNode])
	}
}

func readAutoDeliveryTestJSON(t *testing.T, path string, target any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %q: %v", path, err)
	}
}
