package expendable

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "ExpendableRecognition"
	attachVisited = "visited"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是通用消费性识别器：
// 读 attach.visited → 写入 OCR expected 负向黑名单 → 跑 candidate → 取文案入库。
//
// candidate 应为 OCR，或 And（box_index 指向文案 OCR）。
// 只覆盖 box_index 指向的命名 OCR；点击框仍是 candidate 命中框。
// 可选 visited_node：黑名单读写走该节点的 attach.visited，便于多个消费节点共享。
type Recognition struct{}

type params struct {
	Candidate string `json:"candidate"`
	// VisitedNode 若非空，则从该节点的 attach.visited 读写黑名单；否则用当前 Custom 节点。
	VisitedNode string `json:"visited_node"`
}

// Run implements maa.CustomRecognitionRunner.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("nil context or arg")
		return nil, false
	}

	p, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("parse params failed")
		return nil, false
	}
	self := strings.TrimSpace(arg.CurrentTaskName)
	if self == "" {
		log.Error().Str("component", componentName).Msg("current task name is empty")
		return nil, false
	}
	visitedOwner := p.VisitedNode
	if visitedOwner == "" {
		visitedOwner = self
	}

	ocrNode, err := keyOCRNode(ctx, p.Candidate)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("candidate", p.Candidate).Msg("resolve key ocr failed")
		return nil, false
	}

	visited, err := loadVisited(ctx, visitedOwner)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("node", visitedOwner).Msg("load visited failed")
		return nil, false
	}
	if err := injectBlacklist(ctx, ocrNode, visited); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("inject expected blacklist failed")
		return nil, false
	}

	detail, err := ctx.RunRecognition(p.Candidate, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("candidate", p.Candidate).Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Info().Str("component", componentName).Str("candidate", p.Candidate).Strs("visited", visited).Msg("no unvisited candidate")
		return nil, false
	}

	text, ok := extractText(ctx, p.Candidate, detail)
	if !ok || text == "" {
		log.Warn().Str("component", componentName).Str("candidate", p.Candidate).Msg("hit but text missing")
		return nil, false
	}

	newVisited := append(append([]string{}, visited...), text)
	if err := saveVisited(ctx, visitedOwner, newVisited); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("text", text).Msg("save visited failed")
		return nil, false
	}

	log.Info().
		Str("component", componentName).
		Str("text", text).
		Str("visited_node", visitedOwner).
		Interface("box", detail.Box).
		Strs("visited", newVisited).
		Msg("selected unvisited candidate")

	detailJSON, _ := json.Marshal(map[string]string{"text": text})
	return &maa.CustomRecognitionResult{Box: detail.Box, Detail: string(detailJSON)}, true
}

func parseParams(raw string) (params, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return params{}, fmt.Errorf("custom_recognition_param is empty")
	}
	var p params
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return params{}, err
	}
	p.Candidate = strings.TrimSpace(p.Candidate)
	if p.Candidate == "" {
		return params{}, fmt.Errorf("candidate is required")
	}
	p.VisitedNode = strings.TrimSpace(p.VisitedNode)
	return p, nil
}

// keyOCRNode 返回需要注入黑名单的命名 OCR：And 取 box_index，OCR 取自身。
func keyOCRNode(ctx *maa.Context, candidate string) (string, error) {
	raw, err := ctx.GetNodeJSON(candidate)
	if err != nil {
		return "", err
	}
	fields, err := recogtarget.ParseNodeJSON([]byte(raw))
	if err != nil {
		return "", err
	}

	var ocrNode string
	switch strings.ToLower(fields.Type) {
	case "ocr":
		ocrNode = candidate
	case "and":
		ocrNode, err = namedChild(fields.AllOf, fields.BoxIndex)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("candidate type %q unsupported; want OCR/And", fields.Type)
	}
	if err := ensureKeyIsOCR(ctx, ocrNode); err != nil {
		return "", err
	}
	return ocrNode, nil
}

func ensureKeyIsOCR(ctx *maa.Context, nodeName string) error {
	effectiveType, err := recogtarget.EffectiveType(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("resolve key node %s type: %w", nodeName, err)
	}
	if effectiveType != "OCR" {
		return fmt.Errorf("key node %s effective recognition type is %q, want OCR (And.box_index must point at text OCR)", nodeName, effectiveType)
	}
	return nil
}

func namedChild(allOf []json.RawMessage, boxIndex int) (string, error) {
	if boxIndex < 0 || boxIndex >= len(allOf) {
		return "", fmt.Errorf("box_index %d out of range", boxIndex)
	}
	var name string
	if err := json.Unmarshal(allOf[boxIndex], &name); err != nil {
		return "", fmt.Errorf("box_index target must be a named OCR node ref")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("box_index target name is empty")
	}
	return name, nil
}

func injectBlacklist(ctx *maa.Context, ocrNode string, visited []string) error {
	raw, err := ctx.GetNodeJSON(ocrNode)
	if err != nil {
		return err
	}
	base, err := readExpected(raw)
	if err != nil {
		return fmt.Errorf("node %s: %w", ocrNode, err)
	}
	if len(base) == 0 {
		base = []string{".+"}
	}
	// 只覆盖 expected；order_by 等字段由框架保留原值。
	return ctx.OverridePipeline(map[string]any{
		ocrNode: map[string]any{"expected": withBlacklist(base, visited)},
	})
}

func readExpected(raw string) ([]string, error) {
	var node struct {
		Expected    any             `json:"expected"`
		Recognition json.RawMessage `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return nil, err
	}
	expected, err := asStringList(node.Expected)
	if err != nil {
		return nil, err
	}
	if len(expected) == 0 && len(node.Recognition) > 0 && node.Recognition[0] == '{' {
		var v2 struct {
			Param struct {
				Expected any `json:"expected"`
			} `json:"param"`
		}
		if err := json.Unmarshal(node.Recognition, &v2); err != nil {
			return nil, err
		}
		expected, err = asStringList(v2.Param.Expected)
		if err != nil {
			return nil, err
		}
	}
	for i := range expected {
		expected[i] = stripBlacklistPrefix(strings.TrimSpace(expected[i]))
	}
	return expected, nil
}

func asStringList(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		return []string{strings.TrimSpace(v)}, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected item is not string")
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected has unsupported type %T", raw)
	}
}

var blacklistPrefixRe = regexp.MustCompile(`^\^\(\?!\(\?:(?:[^\\]|\\.)*?\)\$\)`)

func stripBlacklistPrefix(pattern string) string {
	if loc := blacklistPrefixRe.FindStringIndex(pattern); loc != nil {
		return pattern[loc[1]:]
	}
	return pattern
}

func withBlacklist(base, visited []string) []string {
	escaped := make([]string, 0, len(visited))
	for _, v := range visited {
		v = strings.TrimSpace(v)
		if v != "" {
			escaped = append(escaped, regexp.QuoteMeta(v))
		}
	}
	prefix := ""
	if len(escaped) > 0 {
		prefix = fmt.Sprintf("^(?!(?:%s)$)", strings.Join(escaped, "|"))
	}
	out := make([]string, 0, len(base))
	for _, item := range base {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, prefix+item)
		}
	}
	if len(out) == 0 {
		return []string{prefix + ".+"}
	}
	return out
}

func extractText(ctx *maa.Context, candidate string, detail *maa.RecognitionDetail) (string, bool) {
	raw, err := ctx.GetNodeJSON(candidate)
	if err != nil {
		return "", false
	}
	selected, err := recogtarget.SelectDetailFromJSON([]byte(raw), detail)
	if err != nil {
		return "", false
	}
	return ocrText(selected)
}

func ocrText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || detail.Results == nil {
		return "", false
	}
	try := func(result *maa.RecognitionResult) (string, bool) {
		if result == nil {
			return "", false
		}
		ocr, ok := result.AsOCR()
		if !ok {
			return "", false
		}
		text := strings.TrimSpace(ocr.Text)
		return text, text != ""
	}
	if text, ok := try(detail.Results.Best); ok {
		return text, true
	}
	for _, result := range detail.Results.Filtered {
		if text, ok := try(result); ok {
			return text, true
		}
	}
	return "", false
}

func loadVisited(ctx *maa.Context, nodeName string) ([]string, error) {
	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Attach struct {
			Visited []string `json:"visited"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(wrapper.Attach.Visited))
	seen := map[string]struct{}{}
	for _, v := range wrapper.Attach.Visited {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

func saveVisited(ctx *maa.Context, nodeName string, visited []string) error {
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
		wrapper.Attach = map[string]any{}
	}
	wrapper.Attach[attachVisited] = visited
	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{"attach": wrapper.Attach},
	})
}
