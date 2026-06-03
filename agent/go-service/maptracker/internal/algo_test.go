// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import "testing"

func TestAstarPathChoosesLowerCostPath(t *testing.T) {
	points := map[int]astarPoint{
		1: {x: 0, y: 0},
		2: {x: 1, y: 0},
		3: {x: 0, y: 1},
		4: {x: 2, y: 0},
	}
	adjacency := map[int][]astarEdge{
		1: {{to: 2, cost: 1}, {to: 3, cost: 10}},
		2: {{to: 4, cost: 1}},
		3: {{to: 4, cost: 1}},
	}

	path, err := astarPath(points, adjacency, 1, 4)
	if err != nil {
		t.Fatalf("astarPath() error = %v", err)
	}
	assertAstarPath(t, path, []int{1, 2, 4})
}

func TestAstarPathRejectsUnreachableTarget(t *testing.T) {
	points := map[int]astarPoint{
		1: {x: 0, y: 0},
		2: {x: 1, y: 0},
		3: {x: 2, y: 0},
	}
	adjacency := map[int][]astarEdge{
		1: {{to: 2, cost: 1}},
	}

	if _, err := astarPath(points, adjacency, 1, 3); err == nil {
		t.Fatalf("astarPath() error = nil")
	}
}

func TestAstarPathBreaksPriorityTieByID(t *testing.T) {
	points := map[int]astarPoint{
		1: {x: 0, y: 0},
		2: {x: 1, y: 0},
		3: {x: 0, y: 1},
		4: {x: 2, y: 0},
	}
	adjacency := map[int][]astarEdge{
		1: {{to: 3, cost: 1}, {to: 2, cost: 1}},
		2: {{to: 4, cost: 1}},
		3: {{to: 4, cost: 1}},
	}

	path, err := astarPath(points, adjacency, 1, 4)
	if err != nil {
		t.Fatalf("astarPath() error = %v", err)
	}
	assertAstarPath(t, path, []int{1, 2, 4})
}

func assertAstarPath(t *testing.T, actual []int, expected []int) {
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
