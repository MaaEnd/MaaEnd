// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"math"

	"github.com/rs/zerolog"
)

/* ******** Point ******** */

type Point struct {
	X float64
	Y float64
}

func (p Point) Clone() Point {
	return Point{X: p.X, Y: p.Y}
}

// MarshalZerologObject serializes the point as zerolog sub-fields X and Y.
func (p Point) MarshalZerologObject(e *zerolog.Event) {
	e.Float64("X", p.X).Float64("Y", p.Y)
}

func (p Point) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{p.X, p.Y})
}

func (p *Point) UnmarshalJSON(data []byte) error {
	var arr [2]float64
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}

	p.X = arr[0]
	p.Y = arr[1]
	return nil
}

func (p Point) IntX() int {
	return int(math.Round(p.X))
}

func (p Point) IntY() int {
	return int(math.Round(p.Y))
}

func (p Point) IsNaN() bool {
	return math.IsNaN(p.X) || math.IsNaN(p.Y)
}

func (p Point) IsInf() bool {
	return math.IsInf(p.X, 0) || math.IsInf(p.Y, 0)
}

func (p Point) IsValid() bool {
	return !p.IsNaN() && !p.IsInf()
}

// InRect reports whether p lies within the axis-aligned rectangle [x, x+w) x [y, y+h).
func (p Point) InRect(x, y, w, h float64) bool {
	return p.X >= x && p.X < x+w && p.Y >= y && p.Y < y+h
}

// DistanceTo returns the Euclidean distance from this point to another point.
func (p Point) DistanceTo(other Point) float64 {
	return math.Hypot(p.X-other.X, p.Y-other.Y)
}

// ManhattanDistanceTo returns the Manhattan distance from this point to another point.
func (p Point) ManhattanDistanceTo(other Point) float64 {
	return math.Abs(p.X-other.X) + math.Abs(p.Y-other.Y)
}

// AngleTo returns the angle in degrees [0, 360) from this point to another point,
// where 0° is up (negative Y direction), and angles increase clockwise.
func (p Point) AngleTo(other Point) float64 {
	dx := other.X - p.X
	dy := other.Y - p.Y

	// 0° is up (-Y), increasing clockwise
	angle := math.Atan2(dx, -dy) * 180 / math.Pi

	// Normalize to [0, 360)
	angle = math.Mod(angle+360, 360)

	return angle
}

/* ******** Linear Transformation ******** */

type LinearTransform struct {
	ScaleX  float64
	ScaleY  float64
	OffsetX float64
	OffsetY float64
}

func (lt LinearTransform) Apply(p Point) Point {
	return Point{
		X: p.X*lt.ScaleX + lt.OffsetX,
		Y: p.Y*lt.ScaleY + lt.OffsetY,
	}
}

func (lt LinearTransform) Inverse(p Point) Point {
	return Point{
		X: (p.X - lt.OffsetX) / lt.ScaleX,
		Y: (p.Y - lt.OffsetY) / lt.ScaleY,
	}
}

/* ******** Misc ******** */

// PathBounds returns the min and max X/Y of all valid points in path.
// Returns (+Inf, +Inf, -Inf, -Inf) when no valid points are present.
func PathBounds(path []Point) (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range path {
		if !p.IsValid() {
			continue
		}
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	}
	return
}

// PathTotalDistance returns the cumulative Euclidean distance along a coordinate path.
func PathTotalDistance(path []Point) float64 {
	distance := 0.0
	for i := 1; i < len(path); i++ {
		distance += path[i].DistanceTo(path[i-1])
	}
	return distance
}

// DeltaRotation returns the minimum signed angle difference from current to target in [-180, 180].
func DeltaRotation(current, target int) int {
	diff := target - current
	for diff > 180 {
		diff -= 360
	}
	for diff < -180 {
		diff += 360
	}
	return diff
}

/* ******** Graph searching algorithms ******** */

type algoEdge struct {
	to   int
	cost float64
}

func dijkstraPath(adjacency map[int][]algoEdge, startID, targetID int) ([]int, error) {
	open := &dijkstraPriorityQueue{}
	heap.Init(open)
	heap.Push(open, dijkstraQueueItem{id: startID, priority: 0})

	cameFrom := map[int]int{}
	gScore := map[int]float64{startID: 0}
	closed := map[int]bool{}

	for open.Len() > 0 {
		current := heap.Pop(open).(dijkstraQueueItem).id
		if closed[current] {
			continue
		}
		if current == targetID {
			return reconstructDijkstraPath(cameFrom, current), nil
		}
		closed[current] = true

		for _, edge := range adjacency[current] {
			if closed[edge.to] {
				continue
			}
			tentativeG := gScore[current] + edge.cost
			oldG, ok := gScore[edge.to]
			if ok && tentativeG >= oldG {
				continue
			}
			cameFrom[edge.to] = current
			gScore[edge.to] = tentativeG
			heap.Push(open, dijkstraQueueItem{id: edge.to, priority: tentativeG})
		}
	}

	return nil, fmt.Errorf("dijkstra path not found")
}

func reconstructDijkstraPath(cameFrom map[int]int, current int) []int {
	path := []int{current}
	for {
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		path = append(path, prev)
		current = prev
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

type dijkstraQueueItem struct {
	id       int
	priority float64
}

type dijkstraPriorityQueue []dijkstraQueueItem

func (q dijkstraPriorityQueue) Len() int { return len(q) }

func (q dijkstraPriorityQueue) Less(i, j int) bool {
	if math.Abs(q[i].priority-q[j].priority) < 1e-9 {
		return q[i].id < q[j].id
	}
	return q[i].priority < q[j].priority
}

func (q dijkstraPriorityQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *dijkstraPriorityQueue) Push(x any) {
	*q = append(*q, x.(dijkstraQueueItem))
}

func (q *dijkstraPriorityQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	*q = old[:len(old)-1]
	return item
}
