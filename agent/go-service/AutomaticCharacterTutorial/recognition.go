package AutomaticCharacterTutorial

import (
	"encoding/json"
	"image"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// DynamicMatchRecognition implements logic to compare a top hint icon with candidate icons below
// 动态匹配识别：比较顶部提示图标和下方候选图标，并识别对应按键数字
type DynamicMatchRecognition struct{}

// Run implements the custom recognition logic
func (r *DynamicMatchRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 1. Parse parameters
	// 解析参数
	var params struct {
		TopROI     []int   `json:"top_roi"`     // [x, y, w, h] 目标提示图标区域
		SkillROI   []int   `json:"skill_roi"`   // [x, y, w, h] 更精细的图标区域（新增）
		BottomROIs [][]int `json:"bottom_rois"` // [[x, y, w, h], ...] 候选角色技能图标区域列表
		KeyROIs    [][]int `json:"key_rois"`    // [[x, y, w, h], ...] 候选角色技能下方数字区域列表，与 BottomROIs 一一对应
		Threshold  float64 `json:"threshold"`   // 差异阈值 (0-255), 越小越严格
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse params for DynamicMatchRecognition")
		return nil, false
	}

	// Default threshold if not set
	if params.Threshold == 0 {
		params.Threshold = 60.0 // 默认容差
	}

	img := arg.Img
	if img == nil {
		return nil, false
	}

	// 2. Crop Top Image
	// 优先使用 SkillROI（更精细的图标），如果没有则使用 TopROI
	var targetROI []int
	if len(params.SkillROI) >= 4 {
		// 检查 SkillROI 是否在图片范围内
		skillRect := image.Rect(params.SkillROI[0], params.SkillROI[1], params.SkillROI[0]+params.SkillROI[2], params.SkillROI[1]+params.SkillROI[3])
		if skillRect.In(img.Bounds()) {
			// 如果 SkillROI 有效且在范围内，优先使用它
			// 但这里有个问题：我们如何知道战技是否"正常识别"？
			// 用户说"当战技正常识别...就直接截取skill_roi...匹配不上就用top_roi"
			// 这意味着我们需要先尝试 SkillROI，如果匹配失败（bestIdx == -1），再尝试 TopROI。

			// 策略：先设置 targetROI 为 SkillROI，后续如果匹配失败，再回退到 TopROI 重试。
			targetROI = params.SkillROI
		} else {
			targetROI = params.TopROI
		}
	} else {
		targetROI = params.TopROI
	}

	if len(targetROI) < 4 {
		log.Error().Msg("Invalid TopROI/SkillROI")
		return nil, false
	}

	// Helper interface for cropping
	type SubImager interface {
		SubImage(r image.Rectangle) image.Image
	}

	// 封装匹配逻辑为函数，以便重试
	matchWithROI := func(roi []int) (int, float64) {
		cropRect := image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3])
		// Check bounds
		if !cropRect.In(img.Bounds()) {
			cropRect = cropRect.Intersect(img.Bounds())
			if cropRect.Empty() {
				log.Error().Msg("ROI outside of image bounds")
				return -1, -1.0
			}
		}

		subImager, ok := img.(SubImager)
		if !ok {
			return -1, -1.0
		}
		cropImg := subImager.SubImage(cropRect)

		// 保存截图到临时文件
		tempFile, err := os.CreateTemp("", "maa_dynamic_*.png")
		if err != nil {
			log.Error().Err(err).Msg("Failed to create temp file")
			return -1, -1.0
		}
		tempPath := tempFile.Name()
		if err := png.Encode(tempFile, cropImg); err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			log.Error().Err(err).Msg("Failed to encode temp image")
			return -1, -1.0
		}
		tempFile.Close()
		defer os.Remove(tempPath)

		localBestIdx := -1
		localMaxScore := -1.0

		for i, bottomROI := range params.BottomROIs {
			if len(bottomROI) < 4 {
				continue
			}
			taskName := "DynamicMatch_" + strconv.Itoa(i)
			tmParam := map[string]any{
				taskName: map[string]any{
					"recognition": "TemplateMatch",
					"template":    tempPath,
					"threshold":   0.7,
					"roi":         bottomROI,
					"method":      5,
				},
			}
			res := ctx.RunRecognition(taskName, img, tmParam)

			var score float64
			if res != nil && res.Hit {
				var detail struct {
					Best struct {
						Score float64 `json:"score"`
					} `json:"best"`
				}
				if err := json.Unmarshal([]byte(res.DetailJson), &detail); err == nil {
					score = detail.Best.Score
				}
			} else {
				// 未命中是正常现象（例如该位置没有角色），仅记录调试日志
				log.Debug().Int("index", i).Msg("TemplateMatch not hit")
			}

			if score > localMaxScore {
				localMaxScore = score
				localBestIdx = i
			}
		}

		if localMaxScore < 0.7 {
			// 如果所有候选区域的分数都低于阈值，说明没找到匹配
			// 返回 -1 表示未命中，而不是错误
			return -1, localMaxScore
		}
		return localBestIdx, localMaxScore
	}

	// 第一次尝试：使用 targetROI (可能是 SkillROI)
	bestIdx, minDiff := matchWithROI(targetROI)

	// 如果失败，且之前用的是 SkillROI，尝试回退到 TopROI
	if bestIdx == -1 && len(params.SkillROI) >= 4 && len(params.TopROI) >= 4 &&
		targetROI[0] == params.SkillROI[0] && targetROI[1] == params.SkillROI[1] {

		log.Debug().Msg("Match with SkillROI failed, retrying with TopROI")
		bestIdx, minDiff = matchWithROI(params.TopROI)
	}

	// ... 后续 OCR 逻辑保持不变 ...

	log.Info().Int("bestIdx", bestIdx).Float64("score", minDiff).Float64("threshold", params.Threshold).Msg("Dynamic match result")

	// 4. Return result if match found
	// 如果最佳匹配的差异小于阈值，则返回
	if bestIdx != -1 && minDiff >= 0.7 { // 注意：这里使用了硬编码的 0.7 阈值，因为我们用的是 TemplateMatch score
		matchedROI := params.BottomROIs[bestIdx]
		// Create box for the matched icon
		// Fix: maa.Rect is type Rect [4]int (or similar array/slice), not struct with named fields
		box := maa.Rect{matchedROI[0], matchedROI[1], matchedROI[2], matchedROI[3]}

		keyNum := -1
		// If KeyROIs are provided, perform OCR to get the key number
		if bestIdx < len(params.KeyROIs) {
			keyROI := params.KeyROIs[bestIdx]
			if len(keyROI) >= 4 {
				// Call OCR for the key number
				ocrParam := map[string]any{
					"OCR_Key": map[string]any{
						"recognition": "OCR",
						"expected":    []string{"1", "2", "3", "4"}, // We expect digits
						"roi":         keyROI,
					},
				}

				// We use RunRecognition to get OCR result for the specific ROI
				ocrRes := ctx.RunRecognition("OCR_Key", img, ocrParam)

				if ocrRes != nil && ocrRes.Hit {
					// Parse detail to find the text
					var ocrDetail struct {
						Text string `json:"text"`
						Best struct {
							Text string `json:"text"`
						} `json:"best"` // Sometimes it's in best
					}

					if err := json.Unmarshal([]byte(ocrRes.DetailJson), &ocrDetail); err == nil {
						text := ocrDetail.Text
						if text == "" {
							text = ocrDetail.Best.Text
						}

						// Extract digits
						text = strings.TrimSpace(text)
						if val, err := strconv.Atoi(text); err == nil {
							keyNum = val
						} else {
							// Try to find any digit
							for _, r := range text {
								if r >= '0' && r <= '9' {
									keyNum = int(r - '0')
									break
								}
							}
						}
					}
				}
			}
		}

		detailBytes, _ := json.Marshal(map[string]any{
			"index":   bestIdx,
			"score":   minDiff, // 复用 minDiff 作为 score
			"key_num": keyNum,
		})

		return &maa.CustomRecognitionResult{
			Box:    box,
			Detail: string(detailBytes),
		}, true
	}

	return nil, false
}

// compareImagesWhiteOnly calculates difference focusing on bright/white areas
// 仅比较两张图片中“白色/高亮”部分的差异，忽略背景杂色
func compareImagesWhiteOnly(img1, img2 image.Image) float64 {
	b1 := img1.Bounds()
	b2 := img2.Bounds()
	w1, h1 := b1.Dx(), b1.Dy()
	w2, h2 := b2.Dx(), b2.Dy()

	w := min(w1, w2)
	h := min(h1, h2)

	var sumDiff float64
	count := 0

	// 亮度阈值：只有当图片中某一点的亮度超过此值时，才参与比较
	// 这样可以过滤掉深色背景
	const brightnessThreshold = 100.0

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r1, g1, b1, _ := img1.At(b1.Min.X+x, b1.Min.Y+y).RGBA()
			r2, g2, b2, _ := img2.At(b2.Min.X+x, b2.Min.Y+y).RGBA()

			pr1, pg1, pb1 := float64(r1>>8), float64(g1>>8), float64(b1>>8)
			pr2, pg2, pb2 := float64(r2>>8), float64(g2>>8), float64(b2>>8)

			// 计算亮度 (简单的 RGB 平均，或者用 0.299R+0.587G+0.114B)
			bright1 := (pr1 + pg1 + pb1) / 3.0
			// bright2 := (pr2 + pg2 + pb2) / 3.0 // 未使用，注释掉以修复未使用变量错误

			// 只有当两个像素中至少有一个是“亮”的，才进行比较
			// 或者：只有当 TopImg 的该像素是亮的，才去比较 BottomImg 的对应像素（Mask 模式）
			// 这里采用 Mask 模式：以上方提示图标为准，只比较上方图标是白色的区域
			if bright1 > brightnessThreshold {
				sumDiff += math.Abs(pr1 - pr2)
				sumDiff += math.Abs(pg1 - pg2)
				sumDiff += math.Abs(pb1 - pb2)
				count++
			}
		}
	}

	if count == 0 {
		return math.MaxFloat64
	}

	return sumDiff / float64(count) / 3.0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
