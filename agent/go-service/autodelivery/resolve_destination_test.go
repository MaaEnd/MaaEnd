package autodelivery

import (
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBuildDeliveryNavigationOverride(t *testing.T) {
	t.Parallel()

	deckY := 315.37
	destination := deliveryDestination{
		MapNavigatorZone:        "Wuling_Base",
		MapNavigatorTarget:      [2]float64{525.72, 1749.78},
		MapNavigatorTargetDeckY: &deckY,
		InitialPathPrefix: []any{
			map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
		},
		InitialPathSuffix: []any{
			[]any{724.98, 1596.8},
		},
	}

	override := buildDeliveryNavigationOverride(destination)
	initialParam := navigationParam(t, override, autoDeliveryNavigateNode)
	retryParam := navigationParam(t, override, autoDeliveryRetryNavigateNode)

	if initialParam["map_name"] != "Wuling_Base" || retryParam["map_name"] != "Wuling_Base" {
		t.Fatalf("unexpected map names: initial=%v retry=%v", initialParam["map_name"], retryParam["map_name"])
	}
	if _, exists := initialParam["zipline_policy"]; exists {
		t.Fatal("shared MapNavigator parameters must not contain legacy zipline_policy")
	}
	if _, exists := retryParam["zipline_policy"]; exists {
		t.Fatal("retry MapNavigator parameters must not contain legacy zipline_policy")
	}

	wantWaypoint := map[string]any{
		"action":        "NAVMESH",
		"target":        [2]float64{525.72, 1749.78},
		"target_deck_y": deckY,
	}
	wantInitialPath := []any{
		map[string]any{"action": "ZONE", "zone_id": "Wuling_Base"},
		[]any{724.98, 1596.8},
		wantWaypoint,
	}
	if !reflect.DeepEqual(initialParam["path"], wantInitialPath) {
		t.Fatalf("unexpected initial path: %#v", initialParam["path"])
	}
	if !reflect.DeepEqual(retryParam["path"], []any{wantWaypoint}) {
		t.Fatalf("unexpected retry path: %#v", retryParam["path"])
	}
}

func TestFindDeliveryRecognitionDetailUsesAutoDeliveryNodes(t *testing.T) {
	t.Parallel()

	area := &maa.RecognitionDetail{Name: autoDeliveryAreaOCRNode}
	destination := &maa.RecognitionDetail{Name: autoDeliveryDestinationOCRNode}
	root := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			area,
			{
				CombinedResult: []*maa.RecognitionDetail{destination},
			},
		},
	}

	if got := findDeliveryRecognitionDetail(root, autoDeliveryAreaOCRNode); got != area {
		t.Fatalf("area detail mismatch: got %p want %p", got, area)
	}
	if got := findDeliveryRecognitionDetail(root, autoDeliveryDestinationOCRNode); got != destination {
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
