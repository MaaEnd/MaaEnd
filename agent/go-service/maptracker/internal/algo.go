// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"container/heap"
	"fmt"
	"math"
)

/* ******** Reusable utilities ******** */

// PathTotalDistance returns the cumulative Euclidean distance along a coordinate path.
func PathTotalDistance(path [][2]float64) float64 {
	distance := 0.0
	for i := 1; i < len(path); i++ {
		distance += math.Hypot(path[i][0]-path[i-1][0], path[i][1]-path[i-1][1])
	}
	return distance
}

/* ******** Graph searching algorithms ******** */

type algoPoint struct {
	x float64
	y float64
}

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
