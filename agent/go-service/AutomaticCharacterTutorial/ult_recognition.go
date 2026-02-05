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

// UltimateSkillRecognition detects which character has an ultimate ready
// 终结技识别：检测顶部提示图标是否与下方终结技图标匹配，并识别对应按键
type UltimateSkillRecognition struct{}

// Run implements the custom recognition logic
func (r *UltimateSkillRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 1. Parse parameters
	var params struct {
		TopROI    []int   `json:"top_roi"`   // [x, y, w, h] 顶部提示图标区域（白色箭头/图标）
		UltROIs   [][]int `json:"ult_rois"`  // [[x, y, w, h], ...] 下方终结技图标区域列表
		KeyROIs   [][]int `json:"key_rois"`  // [[x, y, w, h], ...] 对应的数字按键区域
		Threshold float64 `json:"threshold"` // 匹配阈值 (0-255)
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to parse params for UltimateSkillRecognition")
		return nil, false
	}

	// Default threshold for image comparison
	if params.Threshold == 0 {
		params.Threshold = 60.0
	}

	img := arg.Img
	if img == nil {
		return nil, false
	}

	// Helper interface for cropping
	type SubImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	subImager, ok := img.(SubImager)
	if !ok {
		log.Error().Msg("Image does not support SubImage")
		return nil, false
	}

	// 2. Crop Top Indicator Image
	if len(params.TopROI) < 4 {
		log.Error().Msg("Invalid TopROI for Ultimate")
		return nil, false
	}
	topRect := image.Rect(params.TopROI[0], params.TopROI[1], params.TopROI[0]+params.TopROI[2], params.TopROI[1]+params.TopROI[3])
	if !topRect.In(img.Bounds()) {
		topRect = topRect.Intersect(img.Bounds())
	}
	if topRect.Empty() {
		return nil, false
	}
	topImg := subImager.SubImage(topRect)

	bestIdx := -1
	minDiff := math.MaxFloat64

	// 3. Compare with Bottom Ult Icons
	// 使用系统临时目录
	tempFile, err := os.CreateTemp("", "maa_ult_template_*.png")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create temp ult template file")
		return nil, false
	}
	tempTemplatePath := tempFile.Name()

	if err := png.Encode(tempFile, topImg); err != nil {
		tempFile.Close()
		os.Remove(tempTemplatePath)
		log.Error().Err(err).Msg("Failed to encode temp ult template image")
		return nil, false
	}
	tempFile.Close()
	defer os.Remove(tempTemplatePath)

	// 遍历所有终结技小图标区域，与顶部提示图标进行对比
	for i, roi := range params.UltROIs {
		if len(roi) < 4 {
			continue
		}

		taskName := "UltMatch_Temp_" + strconv.Itoa(i)
		tmParam := map[string]any{
			taskName: map[string]any{
				"recognition": "TemplateMatch",
				"template":    tempTemplatePath,
				"threshold":   0.7,
				"roi":         roi,
				"method":      5, // TM_CCOEFF_NORMED
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
			if res == nil {
				log.Warn().Str("task", taskName).Msg("RunRecognition returned nil for Ult")
			}
		}

		log.Debug().Int("index", i).Float64("score", score).Msg("Ult TemplateMatch score")

		if score > 0 && (bestIdx == -1 || score > minDiff) {
			minDiff = score
			bestIdx = i
		}
	}

	if minDiff < 0.7 {
		bestIdx = -1
	}

	log.Info().Int("activeIdx", bestIdx).Float64("maxScore", minDiff).Msg("Ultimate skill match result")

	// 4. If match found within threshold
	if bestIdx != -1 {
		matchedROI := params.UltROIs[bestIdx]
		box := maa.Rect{matchedROI[0], matchedROI[1], matchedROI[2], matchedROI[3]}

		keyNum := -1

		// Try OCR first
		if bestIdx < len(params.KeyROIs) {
			keyROI := params.KeyROIs[bestIdx]
			if len(keyROI) >= 4 {
				ocrParam := map[string]any{
					"OCR_Ult_Key": map[string]any{
						"recognition": "OCR",
						"expected":    []string{"1", "2", "3", "4"},
						"roi":         keyROI,
					},
				}

				ocrRes := ctx.RunRecognition("OCR_Ult_Key", img, ocrParam)
				if ocrRes != nil && ocrRes.Hit {
					var ocrDetail struct {
						Text string `json:"text"`
						Best struct {
							Text string `json:"text"`
						} `json:"best"`
					}
					if err := json.Unmarshal([]byte(ocrRes.DetailJson), &ocrDetail); err == nil {
						text := ocrDetail.Text
						if text == "" {
							text = ocrDetail.Best.Text
						}
						text = strings.TrimSpace(text)
						if val, err := strconv.Atoi(text); err == nil {
							keyNum = val
						} else {
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

		// Fallback: Check if the top image itself contains a number (as mentioned by user)
		// If OCR failed, maybe we can assume the index maps to key (0->1, 1->2...)
		// BUT the user said the number 1 appears ABOVE the icon.
		// Since we matched the icon successfully, we know WHICH character it is (bestIdx).
		// The key binding usually follows the character slot position (Index 0 is usually Key 1).
		// So if OCR fails, falling back to index+1 is a safe bet for standard key bindings.

		detailBytes, _ := json.Marshal(map[string]any{
			"index":   bestIdx,
			"key_num": keyNum,
		})

		return &maa.CustomRecognitionResult{
			Box:    box,
			Detail: string(detailBytes),
		}, true
	}

	return nil, false
}
