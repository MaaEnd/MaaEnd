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
	componentName        = "ListCompleteRecognition"
	attachLastText       = "last_text"
	attachUnchangedCount = "unchanged_count"
	// fingerprintSep 拼接整屏 OCR 文本；选不会出现在好友名中的分隔符。
	fingerprintSep = "\n"
)

var _ maa.CustomRecognitionRunner = &Recognition{}
var _ nodeStore = (*maa.Context)(nil)

// Recognition 是通用的列表完成识别器：通过 OCR 指纹是否变化判断列表是否仍在滚动/更新。
//
// 指纹取自目标 OCR 节点命中（优先 Filtered，否则 All）：按纵向（再按横向）排序后
// 只取首尾两条用换行拼接（仅一条时用该条）。比只用 Best 更能发现「顶不变、底已滚」；
// 比整屏 join 更耐中间项 OCR 抖动。
//
// 首次命中（当前节点 attach.last_text 为空）时，只要能抽出指纹即返回 true，并写入指纹与框；
// 之后若指纹与 attach.last_text 不一致则更新 attach、清零 unchanged_count 并返回 true；
// 若一致则累加 unchanged_count：当 unchanged_count > retry（默认 0）时返回 false（视为到底），
// 否则仍返回 true（确认重试，便于 Pipeline 再滑一次）。
//
// node 可为 OCR 节点，或 And 节点。对 And 节点，按节点自身 box_index（默认 0）
// 从 CombinedResult 中选取子识别结果，再从该结果提取 OCR——目标解析复用 recogtarget。
type Recognition struct{}

type params struct {
	// Node 为 OCR 节点名，或内部必须包含 OCR 的 And 节点名。
	Node string `json:"node"`
	// Retry 为指纹判定相等后仍返回 true 的次数；超过该次数才返回 false。默认 0（不重试）。
	Retry int `json:"retry"`
}

type ocrHit struct {
	Text string
	Box  maa.Rect
}

type attachState struct {
	LastText       string
	UnchangedCount int
}

// listCompleteOutcome 是指纹对比后的判定结果（不含框，框由 OCR 命中提供）。
type listCompleteOutcome struct {
	Next           attachState
	Hit            bool
	UnchangedRetry bool
	Detail         map[string]any
}

// nodeStore 抽象 attach 读写，便于单测用 fake 跨多次调用保留状态。
type nodeStore interface {
	GetNodeJSON(nodeName string) (string, error)
	OverridePipeline(pipelineOverride any) error
}

// evaluateListComplete 根据上次指纹与 retry 判定是否继续命中。
func evaluateListComplete(
	state attachState,
	fingerprint string,
	currentNode string,
	targetNode string,
	retry int,
) listCompleteOutcome {
	if state.LastText != "" && state.LastText == fingerprint {
		unchanged := state.UnchangedCount + 1
		next := attachState{
			LastText:       fingerprint,
			UnchangedCount: unchanged,
		}
		if unchanged > retry {
			return listCompleteOutcome{Next: next, Hit: false}
		}
		return listCompleteOutcome{
			Next:           next,
			Hit:            true,
			UnchangedRetry: true,
			Detail: map[string]any{
				"node":            currentNode,
				"target_node":     targetNode,
				"text":            fingerprint,
				"last_text":       state.LastText,
				"unchanged_count": unchanged,
				"retry":           retry,
				"unchanged_retry": true,
			},
		}
	}

	return listCompleteOutcome{
		Next: attachState{
			LastText:       fingerprint,
			UnchangedCount: 0,
		},
		Hit: true,
		Detail: map[string]any{
			"node":            currentNode,
			"target_node":     targetNode,
			"text":            fingerprint,
			"last_text":       state.LastText,
			"first_run":       state.LastText == "",
			"unchanged_count": 0,
			"retry":           retry,
		},
	}
}

// applyFingerprint 加载 attach → 判定 → 写回，模拟 Run 中与 Context OCR 无关的重试核心。
func applyFingerprint(
	store nodeStore,
	currentNode string,
	targetNode string,
	fingerprint string,
	retry int,
) (listCompleteOutcome, error) {
	state, err := loadAttachState(store, currentNode)
	if err != nil {
		return listCompleteOutcome{}, err
	}
	outcome := evaluateListComplete(state, fingerprint, currentNode, targetNode, retry)
	if err := saveAttachState(store, currentNode, outcome.Next); err != nil {
		return listCompleteOutcome{}, err
	}
	return outcome, nil
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
			Str("node", currentNode).
			Str("target_node", p.Node).
			Msg("node must be OCR or And whose box_index target contains OCR")
		return nil, false
	}

	detail, err := ctx.RunRecognition(p.Node, arg.Img)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Debug().
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Msg("OCR/And recognition missed")
		return nil, false
	}

	hit, err := extractOCRFingerprintFromNode(ctx, p.Node, detail)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Msg("failed to extract OCR fingerprint from recognition result")
		return nil, false
	}

	state, err := loadAttachState(ctx, currentNode)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Msg("failed to load attach state")
		return nil, false
	}

	outcome := evaluateListComplete(state, hit.Text, currentNode, p.Node, p.Retry)
	if err := saveAttachState(ctx, currentNode, outcome.Next); err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Str("text", hit.Text).
			Int("unchanged_count", outcome.Next.UnchangedCount).
			Msg("failed to save attach state")
		return nil, false
	}

	if !outcome.Hit {
		log.Info().
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Str("text", hit.Text).
			Int("unchanged_count", outcome.Next.UnchangedCount).
			Int("retry", p.Retry).
			Msg("OCR fingerprint unchanged, list complete")
		return nil, false
	}

	if outcome.UnchangedRetry {
		log.Info().
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Str("text", hit.Text).
			Int("unchanged_count", outcome.Next.UnchangedCount).
			Int("retry", p.Retry).
			Msg("OCR fingerprint unchanged, retrying")
	} else {
		log.Info().
			Str("component", componentName).
			Str("node", currentNode).
			Str("target_node", p.Node).
			Str("text", hit.Text).
			Str("last_text", state.LastText).
			Bool("first_run", state.LastText == "").
			Msg("OCR fingerprint accepted")
	}

	return &maa.CustomRecognitionResult{
		Box:    hit.Box,
		Detail: marshalDetail(outcome.Detail),
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
	if p.Retry < 0 {
		return nil, fmt.Errorf("retry must be >= 0")
	}
	return &p, nil
}

// marshalDetail 序列化识别 Detail；失败时记日志并以空串继续，不影响命中判定。
func marshalDetail(payload map[string]any) string {
	detailJSON, err := json.Marshal(payload)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Interface("payload", payload).
			Msg("failed to marshal recognition detail")
		return ""
	}
	return string(detailJSON)
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

func loadAttachState(store nodeStore, nodeName string) (attachState, error) {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return attachState{}, err
	}
	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return attachState{}, err
	}
	if wrapper.Attach == nil {
		return attachState{}, nil
	}

	var state attachState
	if rawText, ok := wrapper.Attach[attachLastText]; ok && len(rawText) > 0 && string(rawText) != "null" {
		var text string
		if err := json.Unmarshal(rawText, &text); err != nil {
			return attachState{}, fmt.Errorf("attach.%s must be string: %w", attachLastText, err)
		}
		state.LastText = strings.TrimSpace(text)
	}
	if rawCount, ok := wrapper.Attach[attachUnchangedCount]; ok && len(rawCount) > 0 && string(rawCount) != "null" {
		var count int
		if err := json.Unmarshal(rawCount, &count); err != nil {
			return attachState{}, fmt.Errorf("attach.%s must be int: %w", attachUnchangedCount, err)
		}
		if count < 0 {
			count = 0
		}
		state.UnchangedCount = count
	}
	return state, nil
}

func saveAttachState(store nodeStore, nodeName string, state attachState) error {
	raw, err := store.GetNodeJSON(nodeName)
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
	wrapper.Attach[attachLastText] = state.LastText
	wrapper.Attach[attachUnchangedCount] = state.UnchangedCount

	return store.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": wrapper.Attach,
		},
	})
}
