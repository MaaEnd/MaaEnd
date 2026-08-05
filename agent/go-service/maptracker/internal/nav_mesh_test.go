// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testNavMeshText = `[MapTrackerNavMesh.Meta]
Version=1
Encoding=UTF-8
Name=map_test_lv001
Description=Test navmesh
MapRegionName=map_test
MapLevelName=lv001
GeoWidth=100.0
GeoHeight=100.0

[MapTrackerNavMesh.Vertices]
V1=X0,Y0,T0,E100,F()
V2=X10,Y0,T0,E200,F(C)
V3=X20,Y0,T0,E300,F(H)

[MapTrackerNavMesh.Edges]
E1=S1,D2,B1,C10,F()
E2=S2,D3,B1,C10,F()
`

func TestParseNavMesh(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	if mesh.Meta.Name != "map_test_lv001" {
		t.Fatalf("Meta.Name = %q", mesh.Meta.Name)
	}
	if len(mesh.Vertices) != 3 {
		t.Fatalf("len(Vertices) = %d", len(mesh.Vertices))
	}
	vertex, ok := mesh.FindVertexByEntityID(200)
	if !ok {
		t.Fatalf("FindVertexByEntityID(200) not found")
	}
	if vertex.ID != 2 || vertex.Flags&NavMeshVertexFlagCollectable == 0 {
		t.Fatalf("unexpected entity vertex: %+v", vertex)
	}
}

func TestFindPath(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	path, err := findTemporaryPath(mesh, Point{X: -1, Y: 0}, Point{X: 11, Y: 0})
	if err != nil {
		t.Fatalf("FindPath() error = %v", err)
	}
	if len(path) < 3 {
		t.Fatalf("len(path) = %d, path = %+v", len(path), path)
	}
	if path[0] != (Point{X: -1, Y: 0}) {
		t.Fatalf("path[0] = %+v", path[0])
	}
	if path[len(path)-1] != (Point{X: 11, Y: 0}) {
		t.Fatalf("path last = %+v", path[len(path)-1])
	}
}

func TestFindPathUsesTemporaryVertexConnection(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	path, err := findTemporaryPath(mesh, Point{X: 0, Y: 0}, Point{X: 5, Y: 5})
	if err != nil {
		t.Fatalf("FindPath() error = %v", err)
	}
	assertNavMeshPath(t, path, []Point{{X: 0, Y: 0}, {X: 5, Y: 5}})
}

func TestFindPathRejectsFarConnectPoints(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	if _, err := findTemporaryPath(mesh, Point{X: -100, Y: 0}, Point{X: 11, Y: 0}); err == nil {
		t.Fatalf("FindPath() error = nil")
	}
}

func TestTemporaryVertexIDsAreNegative(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	firstID, first := mesh.AddTemporaryVertex(Point{X: 1, Y: 2}, 1.05, 20)
	secondID, second := mesh.AddTemporaryVertex(Point{X: 3, Y: 4}, 1.05, 20)
	if firstID != -1 || first.ID != -1 || secondID != -2 || second.ID != -2 {
		t.Fatalf("unexpected temporary vertices: firstID=%d first=%+v secondID=%d second=%+v", firstID, first, secondID, second)
	}
	mesh.ClearTemporaryVertex()
	if len(mesh.TemporaryVertices) != 0 {
		t.Fatalf("TemporaryVertices not cleared: %+v", mesh.TemporaryVertices)
	}
}

func TestPathConnectPlansSupportThreeTemporaryVertices(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	mesh.AddTemporaryVertex(Point{X: 0, Y: 0}, 1.05, 20)
	mesh.AddTemporaryVertex(Point{X: 10, Y: 0}, 1.05, 20)
	mesh.AddTemporaryVertex(Point{X: 11, Y: 0}, 1.05, 20)
	plans := mesh.pathConnectPlans()
	if len(plans) == 0 {
		t.Fatalf("pathConnectPlans() returned no plans")
	}
	if len(plans[0].choices) != 3 {
		t.Fatalf("len(choices) = %d, choices = %+v", len(plans[0].choices), plans[0].choices)
	}
}

func TestTemporaryEdgeCostUsesMaxCostFactorBetweenTemporaryVertices(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	leftID, _ := mesh.AddTemporaryVertex(Point{X: 0, Y: 0}, 1.05, 20)
	rightID, _ := mesh.AddTemporaryVertex(Point{X: 3, Y: 4}, 2.0, 20)
	if cost := mesh.temporaryEdgeCost(leftID, rightID); cost != 10 {
		t.Fatalf("temporaryEdgeCost() = %v", cost)
	}
}

func TestRuntimeZiplineEdgesAffectPath(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(`[MapTrackerNavMesh.Meta]
Version=1
Encoding=UTF-8
Name=map_test_lv001
Description=Test navmesh
MapRegionName=map_test
MapLevelName=lv001
GeoWidth=100.0
GeoHeight=100.0

[MapTrackerNavMesh.Vertices]
V1=X0,Y0,T0,E0,F()
V2=X10,Y0,T0,E0,F()
V3=X20,Y0,T0,E0,F()
V4=X30,Y0,T0,E0,F()

[MapTrackerNavMesh.Edges]
E1=S1,D2,B1,C10,F()
E2=S2,D3,B1,C10,F()
E3=S3,D4,B1,C10,F()
`))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	leftID, _ := mesh.AddRuntimeVertex(Point{X: 0, Y: 0}, 0, 0, NavMeshVertexFlagZipline)
	rightID, _ := mesh.AddRuntimeVertex(Point{X: 30, Y: 0}, 0, 0, NavMeshVertexFlagZipline)
	mesh.AddRuntimeEdge(-100, leftID, 1, true, 0.1, 0)
	mesh.AddRuntimeEdge(-101, rightID, 4, true, 0.1, 0)
	mesh.AddRuntimeEdge(-102, leftID, rightID, true, 1, NavMeshEdgeFlagZipline)

	pathIDs, err := mesh.FindPathIDs(1, 4)
	if err != nil {
		t.Fatalf("FindPathIDs() error = %v", err)
	}
	if !containsID(pathIDs, leftID) || !containsID(pathIDs, rightID) {
		t.Fatalf("pathIDs = %+v, want runtime zipline vertices %d and %d", pathIDs, leftID, rightID)
	}

	mesh.DisableEdge(-102)
	pathIDs, err = mesh.FindPathIDs(1, 4)
	if err != nil {
		t.Fatalf("FindPathIDs() after DisableEdge error = %v", err)
	}
	if containsID(pathIDs, leftID) || containsID(pathIDs, rightID) {
		t.Fatalf("pathIDs = %+v, disabled zipline edge should not be used", pathIDs)
	}
}

func TestDisableRuntimeZiplineVertex(t *testing.T) {
	mesh, err := ParseNavMesh(strings.NewReader(testNavMeshText))
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	ziplineID, _ := mesh.AddRuntimeVertex(Point{X: 5, Y: 0}, 0, 0, NavMeshVertexFlagZipline)
	mesh.AddRuntimeEdge(-100, 1, ziplineID, true, 1, 0)
	mesh.AddRuntimeEdge(-101, ziplineID, 2, true, 1, 0)
	mesh.DisableVertex(ziplineID)
	pathIDs, err := mesh.FindPathIDs(1, 2)
	if err != nil {
		t.Fatalf("FindPathIDs() error = %v", err)
	}
	if containsID(pathIDs, ziplineID) {
		t.Fatalf("pathIDs = %+v, disabled runtime vertex should not be used", pathIDs)
	}
}

func TestParseNavMeshRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "duplicate section",
			text: strings.Replace(testNavMeshText, "[MapTrackerNavMesh.Vertices]", "[MapTrackerNavMesh.Meta]", 1),
		},
		{
			name: "unknown vertex flag",
			text: strings.Replace(testNavMeshText, "F(C)", "F(Z)", 1),
		},
		{
			name: "missing edge source",
			text: strings.Replace(testNavMeshText, "E1=S1,D2", "E1=S99,D2", 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseNavMesh(strings.NewReader(tt.text)); err == nil {
				t.Fatalf("ParseNavMesh() error = nil")
			}
		})
	}
}

func findTemporaryPath(mesh *NavMesh, start, target Point) ([]Point, error) {
	mesh.ClearTemporaryVertex()
	startID, _ := mesh.AddTemporaryVertex(start, 1.05, 20)
	targetID, _ := mesh.AddTemporaryVertex(target, 1.05, 20)
	return mesh.FindPath(startID, targetID)
}

func assertNavMeshPath(t *testing.T, actual []Point, expected []Point) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("len(path) = %d, path = %+v, expected = %+v", len(actual), actual, expected)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("path = %+v, expected = %+v", actual, expected)
		}
	}
}

func containsID(ids []int, target int) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func TestParseRealNavMesh(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "..", "assets", "data", "MapTrackerNavMesh", "map02_lv002.mtnm"))
	if err != nil {
		t.Fatalf("Open real NavMesh file error = %v", err)
	}
	defer func() { _ = file.Close() }()

	mesh, err := ParseNavMesh(file)
	if err != nil {
		t.Fatalf("ParseNavMesh() error = %v", err)
	}
	if mesh.Meta.Name != "map02_lv002" {
		t.Fatalf("Meta.Name = %q", mesh.Meta.Name)
	}
	if len(mesh.Vertices) == 0 {
		t.Fatalf("real navmesh has no vertices")
	}
	if len(mesh.Edges) == 0 {
		t.Fatalf("real navmesh has no edges")
	}
	vertex, ok := mesh.FindVertexByEntityID(22800173539)
	if !ok {
		t.Fatalf("known entity vertex not found")
	}
	path, err := findTemporaryPath(mesh, vertex.Pos, Point{X: vertex.Pos.X + 1, Y: vertex.Pos.Y + 1})
	if err != nil {
		t.Fatalf("FindPath() on real navmesh error = %v", err)
	}
	if len(path) == 0 {
		t.Fatalf("FindPath() returned empty path")
	}
}
