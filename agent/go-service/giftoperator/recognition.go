package giftoperator

import (
	"encoding/json"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &CandidateRecognition{}

// CandidateRecognition 根据头像及信赖选择尚未成功送礼或排除的干员。
// 每次只返回一位干员；记录身份及点击由 Pipeline 的后续节点负责。
type CandidateRecognition struct{}

type candidate struct {
	Operator string   `json:"operator"`
	Names    []string `json:"names,omitempty"`
	Template string   `json:"template,omitempty"`
	Portrait string   `json:"portrait,omitempty"`
	Box      maa.Rect `json:"-"`
}

// Run implements maa.CustomRecognitionRunner.
func (r *CandidateRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	s, err := loadSession(ctx)
	if err != nil || s.Remaining <= 0 {
		log.Error().Err(err).Msg("gift session unavailable")
		return nil, false
	}
	raw, err := ctx.GetNodeJSON("GiftOperatorSendCandidate")
	if err != nil {
		log.Error().Err(err).Msg("read gift candidates failed")
		return nil, false
	}
	var config struct {
		Attach struct {
			Operators []string `json:"operators"`
			Templates []string `json:"templates"`
			Generic   bool     `json:"generic"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		log.Error().Err(err).Msg("parse gift candidates failed")
		return nil, false
	}
	var best *candidate
	var trust int
	if config.Attach.Generic {
		geometry, geometryErr := readPortraitGeometry(ctx)
		if geometryErr != nil {
			log.Error().Err(geometryErr).Msg("read gift contact geometry failed")
			return nil, false
		}
		best, trust, err = recognizeGenericCandidate(ctx, arg.Img, s, geometry,
			arg.CurrentTaskName == "GiftOperatorSendClickCandidate")
	} else {
		var candidates []candidate
		for _, node := range config.Attach.Operators {
			info, template, readErr := readCandidate(ctx, node)
			if readErr != nil {
				log.Error().Err(readErr).Str("node", node).Msg("read gift operator failed")
				return nil, false
			}
			if slices.Contains(s.Completed, info.Operator) || slices.Contains(s.Excluded, info.Operator) {
				continue
			}
			if len(config.Attach.Templates) > 0 && !slices.Contains(config.Attach.Templates, template) {
				continue
			}
			// 记录身份后再次识别同一头像，避免使用上一帧的点击框。
			if arg.CurrentTaskName == "GiftOperatorSendClickCandidate" && info.Operator != s.Pending {
				continue
			}
			detail, matchErr := ctx.RunRecognition("GiftOperatorSelectSpecifiedOp", arg.Img, map[string]any{
				"GiftOperatorSelectSpecifiedOp": map[string]any{"template": []string{template}},
			})
			if matchErr != nil {
				log.Error().Err(matchErr).Str("operator", info.Operator).Msg("match gift operator failed")
				return nil, false
			}
			if detail == nil || !detail.Hit {
				continue
			}
			info.Box = detail.Box
			candidates = append(candidates, info)
		}
		best, trust, err = selectGiftCandidate(candidates, func(info candidate) (int, bool, error) {
			return recognizeCandidateTrust(ctx, arg.Img, info.Box)
		})
	}
	if err != nil {
		log.Error().Err(err).Str("component", "GiftOperatorCandidateRecognition").
			Msg("recognize candidate trust failed")
		return nil, false
	}
	if best == nil {
		return nil, false
	}
	log.Info().Str("component", "GiftOperatorCandidateRecognition").
		Str("operator", best.Operator).Int("trust", trust).Interface("box", best.Box).
		Msg("selected gift recipient")
	encoded, err := json.Marshal(best)
	if err != nil {
		return nil, false
	}
	return &maa.CustomRecognitionResult{Box: best.Box, Detail: string(encoded)}, true
}

// selectGiftCandidate 按卡片的行、列顺序选择信赖未满的干员。
// 头像模板的顶部会相差几个像素，同排卡片须先按横坐标排序。
func selectGiftCandidate(candidates []candidate, readTrust func(candidate) (int, bool, error)) (*candidate, int, error) {
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.Box[1] != b.Box[1] {
			return a.Box[1] - b.Box[1]
		}
		return a.Box[0] - b.Box[0]
	})
	for start := 0; start < len(candidates); {
		anchor := candidates[start].Box
		end := start + 1
		// 同排头像的垂直重叠至少达到较小头像高度的一半。
		for end < len(candidates) && candidates[end].Box[1]-anchor[1] < min(anchor[3], candidates[end].Box[3])/2 {
			end++
		}
		slices.SortFunc(candidates[start:end], func(a, b candidate) int { return a.Box[0] - b.Box[0] })
		start = end
	}
	for _, info := range candidates {
		trust, hit, err := readTrust(info)
		if err != nil {
			return nil, 0, fmt.Errorf("read trust for %s: %w", info.Operator, err)
		}
		if !hit {
			log.Warn().Str("component", "GiftOperatorCandidateRecognition").Str("operator", info.Operator).
				Interface("box", info.Box).Msg("candidate trust could not be confirmed")
			continue
		}
		if trust < 0 || trust > 200 {
			return nil, 0, fmt.Errorf("invalid candidate trust %d for %s", trust, info.Operator)
		}
		if trust == 200 {
			log.Info().Str("component", "GiftOperatorCandidateRecognition").Str("operator", info.Operator).
				Int("trust", trust).Interface("box", info.Box).Msg("excluded recipient with maximum trust")
			continue
		}
		return &info, trust, nil
	}
	return nil, 0, nil
}

type recognitionRunner interface {
	RunRecognition(string, image.Image, ...any) (*maa.RecognitionDetail, error)
}

func recognizeCandidateTrust(runner recognitionRunner, img image.Image, box maa.Rect) (int, bool, error) {
	// 720p 联络卡片的标准头像为 80×72，信赖数字位于其右下方。
	// ADB 使用较大头像模板，按实际头像尺寸缩放，避免读到相邻卡片。
	roi := maa.Rect{box[0] + box[2]*30/80, box[1] + box[3]*84/72, box[2] * 40 / 80, box[3] * 18 / 72}
	if box[2] <= 0 || box[3] <= 0 || roi[2] <= 0 || roi[3] <= 0 ||
		!image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3]).In(img.Bounds()) {
		return 0, false, nil
	}
	detail, err := runner.RunRecognition("GiftOperatorTrustValue", img, map[string]any{
		"GiftOperatorTrustValue": map[string]any{"roi": roi, "roi_offset": maa.Rect{}, "expected": []string{`^[0-9]+[%％]?$`}},
	})
	if err != nil {
		return 0, false, err
	}
	if detail == nil || !detail.Hit || detail.Algorithm != "OCR" {
		return 0, false, nil
	}
	var result struct {
		Best *maa.OCRResult `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &result); err != nil {
		return 0, false, err
	}
	if result.Best == nil {
		return 0, false, nil
	}
	text := strings.Join(strings.Fields(result.Best.Text), "")
	text = strings.ReplaceAll(text, "％", "%")
	text = strings.TrimSuffix(text, "%")
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return 0, false, nil
		}
	}
	trust, err := strconv.Atoi(text)
	if err != nil || trust < 0 || trust > 200 {
		return 0, false, nil
	}
	return trust, true, nil
}

// readCandidate 复用收礼身份节点的模板和五语言姓名，不另建一份干员映射。
func readCandidate(store nodeStore, node string) (candidate, string, error) {
	raw, err := store.GetNodeJSON(node)
	if err != nil {
		return candidate{}, "", err
	}
	type identityPatch struct {
		Patch map[string]struct {
			Expected []string `json:"expected"`
		} `json:"patch"`
	}
	var metadata struct {
		Template          []string        `json:"template"`
		CustomActionParam identityPatch   `json:"custom_action_param"`
		Recognition       json.RawMessage `json:"recognition"`
		Action            json.RawMessage `json:"action"`
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return candidate{}, "", err
	}
	// 同时支持 V2 对象和扁平字段；扁平格式的 recognition/action 是字符串。
	if len(metadata.Recognition) > 0 && metadata.Recognition[0] == '{' {
		var recognition struct {
			Param struct {
				Template []string `json:"template"`
			} `json:"param"`
		}
		if err := json.Unmarshal(metadata.Recognition, &recognition); err != nil {
			return candidate{}, "", err
		}
		metadata.Template = recognition.Param.Template
	}
	if len(metadata.Action) > 0 && metadata.Action[0] == '{' {
		var action struct {
			Param struct {
				CustomActionParam identityPatch `json:"custom_action_param"`
			} `json:"param"`
		}
		if err := json.Unmarshal(metadata.Action, &action); err != nil {
			return candidate{}, "", err
		}
		metadata.CustomActionParam = action.Param.CustomActionParam
	}
	templates := metadata.Template
	names := metadata.CustomActionParam.Patch["GiftOperatorReceiveName"].Expected
	if len(templates) != 1 || len(names) == 0 {
		return candidate{}, "", fmt.Errorf("operator metadata missing in %s", node)
	}
	template := templates[0]
	filename := template[strings.LastIndex(template, "/")+1:]
	id := strings.TrimSuffix(filename, ".png")
	if id == "" {
		return candidate{}, "", fmt.Errorf("operator ID missing in %s", node)
	}
	return candidate{Operator: id, Names: names}, template, nil
}
