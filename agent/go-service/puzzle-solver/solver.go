// Copyright (c) 2026 Harry Huang
package puzzle

import (
	"errors"
	"sort"
)

// Placement represents a settled position for one puzzle piece
type Placement struct {
	MachineX    int // X coordinate (grid index)
	MachineY    int // Y coordinate (grid index)
	Rotation    int // 0, 1, 2, 3 (CCW * 90)
	PuzzleIndex int // Index of the puzzle in the input list, can ignore since output is in order
}

type Puzzle struct {
	Index    int
	Color    int
	Blocks   [][2]int
	Rotation int
}

func (p *Puzzle) getAllDerivatives() []*Puzzle {
	drv := make([]*Puzzle, 4)
	// 0 deg
	drv[0] = &Puzzle{Index: p.Index, Color: p.Color, Blocks: p.Blocks, Rotation: 0}

	// 90 deg CCW: (x, y) -> (y, -x)
	b90 := make([][2]int, len(p.Blocks))
	for i, b := range p.Blocks {
		b90[i] = [2]int{b[1], -b[0]}
	}
	drv[1] = &Puzzle{Index: p.Index, Color: p.Color, Blocks: b90, Rotation: 1}

	// 180 deg CCW: (x, y) -> (-x, -y)
	b180 := make([][2]int, len(p.Blocks))
	for i, b := range p.Blocks {
		b180[i] = [2]int{-b[0], -b[1]}
	}
	drv[2] = &Puzzle{Index: p.Index, Color: p.Color, Blocks: b180, Rotation: 2}

	// 270 deg CCW: (x, y) -> (-y, x)
	b270 := make([][2]int, len(p.Blocks))
	for i, b := range p.Blocks {
		b270[i] = [2]int{-b[1], b[0]}
	}
	drv[3] = &Puzzle{Index: p.Index, Color: p.Color, Blocks: b270, Rotation: 3}

	return drv
}

func (p *Puzzle) convertFromPuzzleDesc(i int, pd *PuzzleDesc, hueMap map[int]int) {
	p.Index = i
	// Find color index from HueList
	if idx, ok := hueMap[pd.Hue]; ok {
		p.Color = idx
	} else {
		// Try to find nearest cluster
		minDiff := 1000
		bestIdx := 0
		for h, idx := range hueMap {
			diff := diffHue(h, pd.Hue)
			if diff < minDiff {
				minDiff = diff
				bestIdx = idx
			}
		}
		p.Color = bestIdx
	}
	p.Blocks = pd.Blocks
	p.Rotation = 0
}

type Board struct {
	XSize       int
	YSize       int
	K           int
	XProj       [][]int
	YProj       [][]int
	Grid        [][]int
	CurrXCounts [][]int
	CurrYCounts [][]int
}

func (b *Board) convertFromBoardDesc(bd *BoardDesc) error {
	// 1. Determine Board Dimensions (Max Extents)
	// The problem requires a fixed rectangular grid.
	// Since individual hue projections might be smaller (if blocks are missing on edge),
	// we take the maximum extent observed across all hue projections.
	maxExtX := 0
	maxExtY := 0
	for _, pd := range bd.ProjDescList {
		if pd.ExtX > maxExtX {
			maxExtX = pd.ExtX
		}
		if pd.ExtY > maxExtY {
			maxExtY = pd.ExtY
		}
	}

	b.XSize = 2*maxExtX + 1
	b.YSize = 2*maxExtY + 1
	b.K = len(bd.HueList)

	// 2. Initialize Projections
	// Map ProjDescList (by hue index) to XProj/YProj
	// Align projections to the center.
	// pd.XProjList is centered at pd.ExtX. b.XProj is centered at maxExtX.
	b.XProj = make([][]int, b.K)
	b.YProj = make([][]int, b.K)

	for i, pd := range bd.ProjDescList {
		// X Project
		b.XProj[i] = make([]int, b.XSize)
		// Offset: The center of ProjDesc (index ExtX) should map to center of Board (index maxExtX)
		// Shift = maxExtX - pd.ExtX
		shiftX := maxExtX - pd.ExtX
		for j, val := range pd.XProjList {
			targetIdx := j + shiftX
			if targetIdx >= 0 && targetIdx < b.XSize {
				b.XProj[i][targetIdx] = val
			}
		}

		// Y Project
		b.YProj[i] = make([]int, b.YSize)
		shiftY := maxExtY - pd.ExtY
		for j, val := range pd.YProjList {
			targetIdx := j + shiftY
			if targetIdx >= 0 && targetIdx < b.YSize {
				b.YProj[i][targetIdx] = val
			}
		}
	}

	// 3. Initialize Grid
	b.Grid = make([][]int, b.YSize)
	for y := 0; y < b.YSize; y++ {
		b.Grid[y] = make([]int, b.XSize)
		for x := 0; x < b.XSize; x++ {
			b.Grid[y][x] = -1 // -1 means an empty block
		}
	}
	b.CurrXCounts = make([][]int, b.K)
	for i := range b.CurrXCounts {
		b.CurrXCounts[i] = make([]int, b.XSize)
	}
	b.CurrYCounts = make([][]int, b.K)
	for i := range b.CurrYCounts {
		b.CurrYCounts[i] = make([]int, b.YSize)
	}

	// 4. Fill Banned Blocks
	// Banned blocks are stored relative to refProj (which should match our maxExt or close to it)
	var refProj ProjDesc
	for _, pd := range bd.ProjDescList {
		if pd.ExtX+pd.ExtY > refProj.ExtX+refProj.ExtY {
			refProj = pd
		}
	}

	bannedShiftX := maxExtX - refProj.ExtX
	bannedShiftY := maxExtY - refProj.ExtY

	for _, bb := range bd.BannedBlockList {
		nx := bb.Loc[0] + bannedShiftX
		ny := bb.Loc[1] + bannedShiftY
		if nx >= 0 && nx < b.XSize && ny >= 0 && ny < b.YSize {
			b.Grid[ny][nx] = -2 // -2 means a banned block
		}
	}

	// 5. Fill Locked Blocks
	// LockedBlockList is [hueIndex][blockIndex].
	// Each block has Loc (relative to that hue's ExtX/ExtY).
	// So each hue needs its own shift.
	for hIdx, blocks := range bd.LockedBlockList {
		hbExtX := bd.ProjDescList[hIdx].ExtX
		hbExtY := bd.ProjDescList[hIdx].ExtY
		shiftX := maxExtX - hbExtX
		shiftY := maxExtY - hbExtY

		for _, lb := range blocks {
			nx := lb.Loc[0] + shiftX
			ny := lb.Loc[1] + shiftY

			if nx >= 0 && nx < b.XSize && ny >= 0 && ny < b.YSize {
				b.Grid[ny][nx] = hIdx // Locked block of color hIdx
				b.CurrXCounts[hIdx][nx]++
				b.CurrYCounts[hIdx][ny]++
			}
		}
	}

	return nil
}

func (b *Board) canPlace(p *Puzzle, cx, cy int) bool {
	// current counts refs
	cxc := b.CurrXCounts[p.Color]
	cyc := b.CurrYCounts[p.Color]
	xp := b.XProj[p.Color]
	yp := b.YProj[p.Color]

	deltaX := make(map[int]int)
	deltaY := make(map[int]int)

	for _, block := range p.Blocks {
		nx, ny := cx+block[0], cy+block[1]

		if nx < 0 || nx >= b.XSize || ny < 0 || ny >= b.YSize {
			return false
		}
		if b.Grid[ny][nx] != -1 {
			return false
		}

		deltaX[nx]++
		deltaY[ny]++
	}

	for x, count := range deltaX {
		if cxc[x]+count > xp[x] {
			return false
		}
	}
	for y, count := range deltaY {
		if cyc[y]+count > yp[y] {
			return false
		}
	}
	return true
}

func (b *Board) place(p *Puzzle, cx, cy int) {
	for _, block := range p.Blocks {
		nx, ny := cx+block[0], cy+block[1]
		b.Grid[ny][nx] = p.Color
		b.CurrXCounts[p.Color][nx]++
		b.CurrYCounts[p.Color][ny]++
	}
}

func (b *Board) remove(p *Puzzle, cx, cy int) {
	for _, block := range p.Blocks {
		nx, ny := cx+block[0], cy+block[1]
		b.Grid[ny][nx] = -1
		b.CurrXCounts[p.Color][nx]--
		b.CurrYCounts[p.Color][ny]--
	}
}

func (b *Board) solveWith(puzzles []*Puzzle) ([]Placement, bool) {
	// Sort puzzles by size (descending)
	type IndexedPuzzle struct {
		OriginalIndex int
		Pz            *Puzzle
	}
	indexed := make([]IndexedPuzzle, len(puzzles))
	for i, p := range puzzles {
		indexed[i] = IndexedPuzzle{OriginalIndex: i, Pz: p}
	}
	sort.Slice(indexed, func(i, j int) bool {
		return len(indexed[i].Pz.Blocks) > len(indexed[j].Pz.Blocks)
	})

	solutionMap := make(map[int]Placement)

	var backtrack func(idx int) bool
	backtrack = func(idxInSorted int) bool {
		if idxInSorted == len(indexed) {
			return true
		}

		currentItem := indexed[idxInSorted]
		originalIdx := currentItem.OriginalIndex
		rawPuzzle := currentItem.Pz
		derivatives := rawPuzzle.getAllDerivatives()

		// Try to place core block at every empty cell
		for y := 0; y < b.YSize; y++ {
			for x := 0; x < b.XSize; x++ {
				if b.Grid[y][x] != -1 {
					continue
				}

				for _, deriv := range derivatives {
					if b.canPlace(deriv, x, y) {
						b.place(deriv, x, y)
						solutionMap[originalIdx] = Placement{
							MachineX:    x,
							MachineY:    y,
							Rotation:    deriv.Rotation,
							PuzzleIndex: originalIdx,
						}

						if backtrack(idxInSorted + 1) {
							return true
						}

						b.remove(deriv, x, y)
						delete(solutionMap, originalIdx)
					}
				}
			}
		}
		return false
	}

	if backtrack(0) {
		result := make([]Placement, len(puzzles))
		for i := range puzzles {
			result[i] = solutionMap[i]
		}
		return result, true
	}

	return nil, false
}

// Solve calculates the placements to solve the puzzle based on the input state.
func Solve(bd *BoardDesc) ([]Placement, error) {
	if len(bd.HueList) == 0 {
		return nil, errors.New("no hues found in board desc")
	}

	// Prepare data
	board := &Board{}
	if err := board.convertFromBoardDesc(bd); err != nil {
		return nil, err
	}

	hueMap := make(map[int]int)
	for i, h := range bd.HueList {
		hueMap[h] = i
	}

	puzzles := make([]*Puzzle, len(bd.PuzzleList))
	for i, pd := range bd.PuzzleList {
		pz := &Puzzle{}
		pz.convertFromPuzzleDesc(i, pd, hueMap)
		puzzles[i] = pz
	}

	result, ok := board.solveWith(puzzles)
	if !ok {
		return nil, errors.New("no solution found")
	}
	return result, nil
}
