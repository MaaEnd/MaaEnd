// Copyright (c) 2026 Harry Huang
package maptrackerinternal

import (
	"container/heap"
	"fmt"
	"math"
)

type astarPoint struct {
	x float64
	y float64
}

type astarEdge struct {
	to   int
	cost float64
}

func astarPath(points map[int]astarPoint, adjacency map[int][]astarEdge, startID, targetID int) ([]int, error) {
	open := &astarPriorityQueue{}
	heap.Init(open)
	heap.Push(open, astarQueueItem{id: startID, priority: 0})

	cameFrom := map[int]int{}
	gScore := map[int]float64{startID: 0}
	closed := map[int]bool{}

	for open.Len() > 0 {
		current := heap.Pop(open).(astarQueueItem).id
		if closed[current] {
			continue
		}
		if current == targetID {
			return reconstructAstarPath(cameFrom, current), nil
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
			priority := tentativeG + astarDistance(points[edge.to], points[targetID])
			heap.Push(open, astarQueueItem{id: edge.to, priority: priority})
		}
	}

	return nil, fmt.Errorf("astar path not found")
}

func reconstructAstarPath(cameFrom map[int]int, current int) []int {
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

func astarDistance(a, b astarPoint) float64 {
	return math.Hypot(a.x-b.x, a.y-b.y)
}

type astarQueueItem struct {
	id       int
	priority float64
}

type astarPriorityQueue []astarQueueItem

func (q astarPriorityQueue) Len() int { return len(q) }

func (q astarPriorityQueue) Less(i, j int) bool {
	if math.Abs(q[i].priority-q[j].priority) < 1e-9 {
		return q[i].id < q[j].id
	}
	return q[i].priority < q[j].priority
}

func (q astarPriorityQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *astarPriorityQueue) Push(x any) {
	*q = append(*q, x.(astarQueueItem))
}

func (q *astarPriorityQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	*q = old[:len(old)-1]
	return item
}
