package autodelivery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildDepotsUsesGeneratedRouteNodes(t *testing.T) {
	t.Parallel()

	generated := generatedCatalog{Depots: []generatedDepot{
		{
			ID:             "domain_1_lv005_depot_1",
			Map:            "map01",
			RouteNode:      "AutoDeliveryRouteDepotDefault",
			ZipRouteNode:   "AutoDeliveryRouteDepotDefaultWithZipline",
			RetryRouteNode: "AutoDeliveryRouteDepotRetryDefault",
		},
	}}
	depots, err := buildDepots(generated)
	if err != nil {
		t.Fatalf("buildDepots() error = %v", err)
	}
	depot := depots["domain_1_lv005_depot_1"]
	if depot.RouteNode != "AutoDeliveryRouteDepotDefault" ||
		depot.ZipRouteNode != "AutoDeliveryRouteDepotDefaultWithZipline" ||
		depot.RetryRouteNode != "AutoDeliveryRouteDepotRetryDefault" {
		t.Fatalf("unexpected generated depot: %#v", depot)
	}
}

func TestBuildDepotsRejectsIncompleteRouteNodes(t *testing.T) {
	t.Parallel()

	_, err := buildDepots(generatedCatalog{Depots: []generatedDepot{{ID: "depot", Map: "map01"}}})
	if err == nil {
		t.Fatal("buildDepots() must reject incomplete route nodes")
	}
}

func TestRepositoryRuntimeCatalog(t *testing.T) {
	t.Parallel()

	var generated generatedCatalog
	readAutoDeliveryTestJSON(
		t,
		filepath.Join("..", "..", "..", "assets", "data", "AutoDelivery", "catalog.json"),
		&generated,
	)
	depots, err := buildDepots(generated)
	if err != nil {
		t.Fatalf("buildDepots() error = %v", err)
	}
	areas, destinations, err := buildDestinations(generated, depots)
	if err != nil {
		t.Fatalf("buildDestinations() error = %v", err)
	}
	if len(depots) != 5 || len(areas) != 5 || len(destinations) != 22 {
		t.Fatalf("runtime catalog counts = depots %d, areas %d, destinations %d", len(depots), len(areas), len(destinations))
	}
	matchedArea, _, err := resolveArea("武陵城", areas)
	if err != nil {
		t.Fatalf("resolveArea() error = %v", err)
	}
	if matchedArea.ID != "WulingCity" {
		t.Fatalf("resolveArea() area = %q, want WulingCity", matchedArea.ID)
	}
}

func TestBuildDepotNavigationOverrideSelectsGeneratedSubTask(t *testing.T) {
	t.Parallel()

	route := depot{
		RouteNode:      "AutoDeliveryRouteDepotDefault",
		ZipRouteNode:   "AutoDeliveryRouteDepotDefaultWithZipline",
		RetryRouteNode: "AutoDeliveryRouteDepotRetryDefault",
	}
	override := buildDepotNavigationOverride(route, true)
	walkNode := override[navigateDepotNode].(map[string]any)
	if walkNode["custom_action"] != "SubTask" {
		t.Fatalf("unexpected depot dispatcher: %#v", walkNode)
	}
	if got := navigationParam(t, override, navigateDepotNode)["sub"]; !reflect.DeepEqual(got, []string{route.ZipRouteNode}) {
		t.Fatalf("depot dispatcher sub = %#v", got)
	}
	retryNode := override[retryNavigateDepotNode].(map[string]any)
	if retryNode["enabled"] != true || retryNode["custom_action"] != "SubTask" {
		t.Fatalf("unexpected retry dispatcher: %#v", retryNode)
	}
	if got := navigationParam(t, override, retryNavigateDepotNode)["sub"]; !reflect.DeepEqual(got, []string{route.RetryRouteNode}) {
		t.Fatalf("retry dispatcher sub = %#v", got)
	}
}

func TestBuildDepotNavigationOverrideDisablesMissingRetryRoute(t *testing.T) {
	t.Parallel()

	route := depot{RouteNode: "normal", ZipRouteNode: "zip"}
	override := buildDepotNavigationOverride(route, false)
	if got := navigationParam(t, override, navigateDepotNode)["sub"]; !reflect.DeepEqual(got, []string{"normal"}) {
		t.Fatalf("depot dispatcher sub = %#v", got)
	}
	retryNode := override[retryNavigateDepotNode].(map[string]any)
	if retryNode["enabled"] != false {
		t.Fatalf("unexpected retry dispatcher: %#v", retryNode)
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
