// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type MapTrackerRandomMoveInRange struct{}

// RandomMoveInRangeParam represents the custom_action_param for RandomMoveInRange
type RandomMoveInRangeParam struct {
	// MapName is the name of the map to navigate (required).
	MapName string `json:"map_name"`
	// Target is the rectangular area [x, y, w, h] within which to randomly move.
	Target [4]float64 `json:"target"`
	// MinDistance is the minimum distance to move (default: 0).
	MinDistance float64 `json:"min_distance,omitempty"`
	// MaxDistance is the maximum distance to move (default: 20).
	MaxDistance float64 `json:"max_distance,omitempty"`
	// NoPrint controls whether to suppress printing navigation status to the GUI.
	NoPrint bool `json:"no_print,omitempty"`
	// ArrivalThreshold is the minimum distance to consider a target reached.
	ArrivalThreshold float64 `json:"arrival_threshold,omitempty"`
	// ArrivalTimeout is the maximum allowed time in milliseconds to reach the target.
	ArrivalTimeout int64 `json:"arrival_timeout,omitempty"`
	// SprintThreshold is the minimum distance beyond which sprinting is used.
	SprintThreshold float64 `json:"sprint_threshold,omitempty"`
	// MaxAttempts is the maximum number of attempts to move to a random target before giving up (default: 1).
	// Set to 0 or negative to disable retry (only one attempt).
	MaxAttempts int `json:"max_attempts,omitempty"`
	// Fallback is the node to execute when max attempts is exceeded and we want to skip to the next task.
	// When set, the action will execute this node instead of returning false.
	Fallback string `json:"fallback,omitempty"`
}

var randomMoveInRangeDefaultParam = RandomMoveInRangeParam{
	MinDistance:      0,
	MaxDistance:      20,
	ArrivalThreshold: 2.5,
	ArrivalTimeout:   60000,
	SprintThreshold:  10.0,
	MaxAttempts:      1,
}

var _ maa.CustomActionRunner = &MapTrackerRandomMoveInRange{}

// Run implements maa.CustomActionRunner
func (a *MapTrackerRandomMoveInRange) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// Parse parameters
	param, err := a.parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse parameters for MapTrackerRandomMoveInRange")
		return false
	}

	log.Info().Msg("Starting random move")

	ctrl := ctx.GetTasker().GetController()
	ca, err := control.NewControlAdaptor(ctx, ctrl, WORK_W, WORK_H)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create control adaptor")
		return false
	}

	// Reset player movement state
	ca.AggressivelyResetPlayerMovement()

	// Get current location and verify map name
	inferParam := &MapTrackerMoveParam{
		MapName:          param.MapName,
		MapNameMatchRule: "^%s(_tier_\\w+)?$",
	}
	initResult, err := doInfer(ctx, ctrl, inferParam)
	if err != nil {
		log.Error().Err(err).
			Msg("Failed to get initial location")
		return false
	}

	// Verify current map matches the expected map (like MapTrackerAssertLocation)
	if !a.isMapMatching(param.MapName, initResult.MapName) {
		log.Error().
			Str("expected_map", param.MapName).
			Str("actual_map", initResult.MapName).
			Msg("Current map does not match expected map")
		return false
	}

	currentX, currentY := initResult.X, initResult.Y

	// Generate random target point within the range
	targetX, targetY := a.generateRandomTarget(currentX, currentY, param)
	log.Info().
		Float64("currentX", currentX).
		Float64("currentY", currentY).
		Float64("targetX", targetX).
		Float64("targetY", targetY).
		Msg("Random target generated")

	// Show navigation UI
	if !param.NoPrint {
		maafocus.PrintLargeContentTrimNewline(
			a.buildNavigationMovingHTML(param, currentX, currentY, targetX, targetY),
		)
	}

	// Move to the random target
	success := a.navigateToTarget(ctx, ctrl, ca, param, inferParam, targetX, targetY)

	if success {
		log.Info().Msg("Random move completed successfully")
		if !param.NoPrint {
			maafocus.PrintLargeContentTrimNewline(
				a.buildNavigationFinishedHTML(param, targetX, targetY),
			)
		}
		return true
	}

	log.Warn().Msg("Random move failed")

	// Check if task is stopping
	if ctx.GetTasker().Stopping() {
		log.Warn().Msg("Task is stopping, exiting")
		return false
	}

	return false
}

// navigateToTarget navigates to the target point
func (a *MapTrackerRandomMoveInRange) navigateToTarget(
	ctx *maa.Context,
	ctrl *maa.Controller,
	ca control.ControlAdaptor,
	param *RandomMoveInRangeParam,
	inferParam *MapTrackerMoveParam,
	targetX, targetY float64,
) bool {
	loopInterval := time.Duration(INFER_INTERVAL_MS) * time.Millisecond
	lastLoopTime := time.Time{}
	lastArrivalTime := time.Now()
	prevLocationTime := time.Time{}
	var prevLocation *[2]float64

	for {
		// Calculate time since last check
		loopElapsed := time.Since(lastLoopTime)
		if loopElapsed < loopInterval {
			time.Sleep(loopInterval - loopElapsed)
		}
		loopStartTime := time.Now()
		lastLoopTime = loopStartTime

		// Check stopping signal
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("Task is stopping, exiting navigation loop")
			doPlayerStop(ca)
			return false
		}

		// Check arrival timeout
		deltaArrivalMs := loopStartTime.Sub(lastArrivalTime).Milliseconds()
		if deltaArrivalMs > param.ArrivalTimeout {
			log.Error().Msg("Arrival timeout, stopping task")
			doPlayerStop(ca)
			return false
		}

		// Run inference to get current location and rotation
		result, err := doInfer(ctx, ctrl, inferParam)
		if err != nil {
			log.Error().Err(err).Msg("Inference failed during navigation")
			ca.SetPlayerMovement(control.MovementStop, control.PolicyDefault)
			continue
		}
		curX, curY := result.X, result.Y
		rot := result.Rot

		// Check if stuck (no movement for a while)
		if prevLocation != nil && math.Hypot(prevLocation[0]-curX, prevLocation[1]-curY) < 2.0 {
			deltaLocationMs := loopStartTime.Sub(prevLocationTime).Milliseconds()
			if deltaLocationMs > 10000 {
				log.Error().Msg("Stuck for too long, stopping task")
				doPlayerStop(ca)
				return false
			}
		} else {
			prevLocation = &[2]float64{curX, curY}
			prevLocationTime = loopStartTime
		}

		// Calculate distance to target
		dist := math.Hypot(curX-targetX, curY-targetY)
		log.Debug().Float64("curX", curX).Float64("curY", curY).Float64("dist", dist).Msg("Navigating to random target")

		// Check arrival
		if dist < param.ArrivalThreshold {
			log.Info().Float64("dist", dist).Msg("Random target reached")
			doPlayerStop(ca)
			return true
		}

		// Calculate target rotation
		targetRot := calcTargetRotation(curX, curY, targetX, targetY)
		deltaRot := calcDeltaRotation(rot, targetRot)
		absDeltaRot := math.Abs(float64(deltaRot))

		// Determine movement speed based on distance and rotation
		if absDeltaRot > 60.0 {
			// Rotation is bad: walk and adjust
			ca.SetPlayerMovement(control.MovementWalk, control.PolicyDefault)
			if absDeltaRot > 1.0 {
				ca.RotateCamera(int(float64(deltaRot)*2.0), 0)
				ca.AggressivelyResetCamera()
			}
		} else if absDeltaRot > 7.5 {
			// Rotation is good: at least run
			ca.SetPlayerMovement(control.MovementRun, control.PolicyDefault)
			if absDeltaRot > 1.0 {
				ca.RotateCamera(int(float64(deltaRot)*2.0), 0)
				ca.AggressivelyResetCamera()
			}
		} else {
			// Rotation is very good: can sprint if target is far enough
			if dist > param.SprintThreshold {
				ca.SetPlayerMovement(control.MovementSprint, control.PolicyDefault)
			} else {
				ca.SetPlayerMovement(control.MovementRun, control.PolicyDefault)
			}
			// Fine rotation adjustment
			if absDeltaRot > 1.0 {
				ca.RotateCamera(int(float64(deltaRot)*2.0), 0)
				ca.AggressivelyResetCamera()
			}
		}
	}
}

// isMapMatching checks if the actual map name matches the expected map name,
// supporting tier variants like "map01_lv001_tier_1" matching "map01_lv001"
func (a *MapTrackerRandomMoveInRange) isMapMatching(expectedMap, actualMap string) bool {
	if expectedMap == actualMap {
		return true
	}
	return strings.HasPrefix(actualMap, expectedMap+"_tier_")
}

// generateRandomTarget 生成指定距离范围内的随机目标点
// 算法思路：先确定移动距离，再确定移动方向，确保距离始终符合要求
//
// 函数流程：
// 1. 首先尝试在指定距离范围内随机生成目标点
// 2. 如果生成的点在目标矩形区域内，则直接返回
// 3. 如果多次尝试都无法找到符合条件的点，则寻找距离目标矩形最近的点
//
// 参数说明：
// - currentX, currentY: 当前玩家坐标
// - param: 包含目标矩形区域[x, y, w, h]、最小和最大移动距离等参数
//
// 返回值：
// - 目标点的X和Y坐标
func (a *MapTrackerRandomMoveInRange) generateRandomTarget(currentX, currentY float64, param *RandomMoveInRangeParam) (float64, float64) {
	// 解析目标矩形区域参数
	x, y, w, h := param.Target[0], param.Target[1], param.Target[2], param.Target[3]

	// 初始化随机数生成器
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	// 最大尝试次数
	maxAttempts := 30

	// 第一阶段：尝试在指定距离范围内生成符合条件的目标点
	for i := 0; i < maxAttempts; i++ {
		// 随机选择一个在[minDistance, maxDistance]范围内的距离
		distance := param.MinDistance + rng.Float64()*(param.MaxDistance-param.MinDistance)

		// 随机选择一个方向角度（0-2π）
		angle := rng.Float64() * 2 * math.Pi

		// 根据距离和角度计算目标点坐标
		targetX := currentX + distance*math.Cos(angle)
		targetY := currentY + distance*math.Sin(angle)

		// 检查目标点是否在目标矩形区域内
		if targetX >= x && targetX <= x+w && targetY >= y && targetY <= y+h {
			return targetX, targetY
		}
	}

	// 如果第一阶段未能找到符合条件的目标点，输出警告日志
	log.Warn().
		Float64("currentX", currentX).
		Float64("currentY", currentY).
		Float64("minDistance", param.MinDistance).
		Float64("maxDistance", param.MaxDistance).
		Msg("Failed to find valid target after max attempts, using closest valid target")

	// 第二阶段：寻找距离目标矩形最近的点
	closestDist := math.MaxFloat64
	var bestX, bestY float64

	for i := 0; i < 100; i++ {
		// 继续随机生成目标点
		distance := param.MinDistance + rng.Float64()*(param.MaxDistance-param.MinDistance)
		angle := rng.Float64() * 2 * math.Pi
		targetX := currentX + distance*math.Cos(angle)
		targetY := currentY + distance*math.Sin(angle)

		// 如果这次生成的点恰好在目标区域内，直接返回
		if targetX >= x && targetX <= x+w && targetY >= y && targetY <= y+h {
			return targetX, targetY
		}

		// 将目标点坐标限制在目标矩形区域内
		clampedX := math.Max(x, math.Min(x+w, targetX))
		clampedY := math.Max(y, math.Min(y+h, targetY))
		// 计算限制后的点到当前位置的距离
		clampedDist := math.Hypot(clampedX-currentX, clampedY-currentY)

		// 保留距离最近的最佳点
		if clampedDist < closestDist {
			closestDist = clampedDist
			bestX = clampedX
			bestY = clampedY
		}
	}

	// 返回找到的最佳点（距离目标矩形最近且在距离范围内的点）
	return bestX, bestY
}

func (a *MapTrackerRandomMoveInRange) parseParam(paramStr string) (*RandomMoveInRangeParam, error) {
	log.Debug().Msg("Parsing and validating parameters for RandomMoveInRange")

	var param RandomMoveInRangeParam
	if err := json.Unmarshal([]byte(paramStr), &param); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	// Validate required parameters
	if len(param.MapName) == 0 {
		return nil, fmt.Errorf("map_name is required")
	}
	if len(param.Target) != 4 {
		return nil, fmt.Errorf("target must have 4 numbers [x, y, w, h]")
	}

	x, y, w, h := param.Target[0], param.Target[1], param.Target[2], param.Target[3]
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("width and height in target must be positive")
	}
	if math.IsNaN(x) || math.IsInf(x, 0) ||
		math.IsNaN(y) || math.IsInf(y, 0) ||
		math.IsNaN(w) || math.IsInf(w, 0) ||
		math.IsNaN(h) || math.IsInf(h, 0) {
		return nil, fmt.Errorf("target contains invalid coordinates")
	}

	// Set defaults
	if param.MinDistance < 0 {
		return nil, fmt.Errorf("min_distance must be non-negative")
	}
	if param.MinDistance == 0 {
		param.MinDistance = randomMoveInRangeDefaultParam.MinDistance
	}

	if param.MaxDistance <= 0 {
		return nil, fmt.Errorf("max_distance must be positive")
	}
	if param.MaxDistance == 0 {
		param.MaxDistance = randomMoveInRangeDefaultParam.MaxDistance
	}

	if param.MinDistance > param.MaxDistance {
		return nil, fmt.Errorf("min_distance must be less than or equal to max_distance")
	}

	if param.ArrivalThreshold < 0 {
		return nil, fmt.Errorf("arrival_threshold must be non-negative")
	} else if param.ArrivalThreshold == 0 {
		param.ArrivalThreshold = randomMoveInRangeDefaultParam.ArrivalThreshold
	}

	if param.ArrivalTimeout < 0 {
		return nil, fmt.Errorf("arrival_timeout must be non-negative")
	} else if param.ArrivalTimeout == 0 {
		param.ArrivalTimeout = randomMoveInRangeDefaultParam.ArrivalTimeout
	}

	if param.SprintThreshold < 0 {
		return nil, fmt.Errorf("sprint_threshold must be non-negative")
	} else if param.SprintThreshold == 0 {
		param.SprintThreshold = randomMoveInRangeDefaultParam.SprintThreshold
	}

	return &param, nil
}

func (a *MapTrackerRandomMoveInRange) buildNavigationMovingHTML(
	param *RandomMoveInRangeParam,
	currentX, currentY, targetX, targetY float64,
) string {
	return fmt.Sprintf(`<div style="padding: 10px;">
		<div style="font-size: 16px; font-weight: bold; margin-bottom: 5px;">Random Move</div>
		<div style="font-size: 14px;">Map: %s</div>
		<div style="font-size: 14px;">From: (%.1f, %.1f)</div>
		<div style="font-size: 14px;">To: (%.1f, %.1f)</div>
	</div>`, param.MapName, currentX, currentY, targetX, targetY)
}

func (a *MapTrackerRandomMoveInRange) buildNavigationFinishedHTML(param *RandomMoveInRangeParam, targetX, targetY float64) string {
	return fmt.Sprintf(`<div style="padding: 10px;">
		<div style="font-size: 16px; font-weight: bold; margin-bottom: 5px;">Random Move Completed</div>
		<div style="font-size: 14px;">Map: %s</div>
		<div style="font-size: 14px;">Reached: (%.1f, %.1f)</div>
	</div>`, param.MapName, targetX, targetY)
}
