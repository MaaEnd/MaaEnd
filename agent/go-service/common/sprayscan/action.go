package sprayscan

import (
	"encoding/json"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "WorldToolSprayScanAction"

	recenterNode  = "__WorldToolSprayRecenterAltDown"
	moveNode      = "__WorldToolSprayScanMove"
	holdNode      = "__WorldToolSprayHoldPress"
	crosshairNode = "__WorldToolSprayCrosshairOnStain"

	// stepPx 是每一格转动的像素，横向约 5.6°（1280 像素约等于 360°）。
	stepPx = 20
	// extremePitchPx 是抬到最高 / 压到最低时每次从屏幕中心拉动的像素，重复 3 次拉到底。
	extremePitchPx = 359
	// halfColumns 是在正前方左右各扫描的格数（约 ±34°）。
	halfColumns = 6

	// maxHoldsPerSpot 是同一格最多连续按住喷洒的次数（每次 2 秒）。
	maxHoldsPerSpot = 5
	// maxHoldsTotal 是整轮扫描最多按住喷洒的次数，防止一直喷不掉时卡住。
	maxHoldsTotal = 15
)

// upperWallRowGaps 是上方墙面每两行之间往下转的像素。镜头抬到最高时越往上同样的像素转得越多，
// 所以上密下疏，合计 120 像素约从最高处转到视线水平。
var upperWallRowGaps = []int{5, 5, 6, 7, 8, 10, 12, 15, 20, 32}

// groundRowGaps 是地面每两行之间往上转的像素。压到最低后往上前 180 像素左右画面几乎不动，
// 之后才均匀抬起，所以第一行跨度大、后面均匀，合计 620 像素约从最低处转到接近视线水平。
var groundRowGaps = []int{120, 80, 60, 60, 50, 50, 50, 50, 50, 50}

var _ maa.CustomActionRunner = &WorldToolSprayScanAction{}

// WorldToolSprayScanAction 在喷洒界面里逐格转动镜头寻找污渍，准心变绿时按住喷洒。
// 调用前需已处于喷洒界面且有液体供给；pattern 为 upper_wall 时先抬到最高，再在正前方从上往下蛇形扫到视线水平，
// 为 ground 时先压到最低，再在正前方从下往上蛇形扫到视线水平。光标跳动也会转动镜头，所以每一步前都按住 Alt 把光标归位到屏幕中心。
type WorldToolSprayScanAction struct{}

// Run 执行一轮扫描，扫描完成返回 true，参数错误、节点执行失败或任务停止时返回 false。
func (a *WorldToolSprayScanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse params")
		return false
	}

	// pitchDir 为 -1 时先抬到最高再逐行往下扫，为 1 时先压到最低再逐行往上扫。
	var pitchDir int
	var rowGaps []int
	switch params.Pattern {
	case "upper_wall":
		pitchDir, rowGaps = -1, upperWallRowGaps
	case "ground":
		pitchDir, rowGaps = 1, groundRowGaps
	default:
		log.Error().Str("component", componentName).Str("pattern", params.Pattern).Msg("unknown pattern")
		return false
	}

	// 先往左转到扫描起点，再逐行蛇形扫；起始位置也要识别，所以 steps 只包含「移动到下一格」。
	var steps []image.Point
	for range halfColumns {
		steps = append(steps, image.Pt(-stepPx, 0))
	}
	dir := 1
	for row := 0; row <= len(rowGaps); row++ {
		for range halfColumns * 2 {
			steps = append(steps, image.Pt(dir*stepPx, 0))
		}
		if row < len(rowGaps) {
			steps = append(steps, image.Pt(0, -pitchDir*rowGaps[row]))
		}
		dir = -dir
	}

	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return false
	}

	// move 先按住 Alt 把光标归位到屏幕中心，再从中心移动 (dx, dy) 转动镜头。
	move := func(dx, dy int) bool {
		return runNode(ctx, recenterNode, nil) && runNode(ctx, moveNode, map[string]any{
			moveNode: map[string]any{"end": maa.Rect{640 + dx, 360 + dy, 1, 1}},
		})
	}

	// spray 在当前这一格准心变绿时归位后按住喷洒，喷完再识别，直到变白或达到次数上限。
	holds := 0
	spray := func() bool {
		for spot := 0; spot < maxHoldsPerSpot && holds < maxHoldsTotal; spot++ {
			if tasker.Stopping() {
				return false
			}
			ctrl.PostScreencap().Wait()
			img, err := ctrl.CacheImage()
			if err != nil || img == nil {
				log.Warn().Err(err).Str("component", componentName).Msg("cache image failed")
				return true
			}
			detail, err := ctx.RunRecognition(crosshairNode, img)
			if err != nil || detail == nil || !detail.Hit {
				return true
			}
			if !runNode(ctx, recenterNode, nil) || !runNode(ctx, holdNode, nil) {
				return false
			}
			holds++
			log.Info().Str("component", componentName).Int("spot_holds", spot+1).Int("total_holds", holds).Msg("sprayed stain")
		}
		return !tasker.Stopping()
	}

	for range 3 {
		if !move(0, pitchDir*extremePitchPx) {
			return false
		}
	}
	if !spray() {
		return false
	}
	for _, step := range steps {
		if !move(step.X, step.Y) || !spray() {
			return false
		}
	}

	log.Info().Str("component", componentName).Str("pattern", params.Pattern).Int("steps", len(steps)).Int("holds", holds).Msg("spray scan finished")
	return true
}

// runNode 以 entry 为入口执行 pipeline 节点链，任务停止或执行失败时返回 false。
func runNode(ctx *maa.Context, entry string, override map[string]any) bool {
	var detail *maa.TaskDetail
	var err error
	if override == nil {
		detail, err = ctx.RunTask(entry)
	} else {
		detail, err = ctx.RunTask(entry, override)
	}
	if err != nil || detail == nil || !detail.Status.Success() {
		log.Error().Err(err).Str("component", componentName).Str("entry", entry).Msg("run node failed")
		return false
	}
	return true
}
