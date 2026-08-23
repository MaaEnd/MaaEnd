package autodelivery

import (
	"reflect"
	"strings"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBuildDeliveryNavigationOverride(t *testing.T) {
	t.Parallel()

	path := []any{
		map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
		[]any{724.98, 1596.8},
		map[string]any{
			"action":        "NAVMESH",
			"target":        [2]float64{525.72, 1749.78},
			"target_deck_y": 315.37,
		},
	}
	destination := destination{
		Path: path,
	}

	override := buildDestinationNavigationOverride(destination, true)
	param := navigationParam(t, override, navigateDestinationNode)

	if _, exists := param["map_name"]; exists {
		t.Fatalf("destination navigation must let MapNavigator derive the current map: %#v", param)
	}
	if _, exists := param["zipline_policy"]; exists {
		t.Fatal("shared MapNavigator parameters must not contain legacy zipline_policy")
	}
	if param["zip"] != true {
		t.Fatalf("MapNavigator zip request = %v, want true", param["zip"])
	}

	if !reflect.DeepEqual(param["path"], path) {
		t.Fatalf("unexpected path: %#v", param["path"])
	}
}

func TestBuildDeliveryNavigationOverrideUsesCurrentMap(t *testing.T) {
	t.Parallel()

	override := buildDestinationNavigationOverride(destination{
		Path: []any{map[string]any{
			"action": "NAVMESH",
			"target": [2]float64{1043.46, 804.72},
		}},
	}, false)
	param := navigationParam(t, override, navigateDestinationNode)
	if _, exists := param["map_name"]; exists {
		t.Fatalf("destination navigation must let MapNavigator derive the current map: %#v", param)
	}
	if param["zip"] != false {
		t.Fatalf("destination navigation zip = %v, want false", param["zip"])
	}
}

func TestBuildDeliveryDestinationDataIncludesUnconfiguredCatalogEntries(t *testing.T) {
	t.Parallel()

	catalog := generatedCatalog{
		Depots: []generatedDepot{
			{ID: "domain_1_lv005_depot_1", Map: "map01", U: 1043.46, V: 804.72},
			{ID: "domain_2_lv005_depot_1", Map: "map02", U: 1252.43, V: 1752.92},
		},
		Destinations: []generatedDestination{
			{
				ID:      "deliver_target_map01_lv005_recycle_02",
				Kind:    destinationKindRecycleBin,
				DepotID: "domain_1_lv005_depot_1",
				U:       100,
				V:       200,
				Name:    map[string]string{"zh_cn": "学术复读羽兽", "en_us": "Academic Repeater Bird"},
				Mission: map[string]string{"zh_cn": "送至资源回收站", "en_us": "Deliver to the recycling station"},
				Area:    map[string]string{"zh_cn": "源石研究园", "en_us": "Originium Science Park"},
			},
			{
				ID:      "deliver_target_map02_lv005_01",
				Kind:    destinationKindNPC,
				DepotID: "domain_2_lv005_depot_1",
				U:       300,
				V:       400,
				Name:    map[string]string{"zh_cn": "经纬田区", "en_us": "Jingwei Field Area"},
				Mission: map[string]string{"zh_cn": "送至经纬田区", "en_us": "Deliver to Jingwei Field Area"},
				Area:    map[string]string{"zh_cn": "试验园区", "en_us": "Test Area"},
			},
		},
	}
	depotPrefix := []any{map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"}}
	destinationPath := []any{[]any{320.0, 410.0}}
	config := navigationOverrides{
		Depots: []depotOverride{
			{SourceID: "domain_2_lv005_depot_1", DeparturePath: depotPrefix},
		},
		Destinations: []destinationOverride{
			{SourceID: "deliver_target_map02_lv005_01", Path: destinationPath},
		},
	}

	depots, err := buildDepots(catalog, config)
	if err != nil {
		t.Fatalf("buildDeliveryDepotData() error = %v", err)
	}
	areas, destinations, err := buildDestinations(catalog, config, depots)
	if err != nil {
		t.Fatalf("buildDeliveryDestinationData() error = %v", err)
	}
	if len(areas) != 2 || areas[0].ID != "OriginiumSciencePark" || areas[1].ID != "TestArea" {
		t.Fatalf("unexpected areas: %#v", areas)
	}
	if areas[0].DepotID != "domain_1_lv005_depot_1" || areas[1].DepotID != "domain_2_lv005_depot_1" {
		t.Fatalf("unexpected area depots: %#v", areas)
	}
	if len(destinations) != 2 {
		t.Fatalf("destination count = %d, want 2", len(destinations))
	}

	valley := destinations[0]
	if valley.ID != "deliver_target_map01_lv005_recycle_02" || valley.DepotID != "domain_1_lv005_depot_1" {
		t.Fatalf("unexpected generated Valley IV destination: %#v", valley)
	}
	if !reflect.DeepEqual(valley.DestinationTexts, []string{"Academic Repeater Bird", "学术复读羽兽"}) {
		t.Fatalf("recycle destination must use its unique buyer name: %#v", valley.DestinationTexts)
	}
	if valley.Kind != destinationKindRecycleBin {
		t.Fatalf("recycle destination kind = %q, want %q", valley.Kind, destinationKindRecycleBin)
	}
	wantGeneratedPath := []any{map[string]any{
		"action": "NAVMESH",
		"target": [2]float64{100, 200},
	}}
	if !reflect.DeepEqual(valley.Path, wantGeneratedPath) {
		t.Fatalf("unexpected generated Valley IV path: %#v", valley.Path)
	}
	wuling := destinations[1]
	if wuling.ID != "deliver_target_map02_lv005_01" || wuling.DepotID != "domain_2_lv005_depot_1" {
		t.Fatalf("unexpected configured Wuling destination: %#v", wuling)
	}
	wantPath := append(append([]any{}, depotPrefix...), destinationPath...)
	if !reflect.DeepEqual(wuling.Path, wantPath) {
		t.Fatalf("unexpected configured Wuling path: %#v", wuling.Path)
	}
}

func TestBuildDeliveryDestinationDataRejectsEmptyOverridePath(t *testing.T) {
	t.Parallel()

	catalog := generatedCatalog{Destinations: []generatedDestination{
		{
			ID:      "deliver_target_map01_lv005_01",
			Kind:    destinationKindNPC,
			U:       100,
			V:       200,
			Name:    map[string]string{"zh_cn": "英格"},
			Mission: map[string]string{"zh_cn": "把货物交给英格"},
			Area:    map[string]string{"zh_cn": "源石研究园"},
		},
	}}
	_, _, err := buildDestinations(
		catalog,
		navigationOverrides{Destinations: []destinationOverride{
			{SourceID: "deliver_target_map01_lv005_01"},
		}},
		map[string]depot{},
	)
	if err == nil {
		t.Fatal("buildDeliveryDestinationData() must reject an override with an empty path")
	}
}

func TestBuildDeliveryDestinationDataRejectsUnknownDepot(t *testing.T) {
	t.Parallel()

	catalog := generatedCatalog{Destinations: []generatedDestination{
		{
			ID:      "deliver_target_map01_lv005_01",
			Kind:    destinationKindNPC,
			DepotID: "unknown",
			Name:    map[string]string{"zh_cn": "英格"},
			Mission: map[string]string{"zh_cn": "把货物交给英格"},
			Area:    map[string]string{"zh_cn": "源石研究园", "en_us": "Originium Science Park"},
		},
	}}
	_, _, err := buildDestinations(catalog, navigationOverrides{}, map[string]depot{})
	if err == nil {
		t.Fatal("buildDeliveryDestinationData() must reject an unknown depot_id")
	}
}

func TestBuildDeliveryDestinationDataRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	catalog := generatedCatalog{Destinations: []generatedDestination{
		{
			ID:      "deliver_target_map01_lv005_01",
			Kind:    "unknown",
			Name:    map[string]string{"zh_cn": "英格"},
			Mission: map[string]string{"zh_cn": "把货物交给英格"},
			Area:    map[string]string{"zh_cn": "源石研究园", "en_us": "Originium Science Park"},
		},
	}}
	_, _, err := buildDestinations(catalog, navigationOverrides{}, map[string]depot{})
	if err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("buildDestinations() error = %v, want unknown kind", err)
	}
}

func TestResolveDestinationTextSpecialCasesRecycleBins(t *testing.T) {
	t.Parallel()

	destinations := []destination{
		{
			ID:               "deliver_target_map02_lv002_01",
			Kind:             destinationKindNPC,
			DestinationTexts: []string{"苏白易"},
			ObjectiveTexts:   []string{"把货物尽可能完整地交给苏白易"},
		},
		{
			ID:               "deliver_target_map02_lv002_recycle_01",
			Kind:             destinationKindRecycleBin,
			DestinationTexts: []string{"火锅探店达人"},
			ObjectiveTexts:   []string{"把货物尽可能完整地送至资源回收站"},
		},
	}

	npc, npcMatch, err := resolveDestinationText("把货物尽可能完整地交给苏白易", destinations)
	if err != nil {
		t.Fatalf("resolveDestinationText() NPC error = %v", err)
	}
	if npc.ID != "deliver_target_map02_lv002_01" || npcMatch.DestinationText != "苏白易" {
		t.Fatalf("unexpected NPC match: destination=%#v match=%#v", npc, npcMatch)
	}

	recycle, recycleMatch, err := resolveDestinationText("把货物尽可能完整地送至资源回收站", destinations)
	if err != nil {
		t.Fatalf("resolveDestinationText() recycle bin error = %v", err)
	}
	if recycle.ID != "deliver_target_map02_lv002_recycle_01" ||
		recycleMatch.ObjectiveText != "把货物尽可能完整地送至资源回收站" {
		t.Fatalf("unexpected recycle bin match: destination=%#v match=%#v", recycle, recycleMatch)
	}
}

func TestResolveDestinationTextRejectsAmbiguousRecycleBins(t *testing.T) {
	t.Parallel()

	const objective = "把货物尽可能完整地送至资源回收站"
	destinations := []destination{
		{
			ID:             "deliver_target_map01_lv005_recycle_02",
			Kind:           destinationKindRecycleBin,
			ObjectiveTexts: []string{objective},
		},
		{
			ID:             "deliver_target_map01_lv005_recycle_03",
			Kind:           destinationKindRecycleBin,
			ObjectiveTexts: []string{objective},
		},
	}

	_, _, err := resolveDestinationText(objective, destinations)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("resolveDestinationText() error = %v, want ambiguous recycle bins", err)
	}
}

func TestParseNavigationOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		paramJSON string
		wantZip   bool
		wantErr   bool
	}{
		{name: "empty", wantZip: false},
		{name: "disabled", paramJSON: `{"zip":false}`, wantZip: false},
		{name: "enabled", paramJSON: `{"zip":true}`, wantZip: true},
		{name: "invalid", paramJSON: `{"zip":"true"}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options, err := parseNavigationOptions(tt.paramJSON)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNavigationOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if options.Zip != tt.wantZip {
				t.Fatalf("parseNavigationOptions() zip = %v, want %v", options.Zip, tt.wantZip)
			}
		})
	}
}

func TestFindDeliveryRecognitionDetailUsesAutoDeliveryNodes(t *testing.T) {
	t.Parallel()

	area := &maa.RecognitionDetail{Name: areaOCRNode}
	destination := &maa.RecognitionDetail{Name: destinationOCRNode}
	root := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			area,
			{
				CombinedResult: []*maa.RecognitionDetail{destination},
			},
		},
	}

	if got := findRecognitionDetail(root, areaOCRNode); got != area {
		t.Fatalf("area detail mismatch: got %p want %p", got, area)
	}
	if got := findRecognitionDetail(root, destinationOCRNode); got != destination {
		t.Fatalf("destination detail mismatch: got %p want %p", got, destination)
	}
}

func navigationParam(t *testing.T, override map[string]any, node string) map[string]any {
	t.Helper()

	nodeOverride, ok := override[node].(map[string]any)
	if !ok {
		t.Fatalf("node %q override has type %T", node, override[node])
	}
	param, ok := nodeOverride["custom_action_param"].(map[string]any)
	if !ok {
		t.Fatalf("node %q custom_action_param has type %T", node, nodeOverride["custom_action_param"])
	}
	return param
}
