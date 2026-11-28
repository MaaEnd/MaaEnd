package camerascan

const (
	phaseGrid         = "grid"
	phaseReset        = "reset"
	phaseFallbackUp   = "fallback_up"
	phaseFallbackMid  = "fallback_middle"
	phaseFallbackDown = "fallback_down"
)

// gridSize 是网格边长。5 × 5 共 25 个机位，每个机位相差一个方向节点的一步。
// 方向节点的一步是 80 / 120 像素（约 22.5°），所以网格比旧九宫格密一倍。
const gridSize = 5

// ringStepUnits 是回退圈每转一次要走的节点步数。一圈仍是 8 次转动、每次 2 步，
// 合计 1280 像素（约 360°），与改动前回退圈的覆盖范围一致。
const ringStepUnits = 2

// 回退圈三档俯仰相对中位的偏移，单位为节点步。
const (
	ringMidPitchUnits  = 0
	ringUpPitchUnits   = -ringStepUnits
	ringDownPitchUnits = 2 * ringStepUnits
)

// cameraScanStep 把镜头移到下一个机位。一步可能连续移动多格，中间不识别。
// yawDelta / pitchDelta 以方向节点的一步为单位，正 yaw 向右、正 pitch 向下；
// recognize 为 false 的步只是把镜头摇过去（摇回中心），到点不识别。
type cameraScanStep struct {
	yawDelta   int
	pitchDelta int
	phase      string
	recognize  bool
}

// buildCameraScanPath 生成一次完整扫描：中心向外的 5 × 5 网格，摇回中心，
// 再按中、上、下三档俯仰各转一圈。
func buildCameraScanPath(fallbackYawSteps int) []cameraScanStep {
	steps := gridSteps()
	steps = append(steps, resetStep(steps))
	// 摇回中心后镜头在中位俯仰，所以先扫中圈，再看上、下两圈。
	steps = appendFallbackRing(steps, ringMidPitchUnits, phaseFallbackMid, fallbackYawSteps)
	steps = appendFallbackRing(steps, ringUpPitchUnits, phaseFallbackUp, fallbackYawSteps)
	steps = appendFallbackRing(steps, ringDownPitchUnits, phaseFallbackDown, fallbackYawSteps)
	return steps
}

// gridSteps 按螺旋顺序走完整个网格：第一个机位在中心，不移动，只识别。
func gridSteps() []cameraScanStep {
	cells := spiralCells()
	steps := make([]cameraScanStep, 0, len(cells))
	steps = append(steps, cameraScanStep{phase: phaseGrid, recognize: true})
	for index := 1; index < len(cells); index++ {
		steps = append(steps, cameraScanStep{
			yawDelta:   cells[index][0] - cells[index-1][0],
			pitchDelta: cells[index][1] - cells[index-1][1],
			phase:      phaseGrid,
			recognize:  true,
		})
	}
	return steps
}

// spiralCells 返回网格从中心向外的螺旋顺序，共 gridSize² 格。
// 中心优先是为了让目标落在常见位置时尽早命中。
func spiralCells() [][2]int {
	center := gridSize / 2
	col, row := center, center
	cells := [][2]int{{col, row}}
	for ring := 1; ring <= center; ring++ {
		cells = walk(cells, &col, &row, 1, 0, 1)
		cells = walk(cells, &col, &row, 0, -1, 2*ring-1)
		cells = walk(cells, &col, &row, -1, 0, 2*ring)
		cells = walk(cells, &col, &row, 0, 1, 2*ring)
		cells = walk(cells, &col, &row, 1, 0, 2*ring)
	}
	return cells
}

// walk 从 (col, row) 出发按方向走 count 步，逐格记入 cells。
func walk(cells [][2]int, col, row *int, yawStep, pitchStep, count int) [][2]int {
	for range count {
		*col += yawStep
		*row += pitchStep
		cells = append(cells, [2]int{*col, *row})
	}
	return cells
}

// resetStep 把镜头摇回网格起点。这一步只移动，不识别。
func resetStep(steps []cameraScanStep) cameraScanStep {
	yaw, pitch := 0, 0
	for _, step := range steps {
		yaw += step.yawDelta
		pitch += step.pitchDelta
	}
	return cameraScanStep{yawDelta: -yaw, pitchDelta: -pitch, phase: phaseReset}
}

// appendFallbackRing 在某一档俯仰上转一整圈，每步 ringStepUnits 格。
func appendFallbackRing(steps []cameraScanStep, pitchDelta int, phase string, fallbackYawSteps int) []cameraScanStep {
	steps = append(steps, cameraScanStep{pitchDelta: pitchDelta, phase: phase, recognize: true})
	for range fallbackYawSteps {
		steps = append(steps, cameraScanStep{yawDelta: ringStepUnits, phase: phase, recognize: true})
	}
	return steps
}
