package sharedziplinedelete

import (
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName   = "SharedZiplineDeleteHitRecognition"
	colorMatchNode  = "__SharedZiplineDeleteHitColor"
	centerTolerance = 10
)

var _ maa.CustomRecognitionRunner = &HitRecognition{}

// HitRecognition 消费性点选共享滑索：跳过已点 box（中心 ±10px），按左上→右上→左下→右下选取。
type HitRecognition struct{}

var visitedBoxes []maa.Rect

func (r *HitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("nil context or arg")
		return nil, false
	}

	detail, err := ctx.RunRecognition(colorMatchNode, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("RunRecognition failed")
		return nil, false
	}

	var boxes []maa.Rect
	if detail != nil && detail.Results != nil {
		for _, result := range detail.Results.Filtered {
			if result == nil {
				continue
			}
			if cm, ok := result.AsColorMatch(); ok && cm != nil {
				boxes = append(boxes, cm.Box)
			}
		}
	}
	if len(boxes) == 0 {
		visitedBoxes = nil
		log.Info().Str("component", componentName).Msg("no boxes, clear visited")
		return nil, false
	}

	// 左上=0 右上=1 左下=2 右下=3，同象限再按 y、x
	sort.Slice(boxes, func(i, j int) bool {
		return boxOrder(boxes[i]) < boxOrder(boxes[j])
	})

	for _, box := range boxes {
		cx, cy := box.X()+box.Width()/2, box.Y()+box.Height()/2
		seen := false
		for _, prev := range visitedBoxes {
			px, py := prev.X()+prev.Width()/2, prev.Y()+prev.Height()/2
			if abs(cx-px) <= centerTolerance && abs(cy-py) <= centerTolerance {
				seen = true
				break
			}
		}
		if seen {
			continue
		}
		visitedBoxes = append(visitedBoxes, box)
		log.Info().Str("component", componentName).Interface("box", box).Msg("selected box")
		return &maa.CustomRecognitionResult{Box: box}, true
	}

	visitedBoxes = nil
	log.Info().Str("component", componentName).Msg("all boxes visited, clear")
	return nil, false
}

func boxOrder(box maa.Rect) int {
	cx, cy := box.X()+box.Width()/2, box.Y()+box.Height()/2
	q := 0
	if cx >= 640 {
		q |= 1
	}
	if cy >= 360 {
		q |= 2
	}
	return q<<20 | cy<<10 | cx
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
