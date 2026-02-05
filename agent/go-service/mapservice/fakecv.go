package mapservice

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"
)

// ==========================================
// 基础图像工具
// ==========================================

// EnsureRGBA 将任意图像转换为 *image.RGBA
func EnsureRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	bounds := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			dst.Set(x, y, img.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}

// copySubImage 从源图像的指定矩形区域创建一个新的 RGBA 图像。
// 生成的图像 Bounds() 从 (0,0) 开始。
func copySubImage(src *image.RGBA, r image.Rectangle) *image.RGBA {
	w, h := r.Dx(), r.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
	return dst
}

// DownscaleRGBA 分配一个新图像并使用最近邻插值进行降采样。
func DownscaleRGBA(img image.Image, scale int) *image.RGBA {
	bounds := img.Bounds()
	newW := bounds.Dx() / scale
	newH := bounds.Dy() / scale
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	DownscaleRGBAInto(img, dst, scale)
	return dst
}

// DownscaleRGBAInto 使用最近邻插值将 src 降采样到 dst。
// dst 的边界必须匹配 src 边界 / 缩放比例。
func DownscaleRGBAInto(img image.Image, dst *image.RGBA, scale int) {
	bounds := img.Bounds()
	newW := dst.Bounds().Dx()
	newH := dst.Bounds().Dy()

	// RGBA 快速路径
	if src, ok := img.(*image.RGBA); ok {
		srcStride := src.Stride
		dstStride := dst.Stride
		srcPix := src.Pix
		dstPix := dst.Pix

		for y := 0; y < newH; y++ {
			// Src Y coordinate
			srcY := bounds.Min.Y + y*scale
			srcRowY := srcY - src.Rect.Min.Y
			if srcRowY < 0 {
				continue
			}
			srcRowStart := srcRowY * srcStride
			dstRowOffset := y * dstStride

			for x := 0; x < newW; x++ {
				srcX := bounds.Min.X + x*scale
				srcColX := srcX - src.Rect.Min.X
				srcOffset := srcRowStart + srcColX*4
				dstOffset := dstRowOffset + x*4
				copy(dstPix[dstOffset:dstOffset+4], srcPix[srcOffset:srcOffset+4])
			}
		}
		return
	}

	// 其他慢速路径
	for y := 0; y < newH; y++ {
		srcY := bounds.Min.Y + y*scale
		for x := 0; x < newW; x++ {
			srcX := bounds.Min.X + x*scale
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
}

// ==========================================
// 几何与绘图
// ==========================================

func checkBounds(x, y, w, h int) bool {
	return x >= 0 && x < w && y >= 0 && y < h
}

func drawRect(dst *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	bounds := dst.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 > w {
		x2 = w
	}
	if y2 > h {
		y2 = h
	}

	// Top & Bottom
	for x := x1; x < x2; x++ {
		if checkBounds(x, y1, w, h) {
			dst.Set(x, y1, c)
		}
		if checkBounds(x, y2-1, w, h) {
			dst.Set(x, y2-1, c)
		}
	}
	// Left & Right
	for y := y1; y < y2; y++ {
		if checkBounds(x1, y, w, h) {
			dst.Set(x1, y, c)
		}
		if checkBounds(x2-1, y, w, h) {
			dst.Set(x2-1, y, c)
		}
	}
}

// GenerateCircularMask 为圆形小地图创建 Alpha 蒙版
func GenerateCircularMask(w, h int) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, w, h))

	// 外半径（保留地图区域）
	radius := float64(w) / 2
	if float64(h)/2 < radius {
		radius = float64(h) / 2
	}

	// 内半径（移除玩家箭头）
	innerRadius := 10.0

	cx, cy := float64(w)/2, float64(h)/2

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) - cx + 0.5
			dy := float64(y) - cy + 0.5
			distSq := dx*dx + dy*dy

			// 在外半径内 且 在内半径外
			if distSq <= radius*radius && distSq > innerRadius*innerRadius {
				mask.SetAlpha(x, y, color.Alpha{255}) // 有效
			} else {
				mask.SetAlpha(x, y, color.Alpha{0}) // 忽略（中心 + 边缘）
			}
		}
	}
	return mask
}

// ApplyMaskFastInto 将 Alpha 蒙版应用到图像并绘制到 dst。
func ApplyMaskFastInto(src image.Image, dst *image.RGBA, mask *image.Alpha) {
	bounds := src.Bounds()
	// 将 src 绘制到 dst
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	// 应用 alpha 蒙版
	draw.DrawMask(dst, dst.Bounds(), src, bounds.Min, mask, image.Point{}, draw.Src)
}

// ==========================================
// 图像过滤/遮罩
// ==========================================

// maskIcons 过滤黄色和蓝色图标。
func maskIcons(img *image.RGBA) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	pix := img.Pix
	stride := img.Stride

	// 色差阈值
	const DiffThreshold = 40

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*stride + x*4

			// 跳过透明像素
			if pix[offset+3] == 0 {
				continue
			}

			r := int(pix[offset+0])
			g := int(pix[offset+1])
			b := int(pix[offset+2])

			isYellow := false
			isBlue := false

			// 检查黄色 (High R, High G, Low B)
			if r > 100 && g > 100 {
				minRG := r
				if g < minRG {
					minRG = g
				}

				if (minRG - b) > DiffThreshold {
					isYellow = true
				}
			}

			// 检查蓝色 (High B, Low R, Low G)
			if b > 100 {
				maxRG := r
				if g > maxRG {
					maxRG = g
				}

				if (b - maxRG) > DiffThreshold {
					isBlue = true
				}
			}

			if isYellow || isBlue {
				pix[offset+3] = 0 // 遮罩过滤
			}
		}
	}
}

// ApplySpotlightEffect 通过透明化移除 Tier 地图中的暗区（空白）。
func ApplySpotlightEffect(img *image.RGBA, threshold int) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	pix := img.Pix
	stride := img.Stride

	for y := 0; y < h; y++ {
		rowOffset := y * stride
		for x := 0; x < w; x++ {
			offset := rowOffset + x*4

			// 如果已经是透明的，则跳过
			if pix[offset+3] == 0 {
				continue
			}

			r := int(pix[offset+0])
			g := int(pix[offset+1])
			b := int(pix[offset+2])

			luma := (r*3 + g*6 + b) / 10

			if luma < threshold {
				// 设置 Alpha 为 0（透明）
				pix[offset+3] = 0
				// 可选：清除颜色通道以便调试/保持整洁
				pix[offset+0] = 0
				pix[offset+1] = 0
				pix[offset+2] = 0
			}
		}
	}
}

// ApplyVoidFilter 扫描图像并将暗区（低于阈值）替换为透明 Alpha (0)。
func ApplyVoidFilter(img *image.RGBA, threshold int) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	pix := img.Pix
	stride := img.Stride

	for y := 0; y < h; y++ {
		rowOffset := y * stride
		for x := 0; x < w; x++ {
			offset := rowOffset + x*4

			r := int(pix[offset+0])
			g := int(pix[offset+1])
			b := int(pix[offset+2])

			luma := (r*3 + g*6 + b) / 10

			if luma < threshold {
				// 设置 Alpha 为 0（透明）
				pix[offset+3] = 0
				// 清除 RGB 通道
				pix[offset+0] = 0
				pix[offset+1] = 0
				pix[offset+2] = 0
			}
		}
	}
}

// ==========================================
// 模板匹配（核心算法）
// ==========================================

// pixelTask 定义了一个需要匹配的像素点
type pixelTask struct {
	offset  int // 相对于 (x,y) 起点的字节偏移量
	r, g, b int
}

// compiledTemplate 存储扁平化后的模板
type compiledTemplate struct {
	pixels []pixelTask
	width  int
	height int
}

// abs 为无分支整数绝对值计算
func abs(x int) int {
	y := x >> 63
	return (x ^ y) - y
}

// compileTemplate 将模板“扁平化”，移除所有透明像素，并预计算偏移量
func compileTemplate(tpl *image.RGBA, imgStride int, step int, skipRows int, sampleRate int) *compiledTemplate {
	bounds := tpl.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	tplStride := tpl.Stride
	tplPix := tpl.Pix

	estimatedPixels := (w * h) / 4
	pixels := make([]pixelTask, 0, estimatedPixels)

	rowStep := 1 + skipRows
	counter := 0

	for y := 0; y < h; y += rowStep {
		tplRowOffset := y * tplStride
		imgRowRelativeOffset := y * imgStride

		for x := 0; x < w; x++ {
			if tplPix[tplRowOffset+x*4+3] == 0 {
				continue
			}

			counter++
			if counter%sampleRate != 0 {
				continue
			}

			pixels = append(pixels, pixelTask{
				offset: imgRowRelativeOffset + x*4,
				r:      int(tplPix[tplRowOffset+x*4]),
				g:      int(tplPix[tplRowOffset+x*4+1]),
				b:      int(tplPix[tplRowOffset+x*4+2]),
			})
		}
	}

	return &compiledTemplate{
		pixels: pixels,
		width:  w,
		height: h,
	}
}

func MatchTemplateRGBA(img *image.RGBA, tpl *image.RGBA, step int, skipRows int, sampleRate int) (bestX, bestY int, maxScore float64) {
	imgW, imgH := img.Bounds().Dx(), img.Bounds().Dy()

	ct := compileTemplate(tpl, img.Stride, step, skipRows, sampleRate)

	validPixels := len(ct.pixels)
	if validPixels == 0 {
		return 0, 0, 0
	}

	maxX := imgW - ct.width
	maxY := imgH - ct.height
	imgPix := img.Pix

	if len(imgPix) > 0 {
		_ = imgPix[len(imgPix)-1]
	}

	var mutex sync.Mutex
	globalBestSAD := math.MaxInt64
	globalBestX, globalBestY := 0, 0

	cx := maxX / 2
	cy := maxY / 2

	numWorkers := 8
	rowsPerWorker := (maxY/step + 1 + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		startIdx := i * rowsPerWorker
		endIdx := (i + 1) * rowsPerWorker

		go func(sIdx, eIdx int) {
			defer wg.Done()

			localBestSAD := math.MaxInt64
			localBestX, localBestY := 0, 0

			yStart := sIdx * step
			yEnd := eIdx * step
			if yEnd > maxY {
				yEnd = maxY + 1
			}

			for y := yStart; y < yEnd; y += step {
				rowBaseOffset := y * img.Stride

				for x := 0; x <= maxX; x += step {
					if x == cx && y == cy {
						continue
					}

					baseOffset := rowBaseOffset + x*4

					var currentSAD int
					for i := range ct.pixels {
						p := &ct.pixels[i]
						off := baseOffset + p.offset

						r := int(imgPix[off])
						g := int(imgPix[off+1])
						b := int(imgPix[off+2])

						currentSAD += abs(r - p.r)
						currentSAD += abs(g - p.g)
						currentSAD += abs(b - p.b)

						if currentSAD > localBestSAD {
							break
						}
					}

					if currentSAD < localBestSAD {
						localBestSAD = currentSAD
						localBestX = x
						localBestY = y
					}
				}
			}

			mutex.Lock()
			if localBestSAD < globalBestSAD {
				globalBestSAD = localBestSAD
				globalBestX = localBestX
				globalBestY = localBestY
			}
			mutex.Unlock()
		}(startIdx, endIdx)
	}

	wg.Wait()

	avgDiff := float64(globalBestSAD) / float64(validPixels*3)
	score := 1.0 - (avgDiff / 255.0)
	if score < 0 {
		score = 0
	}

	return globalBestX, globalBestY, score
}
