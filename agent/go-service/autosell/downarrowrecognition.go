package autosell

import (
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const autoSellDownArrowRecognitionName = "AutoSellDownArrowRecognition"

const (
	// 背景是浅灰，通道差只有个位数；红、绿箭头的通道差都在 120 以上
	arrowChromaMin = 60
	// 下降箭头宽边在上，实测上半像素是下半的 2.0~2.5 倍；上升箭头为 0.4~0.55 倍
	downArrowTopBottomRatio = 1.5
)

var _ maa.CustomRecognitionRunner = &AutoSellDownArrowRecognition{}

// AutoSellDownArrowRecognition 判断 ROI 里的箭头是否朝下。
// ROI 应正好框住箭头；比较上下两半的有色像素，与箭头颜色和大小无关。
type AutoSellDownArrowRecognition struct{}

// Run 上半有色像素明显多于下半时命中。
func (r *AutoSellDownArrowRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", autoSellDownArrowRecognitionName).Msg("custom recognition arg or image is nil")
		return nil, false
	}

	rect := image.Rect(arg.Roi.X(), arg.Roi.Y(), arg.Roi.X()+arg.Roi.Width(), arg.Roi.Y()+arg.Roi.Height()).
		Intersect(arg.Img.Bounds())
	half := rect.Dy() / 2
	if half == 0 || rect.Dx() == 0 {
		log.Warn().Str("component", autoSellDownArrowRecognitionName).Interface("roi", arg.Roi).Msg("roi is too small")
		return nil, false
	}

	// 高度为奇数时中间一行两边都不算
	top := countChromaPixels(arg.Img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+half))
	bottom := countChromaPixels(arg.Img, image.Rect(rect.Min.X, rect.Max.Y-half, rect.Max.X, rect.Max.Y))
	down := top > 0 && float64(top) > float64(bottom)*downArrowTopBottomRatio

	log.Debug().
		Str("component", autoSellDownArrowRecognitionName).
		Interface("roi", arg.Roi).
		Int("top", top).
		Int("bottom", bottom).
		Bool("down", down).
		Msg("arrow direction checked")
	if !down {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: arg.Roi}, true
}

func countChromaPixels(img image.Image, rect image.Rectangle) int {
	count := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			hi := max(r, g, b) >> 8
			lo := min(r, g, b) >> 8
			if hi-lo > arrowChromaMin {
				count++
			}
		}
	}
	return count
}
