// Copyright (c) 2026 Harry Huang
package puzzle

import (
	"image"
	"math"
)

// calcColorVar calculates the average standard deviation across RGB channels
func calcColorVar(img image.Image, rect image.Rectangle) float64 {
	var sumR, sumG, sumB float64
	var count float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8), float64(g>>8), float64(b>>8)
			sumR += fr
			sumG += fg
			sumB += fb
			count++
		}
	}
	if count == 0 {
		return 0
	}
	avgR := sumR / count
	avgG := sumG / count
	avgB := sumB / count

	var varR, varG, varB float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8), float64(g>>8), float64(b>>8)
			varR += (fr - avgR) * (fr - avgR)
			varG += (fg - avgG) * (fg - avgG)
			varB += (fb - avgB) * (fb - avgB)
		}
	}
	return (math.Sqrt(varR/count) + math.Sqrt(varG/count) + math.Sqrt(varB/count)) / 3.0
}

// calcColorSat calculates the average saturation [0.0~1.0]
func calcColorSat(img image.Image, rect image.Rectangle) float64 {
	var sumSat float64
	var count float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

			maxVal := math.Max(fr, math.Max(fg, fb))
			minVal := math.Min(fr, math.Min(fg, fb))
			delta := maxVal - minVal

			var s float64
			if maxVal != 0 {
				s = delta / maxVal
			}
			sumSat += s
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sumSat / count
}

// calcColorHue calculates the average hue [0.0~360.0)
func calcColorHue(img image.Image, rect image.Rectangle) float64 {
	var sumHue float64
	var count float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

			maxVal := math.Max(fr, math.Max(fg, fb))
			minVal := math.Min(fr, math.Min(fg, fb))
			delta := maxVal - minVal

			var h float64
			if delta != 0 {
				switch maxVal {
				case fr:
					h = (fg - fb) / delta
					if fg < fb {
						h += 6
					}
				case fg:
					h = (fb-fr)/delta + 2
				default:
					h = (fr-fg)/delta + 4
				}
				h *= 60
			}
			sumHue += h
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sumHue / count
}

// calcColorVal calculates the average value [0.0~1.0]
func calcColorVal(img image.Image, rect image.Rectangle) float64 {
	var sumVal float64
	var count float64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0
			v := math.Max(fr, math.Max(fg, fb))
			sumVal += v
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sumVal / count
}

// diffHue returns the smallest difference between two hues [0, 360)
func diffHue(h1, h2 int) int {
	diff := int(math.Abs(float64(h1 - h2)))
	if diff > 180 {
		diff = 360 - diff
	}
	return diff
}

// meanHue calculates the circular mean of a slice of hues [0, 360)
func meanHue(hues []int) int {
	if len(hues) == 0 {
		return 0
	}
	var sumSin, sumCos float64
	for _, h := range hues {
		rad := float64(h) * math.Pi / 180.0
		sumSin += math.Sin(rad)
		sumCos += math.Cos(rad)
	}
	avgRad := math.Atan2(sumSin, sumCos)
	avgDeg := avgRad * 180.0 / math.Pi
	if avgDeg < 0 {
		avgDeg += 360
	}
	return int(math.Round(avgDeg))
}

// getPixelHSV returns the Hue[0, 360), Saturation[0, 1], Value[0, 1] of a pixel
func getPixelHSV(img image.Image, x, y int, targetHue int, targetHueAllowance int) (float64, float64, float64) {
	r, g, b, _ := img.At(x, y).RGBA()
	fr, fg, fb := float64(r>>8)/255.0, float64(g>>8)/255.0, float64(b>>8)/255.0

	maxC := math.Max(fr, math.Max(fg, fb))
	minC := math.Min(fr, math.Min(fg, fb))
	delta := maxC - minC

	var h float64
	if delta == 0 {
		h = 0
	} else if maxC == fr {
		h = 60 * math.Mod((fg-fb)/delta, 6)
	} else if maxC == fg {
		h = 60 * ((fb-fr)/delta + 2)
	} else {
		h = 60 * ((fr-fg)/delta + 4)
	}
	if h < 0 {
		h += 360
	}

	if targetHue >= 0 {
		if diffHue(int(h), targetHue) > targetHueAllowance {
			return 0, 0, 0
		}
	}

	s := 0.0
	if maxC != 0 {
		s = delta / maxC
	}

	v := maxC
	return h, s, v
}

// clusterHues groups hues that are close to each other
func clusterHues(hues []int, maxDiff int) map[int][]int {
	clusters := make(map[int][]int)
	processed := make(map[int]bool)

	for _, h1 := range hues {
		if processed[h1] {
			continue
		}

		clusterID := h1
		// Check if h1 belongs to an existing cluster
		foundCluster := false
		for center := range clusters {
			if diffHue(h1, center) <= maxDiff {
				clusterID = center
				foundCluster = true
				break
			}
		}

		if !foundCluster {
			clusters[clusterID] = []int{}
		}
		clusters[clusterID] = append(clusters[clusterID], h1)
		processed[h1] = true
	}
	return clusters
}
