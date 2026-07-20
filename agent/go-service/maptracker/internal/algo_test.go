// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
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
