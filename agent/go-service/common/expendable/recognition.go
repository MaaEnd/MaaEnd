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
// candidate 应为 OCR，或 And（box_index 指向文案 OCR），或 Or(And...)。
// 只覆盖各 And.box_index 指向的命名 OCR；点击框仍是 candidate 命中框。
type Recognition struct{}

type params struct {
	Candidate string `json:"candidate"`
}

// Run implements maa.CustomRecognitionRunner.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("nil context or arg")
		return nil, false
	}

	candidate, err := parseCandidate(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("parse params failed")
		return nil, false
	}
	self := strings.TrimSpace(arg.CurrentTaskName)
	if self == "" {
		log.Error().Str("component", componentName).Msg("current task name is empty")
		return nil, false
	}

	ocrNodes, err := keyOCRNodes(ctx, candidate)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("candidate", candidate).Msg("resolve key ocr failed")
		return nil, false
	}

	visited, err := loadVisited(ctx, self)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("node", self).Msg("load visited failed")
		return nil, false
	}
	if err := injectBlacklist(ctx, ocrNodes, visited); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("inject expected blacklist failed")
		return nil, false
	}

	detail, err := ctx.RunRecognition(candidate, arg.Img)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("candidate", candidate).Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Info().Str("component", componentName).Str("candidate", candidate).Strs("visited", visited).Msg("no unvisited candidate")
		return nil, false
	}

	text, ok := extractText(ctx, candidate, detail)
	if !ok || text == "" {
		log.Warn().Str("component", componentName).Str("candidate", candidate).Msg("hit but text missing")
		return nil, false
	}

	newVisited := append(append([]string{}, visited...), text)
	if err := saveVisited(ctx, self, newVisited); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("text", text).Msg("save visited failed")
		return nil, false
	}

	log.Info().
		Str("component", componentName).
		Str("text", text).
		Interface("box", detail.Box).
		Strs("visited", newVisited).
		Msg("selected unvisited candidate")

	detailJSON, _ := json.Marshal(map[string]string{"text": text})
	return &maa.CustomRecognitionResult{Box: detail.Box, Detail: string(detailJSON)}, true
}

func parseCandidate(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("custom_recognition_param is empty")
	}
	var p params
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return "", err
	}
	p.Candidate = strings.TrimSpace(p.Candidate)
	if p.Candidate == "" {
		return "", fmt.Errorf("candidate is required")
	}
	return p.Candidate, nil
}

// keyOCRNodes 收集需要注入黑名单的命名 OCR：And 取 box_index；Or 收集各 And 的 box_index。
func keyOCRNodes(ctx *maa.Context, candidate string) ([]string, error) {
	raw, err := ctx.GetNodeJSON(candidate)
	if err != nil {
		return nil, err
	}
	fields, err := recogtarget.ParseNodeJSON([]byte(raw))
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(fields.Type) {
	case "ocr":
		return []string{candidate}, nil
	case "and":
		name, err := namedChild(fields.AllOf, fields.BoxIndex)
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	case "or":
		anyOf, err := decodeAnyOf([]byte(raw))
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(anyOf))
		seen := map[string]struct{}{}
		for _, child := range anyOf {
			childRaw, err := ctx.GetNodeJSON(child)
			if err != nil {
				return nil, err
			}
			childFields, err := recogtarget.ParseNodeJSON([]byte(childRaw))
			if err != nil {
				return nil, err
			}
			if !strings.EqualFold(childFields.Type, "And") {
				return nil, fmt.Errorf("or child %s must be And", child)
			}
			name, err := namedChild(childFields.AllOf, childFields.BoxIndex)
			if err != nil {
				return nil, fmt.Errorf("or child %s: %w", child, err)
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("or candidate has no key ocr")
		}
		return out, nil
	default:
		return nil, fmt.Errorf("candidate type %q unsupported; want OCR/And/Or", fields.Type)
	}
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

func decodeAnyOf(nodeRaw []byte) ([]string, error) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(nodeRaw, &node); err != nil {
		return nil, err
	}
	raw := node["any_of"]
	if rec := node["recognition"]; len(rec) > 0 && rec[0] == '{' {
		var v2 struct {
			Param struct {
				AnyOf json.RawMessage `json:"any_of"`
			} `json:"param"`
		}
		_ = json.Unmarshal(rec, &v2)
		if len(raw) == 0 {
			raw = v2.Param.AnyOf
		}
	}
	var refs []string
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, fmt.Errorf("any_of must be named node refs: %w", err)
	}
	return refs, nil
}

func injectBlacklist(ctx *maa.Context, ocrNodes []string, visited []string) error {
	override := make(map[string]any, len(ocrNodes))
	for _, node := range ocrNodes {
		raw, err := ctx.GetNodeJSON(node)
		if err != nil {
			return err
		}
		base, orderBy, err := readExpected(raw)
		if err != nil {
			return fmt.Errorf("node %s: %w", node, err)
		}
		if len(base) == 0 {
			base = []string{".+"}
		}
		patch := map[string]any{"expected": withBlacklist(base, visited)}
		if orderBy != "" {
			patch["order_by"] = orderBy
		}
		override[node] = patch
	}
	return ctx.OverridePipeline(override)
}

func readExpected(raw string) (expected []string, orderBy string, err error) {
	var node struct {
		Expected    any             `json:"expected"`
		OrderBy     string          `json:"order_by"`
		Recognition json.RawMessage `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return nil, "", err
	}
	expected, err = asStringList(node.Expected)
	if err != nil {
		return nil, "", err
	}
	orderBy = strings.TrimSpace(node.OrderBy)
	if len(node.Recognition) > 0 && node.Recognition[0] == '{' {
		var v2 struct {
			Param struct {
				Expected any    `json:"expected"`
				OrderBy  string `json:"order_by"`
			} `json:"param"`
		}
		if err := json.Unmarshal(node.Recognition, &v2); err != nil {
			return nil, "", err
		}
		if len(expected) == 0 {
			expected, err = asStringList(v2.Param.Expected)
			if err != nil {
				return nil, "", err
			}
		}
		if orderBy == "" {
			orderBy = strings.TrimSpace(v2.Param.OrderBy)
		}
	}
	for i := range expected {
		expected[i] = stripBlacklistPrefix(strings.TrimSpace(expected[i]))
	}
	return expected, orderBy, nil
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
	fields, err := recogtarget.ParseNodeJSON([]byte(raw))
	if err != nil {
		return "", false
	}

	switch strings.ToLower(fields.Type) {
	case "or":
		for _, child := range detail.CombinedResult {
			if child == nil || !child.Hit || strings.TrimSpace(child.Name) == "" {
				continue
			}
			selected, err := recogtarget.SelectDetail(ctx, child.Name, child)
			if err != nil {
				continue
			}
			if text, ok := ocrText(selected); ok {
				return text, true
			}
		}
		return "", false
	default:
		selected, err := recogtarget.SelectDetailFromJSON([]byte(raw), detail)
		if err != nil {
			return "", false
		}
		return ocrText(selected)
	}
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
