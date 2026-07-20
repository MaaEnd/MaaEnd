package listcomplete

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName  = "ListCompleteRecognition"
	attachLastText = "last_text"
	// fingerprintSep 拼接整屏 OCR 文本；选不会出现在好友名中的分隔符。
	fingerprintSep = "\n"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是通用的列表完成识别器：通过 OCR 指纹是否变化判断列表是否仍在滚动/更新。
//
// 指纹取自目标 OCR 节点命中（优先 Filtered，否则 All）：按纵向（再按横向）排序后
// 只取首尾两条用换行拼接（仅一条时用该条）。比只用 Best 更能发现「顶不变、底已滚」；
// 比整屏 join 更耐中间项 OCR 抖动。
//
// 首次命中（当前节点 attach.last_text 为空）时，只要能抽出指纹即返回 true，并写入指纹与框；
// 之后若指纹与 attach.last_text 一致则返回 false（视为列表已到底/未变化），
// 不一致则更新 attach 并返回 true。
//
// node 可为 OCR 节点，或 And 节点。对 And 节点，按节点自身 box_index（默认 0）
// 从 CombinedResult 中选取子识别结果，再从该结果提取 OCR——目标解析复用 recogtarget。
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

	hit, err := extractOCRFingerprintFromNode(ctx, p.Node, detail)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", p.Node).
			Msg("failed to extract OCR fingerprint from recognition result")
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
			Msg("OCR fingerprint unchanged, list complete")
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
		Msg("OCR fingerprint accepted")

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

// extractOCRFingerprintFromNode 先用 recogtarget 按 box_index 选中目标子结果，
// 再收集整屏 OCR 命中并生成指纹（纵向排序后 join）。
func extractOCRFingerprintFromNode(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (ocrHit, error) {
	selected, err := recogtarget.SelectDetail(ctx, nodeName, detail)
	if err != nil {
		return ocrHit{}, err
	}
	hit, ok := fingerprintFromOCRResults(selected)
	if !ok {
		return ocrHit{}, fmt.Errorf("no ocr result found")
	}
	return hit, nil
}

// fingerprintFromOCRResults 收集 Filtered（空则 All）中 OCR 命中，
// 按 box 纵向再横向排序后取首尾生成指纹；Box 取最上方一条。
func fingerprintFromOCRResults(detail *maa.RecognitionDetail) (ocrHit, bool) {
	hits := collectOCRHits(detail)
	if len(hits) == 0 {
		return ocrHit{}, false
	}
	return buildFingerprint(hits), true
}

func collectOCRHits(detail *maa.RecognitionDetail) []ocrHit {
	if detail == nil || detail.Results == nil {
		return nil
	}

	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}

	hits := make([]ocrHit, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		ocrResult, ok := result.AsOCR()
		if !ok {
			continue
		}
		text := strings.TrimSpace(ocrResult.Text)
		if text == "" {
			continue
		}
		box := ocrResult.Box
		if !rectValid(box) && rectValid(detail.Box) {
			box = detail.Box
		}
		if !rectValid(box) {
			continue
		}
		hits = append(hits, ocrHit{Text: text, Box: box})
	}
	return hits
}

func buildFingerprint(hits []ocrHit) ocrHit {
	sorted := append([]ocrHit(nil), hits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		yi := sorted[i].Box[1]
		yj := sorted[j].Box[1]
		if yi != yj {
			return yi < yj
		}
		return sorted[i].Box[0] < sorted[j].Box[0]
	})

	// 只取首尾：中间漏检/多检不影响；底部换人仍能与 Best-only 区分开。
	texts := []string{sorted[0].Text}
	if last := sorted[len(sorted)-1]; len(sorted) > 1 {
		texts = append(texts, last.Text)
	}
	return ocrHit{
		Text: strings.Join(texts, fingerprintSep),
		Box:  sorted[0].Box,
	}
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
