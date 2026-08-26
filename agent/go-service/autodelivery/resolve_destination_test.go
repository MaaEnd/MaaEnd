package autodelivery

import (
	"reflect"
	"strings"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBuildDeliveryNavigationOverrideSelectsGeneratedSubTask(t *testing.T) {
	t.Parallel()

	destination := destination{RouteNode: "normal", ZipRouteNode: "zip", RetryRouteNode: "retry"}
	override := buildDestinationNavigationOverride(destination, true)
	node := override[navigateDestinationNode].(map[string]any)
	if node["custom_action"] != "SubTask" {
		t.Fatalf("unexpected destination dispatcher: %#v", node)
	}
	if got := navigationParam(t, override, navigateDestinationNode)["sub"]; !reflect.DeepEqual(got, []string{"zip"}) {
		t.Fatalf("destination dispatcher sub = %#v", got)
	}
	retryNode := override[retryNavigateDestinationNode].(map[string]any)
	if retryNode["enabled"] != true || retryNode["custom_action"] != "SubTask" {
		t.Fatalf("unexpected destination retry dispatcher: %#v", retryNode)
	}
	if got := navigationParam(t, override, retryNavigateDestinationNode)["sub"]; !reflect.DeepEqual(got, []string{"retry"}) {
		t.Fatalf("destination retry dispatcher sub = %#v", got)
	}
	flow := override[afterResolveDestinationNode].(map[string]any)
	if !reflect.DeepEqual(flow["next"], defaultDestinationFlow()) {
		t.Fatalf("destination flow next = %#v", flow["next"])
	}
}

func TestBuildDeliveryNavigationOverrideDisablesMissingRetryRoute(t *testing.T) {
	t.Parallel()

	destination := destination{RouteNode: "normal", ZipRouteNode: "zip"}
	override := buildDestinationNavigationOverride(destination, false)
	retryNode := override[retryNavigateDestinationNode].(map[string]any)
	if retryNode["enabled"] != false {
		t.Fatalf("unexpected destination retry dispatcher: %#v", retryNode)
	}
}

func TestBuildRecycleBinResolutionOverrideSelectsAreaMapFlow(t *testing.T) {
	t.Parallel()

	override := buildRecycleBinResolutionOverride("OriginiumSciencePark")
	if len(override) != 1 {
		t.Fatalf("recycle bin override = %#v, want only the destination flow node", override)
	}
	flow := override[afterResolveDestinationNode].(map[string]any)
	want := []string{
		"AutoDeliveryViewRecycleBinOriginiumScienceParkMap",
		"AutoDeliveryStartTrackingRecycleBinOriginiumSciencePark",
	}
	if !reflect.DeepEqual(flow["next"], want) {
		t.Fatalf("recycle bin flow next = %#v, want %#v", flow["next"], want)
	}
}

func TestBuildDestinationsRejectsUnknownDepotAndKind(t *testing.T) {
	t.Parallel()

	base := generatedDestination{
		ID:           "destination",
		Kind:         destinationKindNPC,
		DepotID:      "unknown",
		Name:         map[string]string{"zh_cn": "英格"},
		Mission:      map[string]string{"zh_cn": "把货物交给英格"},
		Area:         map[string]string{"zh_cn": "源石研究园", "en_us": "Originium Science Park"},
		RouteNode:    "normal",
		ZipRouteNode: "zip",
	}
	_, _, err := buildDestinations(generatedCatalog{Destinations: []generatedDestination{base}}, map[string]depot{})
	if err == nil {
		t.Fatal("buildDestinations() must reject an unknown depot")
	}
	base.Kind = "unknown"
	_, _, err = buildDestinations(
		generatedCatalog{Destinations: []generatedDestination{base}},
		map[string]depot{"unknown": {ID: "unknown"}},
	)
	if err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("buildDestinations() error = %v, want unknown kind", err)
	}
}

func TestBuildDestinationsPreservesRecycleBinSerialID(t *testing.T) {
	t.Parallel()

	source := generatedDestination{
		ID:           "deliver_target_map01_lv005_recycle_03",
		Kind:         destinationKindRecycleBin,
		SerialID:     2,
		DepotID:      "depot",
		Name:         map[string]string{"zh_cn": "数据标识"},
		Mission:      map[string]string{"zh_cn": "把货物尽可能完整地送至资源回收站"},
		Area:         map[string]string{"zh_cn": "源石研究园", "en_us": "Originium Science Park"},
		RouteNode:    "normal",
		ZipRouteNode: "zip",
	}
	_, destinations, err := buildDestinations(
		generatedCatalog{Destinations: []generatedDestination{source}},
		map[string]depot{"depot": {ID: "depot", Map: "map01"}},
	)
	if err != nil {
		t.Fatalf("buildDestinations() error = %v", err)
	}
	if len(destinations) != 1 || destinations[0].SerialID != 2 {
		t.Fatalf("unexpected recycle bin destination: %#v", destinations)
	}

	source.SerialID = 0
	_, _, err = buildDestinations(
		generatedCatalog{Destinations: []generatedDestination{source}},
		map[string]depot{"depot": {ID: "depot", Map: "map01"}},
	)
	if err == nil || !strings.Contains(err.Error(), "serial id") {
		t.Fatalf("buildDestinations() error = %v, want invalid serial id", err)
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

func TestResolveAmbiguousRecycleBinsUsesSameMapCandidates(t *testing.T) {
	t.Parallel()

	const objective = "把货物尽可能完整地送至资源回收站"
	destinations := []destination{
		{
			ID:             "deliver_target_map02_lv002_recycle_03",
			Kind:           destinationKindRecycleBin,
			Map:            "map02",
			ObjectiveTexts: []string{objective},
		},
		{
			ID:             "deliver_target_map02_lv002_recycle_01",
			Kind:           destinationKindRecycleBin,
			Map:            "map02",
			ObjectiveTexts: []string{objective},
		},
		{
			ID:             "deliver_target_map02_lv002_recycle_02",
			Kind:           destinationKindRecycleBin,
			Map:            "map02",
			ObjectiveTexts: []string{objective},
		},
	}

	candidates, match, ok := resolveAmbiguousRecycleBins(objective, destinations)
	if !ok {
		t.Fatal("resolveAmbiguousRecycleBins() did not detect the same-map candidates")
	}
	if got := []string{candidates[0].ID, candidates[1].ID, candidates[2].ID}; !reflect.DeepEqual(got, []string{
		"deliver_target_map02_lv002_recycle_01",
		"deliver_target_map02_lv002_recycle_02",
		"deliver_target_map02_lv002_recycle_03",
	}) {
		t.Fatalf("recycle bin candidates = %#v", got)
	}
	if match.Similarity != 1 || match.RunnerUpSimilarity != 1 {
		t.Fatalf("recycle bin match = %#v", match)
	}

	destinations[0].Map = "map01"
	if _, _, ok := resolveAmbiguousRecycleBins(objective, destinations); ok {
		t.Fatal("resolveAmbiguousRecycleBins() must reject mixed maps")
	}
}

func TestParseNavigationOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nodeJSON string
		wantZip  bool
		wantErr  bool
	}{
		{name: "missing", nodeJSON: `{}`, wantZip: false},
		{name: "disabled", nodeJSON: `{"attach":{"zip":false}}`, wantZip: false},
		{name: "enabled", nodeJSON: `{"attach":{"zip":true}}`, wantZip: true},
		{name: "invalid", nodeJSON: `{"attach":{"zip":"true"}}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options, err := parseNavigationOptions(tt.nodeJSON, navigateDestinationNode)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNavigationOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if options.Zip != tt.wantZip {
				t.Fatalf("parseNavigationOptions() zip = %v, want %v", options.Zip, tt.wantZip)
			}
		})
	}
}

func TestParseDestinationSelection(t *testing.T) {
	t.Parallel()

	selection, err := parseDestinationSelection(`{"destination_id":" deliver_target_map01_lv005_recycle_02 "}`)
	if err != nil {
		t.Fatalf("parseDestinationSelection() error = %v", err)
	}
	if selection.DestinationID != "deliver_target_map01_lv005_recycle_02" {
		t.Fatalf("parseDestinationSelection() = %#v", selection)
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
