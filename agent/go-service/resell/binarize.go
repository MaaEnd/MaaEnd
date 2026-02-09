package resell

import (
    "image"
    "image/color"
)

// BinarizeMode 二值化模式
type BinarizeMode int

const (
    // BinarizeDarkText 深色文字在浅色背景上（如：灰色数字在白色背景）
    // 结果：黑色文字 + 白色背景
    BinarizeDarkText BinarizeMode = iota
    // BinarizeLightText 浅色文字在深色背景上（如：白色数字在灰色背景）
    // 结果：黑色文字 + 白色背景
    BinarizeLightText
)

// String 返回二值化模式的描述
func (m BinarizeMode) String() string {
    switch m {
    case BinarizeDarkText:
        return "DarkText(灰字白底)"
    case BinarizeLightText:
        return "LightText(白字灰底)"
    default:
        return "Unknown"
    }
}

// BackgroundColor 需要被标记为底色的颜色定义
type BackgroundColor struct {
    R, G, B   uint8 // 目标颜色的 RGB 值
    Tolerance uint8 // 每通道允许的偏差范围
}

// backgroundColors 需要被视为底色的颜色组
// 在二值化过程中，匹配这些颜色的像素将被强制标记为白色（背景）
// 可根据实际 UI 需要增减颜色
var backgroundColors = []BackgroundColor{
    {R: 0xD0, G: 0xE3, B: 0x00, Tolerance: 15}, // #D0E300 黄绿色 四号谷地
    {R: 0x00, G: 0xBC, B: 0xBC, Tolerance: 15}, // #00BCBC 青色 武陵
}

// isBackgroundColor 检查像素是否匹配底色组中的任一颜色
func isBackgroundColor(r, g, b uint8) bool {
    for _, bg := range backgroundColors {
        dr := int(r) - int(bg.R)
        dg := int(g) - int(bg.G)
        db := int(b) - int(bg.B)
        if dr < 0 {
            dr = -dr
        }
        if dg < 0 {
            dg = -dg
        }
        if db < 0 {
            db = -db
        }
        if dr <= int(bg.Tolerance) && dg <= int(bg.Tolerance) && db <= int(bg.Tolerance) {
            return true
        }
    }
    return false
}

// BinarizeForOCR 对图像进行二值化处理以提高 OCR 识别率
// 使用 Otsu 自适应阈值算法自动计算最佳阈值
// mode 指定文字与背景的对比模式：
//   - BinarizeDarkText: 适用于白色/浅色背景上的灰色/深色数字
//   - BinarizeLightText: 适用于灰色/深色背景上的白色/浅色数字
//
// 两种模式均输出黑色文字 + 白色背景的二值图像
func BinarizeForOCR(img image.Image, mode BinarizeMode) image.Image {
    bounds := img.Bounds()
    grayImg := toGrayscale(img)
    threshold := otsuThreshold(grayImg)

    white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
    black := color.RGBA{R: 0, G: 0, B: 0, A: 255}

    result := image.NewRGBA(bounds)
    for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
        for x := bounds.Min.X; x < bounds.Max.X; x++ {
            // 优先检查原始像素是否匹配底色组，匹配则强制标记为白色（背景）
            r, g, b, _ := img.At(x, y).RGBA()
            pr, pg, pb := uint8(r>>8), uint8(g>>8), uint8(b>>8)
            if isBackgroundColor(pr, pg, pb) {
                result.Set(x, y, white)
                continue
            }

            // 非底色像素按阈值进行二值化
            gray := grayImg.GrayAt(x, y).Y
            var c color.RGBA
            switch mode {
            case BinarizeLightText:
                // 文字比背景亮：高于阈值的是文字(→黑)，低于阈值的是背景(→白)
                if gray >= threshold {
                    c = black
                } else {
                    c = white
                }
            default:
                // 默认按 DarkText 处理
                // 文字比背景暗：低于阈值的是文字(→黑)，高于阈值的是背景(→白)
                if gray >= threshold {
                    c = white
                } else {
                    c = black
                }
            }
            result.Set(x, y, c)
        }
    }
    return result
}

// toGrayscale 将图像转换为灰度图
// 使用标准亮度公式: Y = 0.299*R + 0.587*G + 0.114*B
func toGrayscale(img image.Image) *image.Gray {
    bounds := img.Bounds()
    grayImg := image.NewGray(bounds)
    for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
        for x := bounds.Min.X; x < bounds.Max.X; x++ {
            r, g, b, _ := img.At(x, y).RGBA()
            // RGBA() 返回的是 [0, 65535] 范围，右移 8 位得到 [0, 255]
            lum := uint8(0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8))
            grayImg.SetGray(x, y, color.Gray{Y: lum})
        }
    }
    return grayImg
}

// otsuThreshold 使用大津法（Otsu's Method）计算最佳二值化阈值
// 该算法通过最大化类间方差自动确定最优阈值，适用于双峰分布的灰度直方图
func otsuThreshold(img *image.Gray) uint8 {
    bounds := img.Bounds()

    // 构建灰度直方图
    var histogram [256]int
    for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
        for x := bounds.Min.X; x < bounds.Max.X; x++ {
            histogram[img.GrayAt(x, y).Y]++
        }
    }

    totalPixels := (bounds.Max.X - bounds.Min.X) * (bounds.Max.Y - bounds.Min.Y)
    if totalPixels == 0 {
        return 128
    }

    // 计算所有像素灰度值的总和
    var totalSum float64
    for i := 0; i < 256; i++ {
        totalSum += float64(i) * float64(histogram[i])
    }

    var sumBackground float64
    var weightBackground, weightForeground int
    var maxVariance float64
    var bestThreshold uint8

    for t := 0; t < 256; t++ {
        weightBackground += histogram[t]
        if weightBackground == 0 {
            continue
        }

        weightForeground = totalPixels - weightBackground
        if weightForeground == 0 {
            break
        }

        sumBackground += float64(t) * float64(histogram[t])

        meanBackground := sumBackground / float64(weightBackground)
        meanForeground := (totalSum - sumBackground) / float64(weightForeground)

        // 类间方差
        variance := float64(weightBackground) * float64(weightForeground) *
            (meanBackground - meanForeground) * (meanBackground - meanForeground)

        if variance > maxVariance {
            maxVariance = variance
            bestThreshold = uint8(t)
        }
    }

    return bestThreshold
}
