package listcomplete

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName  = "ListCompleteRecognition"
	attachLastText = "last_text"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是通用的列表完成识别器：通过 OCR 文本是否变化判断列表是否仍在滚动/更新。
//
// 首次命中（当前节点 attach.last_text 为空）时，只要 OCR 成功即返回 true，并写入文字与框；
// 之后若文字与 attach.last_text 一致则返回 false（视为列表已到底/未变化），
// 不一致则更新 attach 并返回 true。
//
// node 可为 OCR 节点，或 And 节点。对 And 节点，按节点自身 box_index（默认 0）
// 从 CombinedResult 中选取子识别结果，再从该结果提取 OCR 文本与框——目标解析复用 recogtarget。
type Recognition struct{}

type params struct {
	// Node 为 OCR 节点名，或内部必须包含 OCR 的 And 节点名。
	Node string `json:"node"`
}

type ocrHit struct {
	Text string
	Box  maa.Rect
}

// Run implements maa.CustomRecognitionRunner.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().
			Str("component", componentName).
			Msg("nil context or arg")
		return nil, false
	}

	p, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	currentNode := strings.TrimSpace(arg.CurrentTaskName)
	if currentNode == "" {
		log.Error().
			Str("component", componentName).
			Msg("current task name is empty")
		return nil, false
	}

	if err := ensureNodeContainsOCR(ctx, p.Node); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", p.Node).
			Msg("node must be OCR or And whose box_index target contains OCR")
		return nil, false
	}

	detail, err := ctx.RunRecognition(p.Node, arg.Img)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", p.Node).
			Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Debug().
			Str("component", componentName).
			Str("node", p.Node).
			Msg("OCR/And recognition missed")
		return nil, false
	}

	hit, err := extractOCRHitFromNode(ctx, p.Node, detail)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", p.Node).
			Msg("failed to extract OCR from recognition result")
		return nil, false
	}

	lastText, err := loadLastText(ctx, currentNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Msg("failed to load attach.last_text")
		return nil, false
	}

	if lastText != "" && lastText == hit.Text {
		log.Info().
			Str("component", componentName).
			Str("node", p.Node).
			Str("text", hit.Text).
			Msg("OCR text unchanged, list complete")
		return nil, false
	}

	if err := saveLastText(ctx, currentNode, hit.Text); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("text", hit.Text).
			Msg("failed to save attach.last_text")
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"node":      p.Node,
		"text":      hit.Text,
		"last_text": lastText,
		"first_run": lastText == "",
	})

	log.Info().
		Str("component", componentName).
		Str("node", p.Node).
		Str("text", hit.Text).
		Str("last_text", lastText).
		Bool("first_run", lastText == "").
		Msg("OCR text accepted")

	return &maa.CustomRecognitionResult{
		Box:    hit.Box,
		Detail: string(detailJSON),
	}, true
}

func parseParams(raw string) (*params, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("node is required")
	}

	var p params
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	p.Node = strings.TrimSpace(p.Node)
	if p.Node == "" {
		return nil, fmt.Errorf("node is required")
	}
	return &p, nil
}

func ensureNodeContainsOCR(ctx *maa.Context, nodeName string) error {
	effectiveType, err := recogtarget.EffectiveType(ctx, nodeName)
	if err != nil {
		return err
	}
	if effectiveType != "OCR" {
		return fmt.Errorf("node %s effective recognition type is %q, want OCR", nodeName, effectiveType)
	}
	return nil
}

// extractOCRHitFromNode 先用 recogtarget 按 box_index 选中目标子结果，再提取 OCR 文本与框。
func extractOCRHitFromNode(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (ocrHit, error) {
	selected, err := recogtarget.SelectDetail(ctx, nodeName, detail)
	if err != nil {
		return ocrHit{}, err
	}
	hit, ok := ocrHitFromResults(selected)
	if !ok {
		return ocrHit{}, fmt.Errorf("no ocr result found")
	}
	return hit, nil
}

func ocrHitFromResults(detail *maa.RecognitionDetail) (ocrHit, bool) {
	if detail == nil || detail.Results == nil {
		return ocrHit{}, false
	}

	try := func(result *maa.RecognitionResult) (ocrHit, bool) {
		if result == nil {
			return ocrHit{}, false
		}
		ocrResult, ok := result.AsOCR()
		if !ok {
			return ocrHit{}, false
		}
		text := strings.TrimSpace(ocrResult.Text)
		if text == "" {
			return ocrHit{}, false
		}
		box := ocrResult.Box
		if !rectValid(box) && rectValid(detail.Box) {
			box = detail.Box
		}
		if !rectValid(box) {
			return ocrHit{}, false
		}
		return ocrHit{Text: text, Box: box}, true
	}

	if hit, ok := try(detail.Results.Best); ok {
		return hit, true
	}
	for _, result := range detail.Results.Filtered {
		if hit, ok := try(result); ok {
			return hit, true
		}
	}
	for _, result := range detail.Results.All {
		if hit, ok := try(result); ok {
			return hit, true
		}
	}
	return ocrHit{}, false
}

func rectValid(box maa.Rect) bool {
	return box[2] > 0 && box[3] > 0
}

func loadLastText(ctx *maa.Context, nodeName string) (string, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return "", err
	}
	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return "", err
	}
	if wrapper.Attach == nil {
		return "", nil
	}
	rawText, ok := wrapper.Attach[attachLastText]
	if !ok || len(rawText) == 0 || string(rawText) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(rawText, &text); err != nil {
		return "", fmt.Errorf("attach.%s must be string: %w", attachLastText, err)
	}
	return strings.TrimSpace(text), nil
}

func saveLastText(ctx *maa.Context, nodeName string, text string) error {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return err
	}

	var wrapper struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return err
	}
	if wrapper.Attach == nil {
		wrapper.Attach = make(map[string]any)
	}
	wrapper.Attach[attachLastText] = text

	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": wrapper.Attach,
		},
	})
}
