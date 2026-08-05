// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const benchmarkMap02Lv002PairCount = 64

type benchmarkPathPair struct {
	startID  int
	targetID int
}

var benchmarkPathLengthSink int

func TestLerp(t *testing.T) {
	if got := Lerp(10, 20, 0); got != 10 {
		t.Errorf("Lerp(10, 20, 0) = %v, want 10", got)
	}
	if got := Lerp(10, 20, 1); got != 20 {
		t.Errorf("Lerp(10, 20, 1) = %v, want 20", got)
	}
	if got := Lerp(10, 20, 0.5); math.Abs(got-15) > 1e-12 {
		t.Errorf("Lerp(10, 20, 0.5) = %v, want 15", got)
	}
}

func TestUnitRamp(t *testing.T) {
	cases := []struct {
		x, lo, hi, want float64
	}{
		{0, 30, 60, 0},
		{30, 30, 60, 0},
		{45, 30, 60, 0.5},
		{60, 30, 60, 1},
		{90, 30, 60, 1},
		{10, 10, 10, 0},
		{11, 10, 10, 1},
	}
	for _, tc := range cases {
		if got := UnitRamp(tc.x, tc.lo, tc.hi); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("UnitRamp(%v, %v, %v) = %v, want %v", tc.x, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func TestRotateToLocalFrame(t *testing.T) {
	// A delta pointing north (negative Y) in the map frame.
	north := Point{X: 0, Y: -10}

	cases := []struct {
		name        string
		delta       Point
		headingDeg  float64
		wantForward float64
		wantRight   float64
	}{
		{"north delta facing north", north, 0, 10, 0},
		{"north delta facing east", north, 90, 0, -10},
		{"north delta facing south", north, 180, -10, 0},
		{"north delta facing west", north, 270, 0, 10},
		{"east delta facing north", Point{X: 10, Y: 0}, 0, 0, 10},
		{"south delta facing north", Point{X: 0, Y: 10}, 0, -10, 0},
		{"north delta facing northeast", north, 45, 7.0710678, -7.0710678},
		{"diagonal delta facing northeast", Point{X: 10, Y: -10}, 45, 14.1421356, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forward, right := RotateToLocalFrame(tc.delta, tc.headingDeg)
			if math.Abs(forward-tc.wantForward) > 1e-6 {
				t.Errorf("forward = %v, want %v", forward, tc.wantForward)
			}
			if math.Abs(right-tc.wantRight) > 1e-6 {
				t.Errorf("right = %v, want %v", right, tc.wantRight)
			}
		})
	}
}

func TestRotateToLocalFramePreservesMagnitudeAndInvertsAngleTo(t *testing.T) {
	origin := Point{X: 100, Y: 200}
	target := Point{X: 137.5, Y: 163.25}
	delta := Point{X: target.X - origin.X, Y: target.Y - origin.Y}

	// Facing straight at the target must yield a purely forward component.
	heading := origin.AngleTo(target)
	forward, right := RotateToLocalFrame(delta, heading)
	if math.Abs(forward-origin.DistanceTo(target)) > 1e-6 {
		t.Errorf("forward = %v, want %v", forward, origin.DistanceTo(target))
	}
	if math.Abs(right) > 1e-6 {
		t.Errorf("right = %v, want 0", right)
	}

	// Any heading must preserve the length of the delta.
	for _, heading := range []float64{0, 23.5, 90, 187.25, 359} {
		forward, right := RotateToLocalFrame(delta, heading)
		if got, want := math.Hypot(forward, right), math.Hypot(delta.X, delta.Y); math.Abs(got-want) > 1e-6 {
			t.Errorf("heading %v: magnitude = %v, want %v", heading, got, want)
		}
	}
}

func TestDijkstraPathChoosesLowerCostPath(t *testing.T) {
	adjacency := map[int][]algoEdge{
		1: {{to: 2, cost: 1}, {to: 3, cost: 10}},
		2: {{to: 4, cost: 1}},
		3: {{to: 4, cost: 1}},
	}

	path, err := dijkstraPath(adjacency, 1, 4)
	if err != nil {
		t.Fatalf("dijkstraPath() error = %v", err)
	}
	assertDijkstraPath(t, path, []int{1, 2, 4})
}

func TestDijkstraPathRejectsUnreachableTarget(t *testing.T) {
	adjacency := map[int][]algoEdge{
		1: {{to: 2, cost: 1}},
	}

	if _, err := dijkstraPath(adjacency, 1, 3); err == nil {
		t.Fatalf("dijkstraPath() error = nil")
	}
}

func TestDijkstraPathBreaksPriorityTieByID(t *testing.T) {
	adjacency := map[int][]algoEdge{
		1: {{to: 3, cost: 1}, {to: 2, cost: 1}},
		2: {{to: 4, cost: 1}},
		3: {{to: 4, cost: 1}},
	}

	path, err := dijkstraPath(adjacency, 1, 4)
	if err != nil {
		t.Fatalf("dijkstraPath() error = %v", err)
	}
	assertDijkstraPath(t, path, []int{1, 2, 4})
}

func BenchmarkMap02Lv002DijkstraPath(b *testing.B) {
	points, adjacency := loadBenchmarkMap02Lv002Graph(b)
	pairs := benchmarkReachablePathPairs(b, points, adjacency, benchmarkMap02Lv002PairCount)
	benchmarkDijkstraPathPairs(b, adjacency, pairs)
}

func assertDijkstraPath(t *testing.T, actual []int, expected []int) {
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

func benchmarkDijkstraPathPairs(b *testing.B, adjacency map[int][]algoEdge, pairs []benchmarkPathPair) {
	b.Helper()
	b.ReportAllocs()
	b.ReportMetric(float64(len(pairs)), "paths/op")
	b.ResetTimer()

	totalPathLen := 0
	for i := 0; i < b.N; i++ {
		for _, pair := range pairs {
			path, err := dijkstraPath(adjacency, pair.startID, pair.targetID)
			if err != nil {
				b.Fatalf("dijkstraPath(%d, %d) error = %v", pair.startID, pair.targetID, err)
			}
			totalPathLen += len(path)
		}
	}
	benchmarkPathLengthSink = totalPathLen
	if b.N > 0 && len(pairs) > 0 {
		b.ReportMetric(float64(totalPathLen)/(float64(b.N)*float64(len(pairs))), "vertices/path")
	}
}

func loadBenchmarkMap02Lv002Graph(tb testing.TB) (map[int]Point, map[int][]algoEdge) {
	tb.Helper()
	file, err := os.Open(filepath.Join("..", "..", "..", "..", "assets", "data", "MapTrackerNavMesh", "map02_lv002.mtnm"))
	if err != nil {
		tb.Fatalf("Open real NavMesh file error = %v", err)
	}
	defer func() { _ = file.Close() }()

	mesh, err := ParseNavMesh(file)
	if err != nil {
		tb.Fatalf("ParseNavMesh() error = %v", err)
	}
	return mesh.buildPathGraph()
}

func benchmarkReachablePathPairs(tb testing.TB, points map[int]Point, adjacency map[int][]algoEdge, count int) []benchmarkPathPair {
	tb.Helper()
	ids := make([]int, 0, len(points))
	for id := range points {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) < 2 {
		tb.Fatalf("benchmark graph has only %d vertex", len(ids))
	}

	pairs := make([]benchmarkPathPair, 0, count)
	seen := map[benchmarkPathPair]bool{}
	maxAttempts := len(ids) * len(ids)
	for attempt := 0; len(pairs) < count && attempt < maxAttempts; attempt++ {
		startID := ids[(attempt*37+11)%len(ids)]
		targetID := ids[len(ids)-1-((attempt*53+7)%len(ids))]
		if startID == targetID {
			continue
		}
		pair := benchmarkPathPair{startID: startID, targetID: targetID}
		if seen[pair] {
			continue
		}
		if _, err := dijkstraPath(adjacency, startID, targetID); err != nil {
			continue
		}
		seen[pair] = true
		pairs = append(pairs, pair)
	}
	if len(pairs) < count {
		tb.Fatalf("only found %d reachable benchmark pairs, want %d", len(pairs), count)
	}
	return pairs
}
