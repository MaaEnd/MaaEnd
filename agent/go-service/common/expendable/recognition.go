package expendable

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "ExpendableRecognition"
	attachVisited = "visited"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

// Recognition 是通用消费性识别器：命中一次后把 key 写入 attach.visited，
// 下次通过覆盖 OCR expected（负向排除）不再命中同一目标。
//
// 只需传入 candidate；会从候选节点树自动收集命名 OCR 子节点，用于注入排除与提取 key。
// 点击框由 candidate 的命中框决定（And.box_index / Or 胜出分支等仍由 Pipeline 配置）。
type Recognition struct{}

type params struct {
	// Candidate 为外部候选识别节点（OCR / And / Or 等），其命中框即点击框。
	Candidate string `json:"candidate"`
}

type hitDetail struct {
	Text string `json:"text"`
}

// Run implements maa.CustomRecognitionRunner.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("nil context or arg")
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

	nodeName := strings.TrimSpace(arg.CurrentTaskName)
	if nodeName == "" {
		log.Error().Str("component", componentName).Msg("current task name is empty")
		return nil, false
	}

	ocrNodes, err := discoverOCRNodes(ctx, p.Candidate)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("candidate", p.Candidate).
			Msg("discover ocr nodes failed")
		return nil, false
	}
	if len(ocrNodes) == 0 {
		log.Error().
			Str("component", componentName).
			Str("candidate", p.Candidate).
			Msg("candidate tree has no named OCR node")
		return nil, false
	}

	visited, err := loadVisited(ctx, nodeName)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("node", nodeName).Msg("load attach.visited failed")
		return nil, false
	}

	if err := injectVisitedExpected(ctx, ocrNodes, visited); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("override ocr expected failed")
		return nil, false
	}

	detail, err := ctx.RunRecognition(p.Candidate, arg.Img)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("candidate", p.Candidate).
			Msg("RunRecognition failed")
		return nil, false
	}
	if detail == nil || !detail.Hit {
		log.Info().
			Str("component", componentName).
			Str("candidate", p.Candidate).
			Strs("visited", visited).
			Msg("no unvisited candidate")
		return nil, false
	}

	text, ok := extractKeyText(detail, ocrNodes)
	if !ok {
		log.Warn().
			Str("component", componentName).
			Str("candidate", p.Candidate).
			Strs("ocr_nodes", ocrNodes).
			Msg("hit but key text missing")
		return nil, false
	}
	if containsString(visited, text) {
		log.Info().
			Str("component", componentName).
			Str("text", text).
			Msg("key already visited, skip")
		return nil, false
	}

	newVisited := append(append([]string{}, visited...), text)
	if err := saveVisited(ctx, nodeName, newVisited); err != nil {
		log.Error().Err(err).Str("component", componentName).Str("text", text).Msg("save attach.visited failed")
		return nil, false
	}

	detailJSON, _ := json.Marshal(hitDetail{Text: text})
	log.Info().
		Str("component", componentName).
		Str("text", text).
		Interface("box", detail.Box).
		Strs("ocr_nodes", ocrNodes).
		Strs("visited", newVisited).
		Msg("selected unvisited candidate")

	return &maa.CustomRecognitionResult{
		Box:    detail.Box,
		Detail: string(detailJSON),
	}, true
}

func parseParams(raw string) (params, error) {
	var p params
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return params{}, fmt.Errorf("custom_recognition_param is empty")
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return params{}, err
	}
	p.Candidate = strings.TrimSpace(p.Candidate)
	if p.Candidate == "" {
		return params{}, fmt.Errorf("candidate is required")
	}
	return p, nil
}

func trimNonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

type nodeJSONSource interface {
	GetNodeJSON(nodeName string) (string, error)
}

func discoverOCRNodes(ctx *maa.Context, candidate string) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	return discoverOCRNodesRec(ctx, candidate, map[string]struct{}{})
}

func discoverOCRNodesRec(src nodeJSONSource, nodeName string, visiting map[string]struct{}) ([]string, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node name is empty")
	}
	if _, seen := visiting[nodeName]; seen {
		return nil, fmt.Errorf("node %s has cyclic references", nodeName)
	}
	visiting[nodeName] = struct{}{}
	defer delete(visiting, nodeName)

	raw, err := src.GetNodeJSON(nodeName)
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w", nodeName, err)
	}
	recType, children, err := parseNodeTypeAndChildren([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("parse node %s: %w", nodeName, err)
	}

	switch strings.ToLower(recType) {
	case "ocr":
		return []string{nodeName}, nil
	case "and", "or":
		out := make([]string, 0)
		seen := make(map[string]struct{})
		for _, child := range children {
			names, err := resolveChildOCRNodes(src, child, visiting)
			if err != nil {
				return nil, err
			}
			for _, name := range names {
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				out = append(out, name)
			}
		}
		return out, nil
	default:
		return nil, nil
	}
}

func resolveChildOCRNodes(src nodeJSONSource, child json.RawMessage, visiting map[string]struct{}) ([]string, error) {
	if len(child) == 0 || string(child) == "null" {
		return nil, nil
	}
	var refName string
	if err := json.Unmarshal(child, &refName); err == nil {
		refName = strings.TrimSpace(refName)
		if refName == "" {
			return nil, nil
		}
		return discoverOCRNodesRec(src, refName, visiting)
	}

	// 内联识别对象无法按节点名 Override expected。
	recType, _, err := parseRecognitionTypeAndChildren(child)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(recType, "OCR") {
		return nil, fmt.Errorf("inline OCR in candidate tree is unsupported; use a named OCR node")
	}
	if strings.EqualFold(recType, "And") || strings.EqualFold(recType, "Or") {
		_, children, err := parseRecognitionTypeAndChildren(child)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0)
		for _, nested := range children {
			names, err := resolveChildOCRNodes(src, nested, visiting)
			if err != nil {
				return nil, err
			}
			out = append(out, names...)
		}
		return out, nil
	}
	return nil, nil
}

func parseNodeTypeAndChildren(raw []byte) (recType string, children []json.RawMessage, err error) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil {
		return "", nil, err
	}
	recognitionRaw, ok := node["recognition"]
	if !ok || len(recognitionRaw) == 0 || string(recognitionRaw) == "null" {
		return parseRecognitionTypeAndChildren(raw)
	}

	var asString string
	if err := json.Unmarshal(recognitionRaw, &asString); err == nil {
		children, err := decodeChildren(node["all_of"], node["any_of"])
		if err != nil {
			return "", nil, err
		}
		return strings.TrimSpace(asString), children, nil
	}
	return parseRecognitionTypeAndChildren(recognitionRaw)
}

func parseRecognitionTypeAndChildren(raw []byte) (recType string, children []json.RawMessage, err error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", nil, err
	}
	if typeRaw, has := obj["type"]; has {
		_ = json.Unmarshal(typeRaw, &recType)
	}
	if recType == "" {
		if legacy, has := obj["recognition"]; has {
			_ = json.Unmarshal(legacy, &recType)
		}
	}
	recType = strings.TrimSpace(recType)
	if recType == "" {
		return "", nil, fmt.Errorf("recognition type is missing")
	}

	allOf := obj["all_of"]
	anyOf := obj["any_of"]
	if len(obj["param"]) > 0 && string(obj["param"]) != "null" {
		var param map[string]json.RawMessage
		if err := json.Unmarshal(obj["param"], &param); err != nil {
			return "", nil, err
		}
		if len(allOf) == 0 {
			allOf = param["all_of"]
		}
		if len(anyOf) == 0 {
			anyOf = param["any_of"]
		}
	}
	children, err = decodeChildren(allOf, anyOf)
	if err != nil {
		return "", nil, err
	}
	return recType, children, nil
}

func decodeChildren(allOfRaw, anyOfRaw json.RawMessage) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0)
	for _, raw := range []json.RawMessage{allOfRaw, anyOfRaw} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func injectVisitedExpected(ctx *maa.Context, ocrNodes []string, visited []string) error {
	override := make(map[string]any, len(ocrNodes))
	for _, node := range ocrNodes {
		raw, err := ctx.GetNodeJSON(node)
		if err != nil {
			return fmt.Errorf("get node %s: %w", node, err)
		}
		baseExpected, orderBy, err := parseOCRExpectedAndOrderBy([]byte(raw))
		if err != nil {
			return fmt.Errorf("parse node %s: %w", node, err)
		}
		if len(baseExpected) == 0 {
			baseExpected = []string{".+"}
		}
		patch := map[string]any{
			"expected": applyVisitedExclusion(baseExpected, visited),
		}
		if orderBy != "" {
			patch["order_by"] = orderBy
		}
		override[node] = patch
	}
	return ctx.OverridePipeline(override)
}

func parseOCRExpectedAndOrderBy(raw []byte) (expected []string, orderBy string, err error) {
	var node struct {
		Expected    any             `json:"expected"`
		OrderBy     string          `json:"order_by"`
		Recognition json.RawMessage `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, "", err
	}
	expected, err = decodeExpected(node.Expected)
	if err != nil {
		return nil, "", err
	}
	orderBy = strings.TrimSpace(node.OrderBy)

	if len(node.Recognition) > 0 && node.Recognition[0] != '"' {
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
			expected, err = decodeExpected(v2.Param.Expected)
			if err != nil {
				return nil, "", err
			}
		}
		if orderBy == "" {
			orderBy = strings.TrimSpace(v2.Param.OrderBy)
		}
	}

	stripped := make([]string, 0, len(expected))
	for _, item := range expected {
		stripped = append(stripped, stripVisitedExclusionPrefix(strings.TrimSpace(item)))
	}
	return stripped, orderBy, nil
}

func decodeExpected(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, nil
		}
		return []string{trimmed}, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected item is not string")
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out, nil
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("expected has unsupported type %T", raw)
		}
		var asString string
		if err := json.Unmarshal(b, &asString); err == nil {
			asString = strings.TrimSpace(asString)
			if asString == "" {
				return nil, nil
			}
			return []string{asString}, nil
		}
		var asList []string
		if err := json.Unmarshal(b, &asList); err == nil {
			return trimNonEmpty(asList), nil
		}
		return nil, fmt.Errorf("expected has unsupported type %T", raw)
	}
}

// visitedExclusionPrefixRe 匹配 build 出的 `^(?!(?:a|b)$)` 前缀。
var visitedExclusionPrefixRe = regexp.MustCompile(`^\^\(\?!\(\?:(?:[^\\]|\\.)*?\)\$\)`)

func stripVisitedExclusionPrefix(pattern string) string {
	if pattern == "" {
		return pattern
	}
	if loc := visitedExclusionPrefixRe.FindStringIndex(pattern); loc != nil {
		return pattern[loc[1]:]
	}
	return pattern
}

func applyVisitedExclusion(base []string, visited []string) []string {
	escaped := make([]string, 0, len(visited))
	for _, name := range visited {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		escaped = append(escaped, regexp.QuoteMeta(trimmed))
	}
	prefix := ""
	if len(escaped) > 0 {
		prefix = fmt.Sprintf("^(?!(?:%s)$)", strings.Join(escaped, "|"))
	}
	out := make([]string, 0, len(base))
	for _, item := range base {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, prefix+item)
	}
	if len(out) == 0 {
		if prefix == "" {
			return []string{".+"}
		}
		return []string{prefix + ".+"}
	}
	return out
}

// extractKeyText 优先取与 candidate 命中框一致的 OCR（通常即 And.box_index 指向的文案），
// 避免 Or(红点∧文案, NEW∧文案) 场景把 "NEW" 写成 visited key。
func extractKeyText(detail *maa.RecognitionDetail, ocrNodes []string) (string, bool) {
	if detail == nil {
		return "", false
	}
	for _, name := range ocrNodes {
		child := findDetailByName(detail, name)
		if child == nil {
			continue
		}
		text, ok := ocrTextFromDetail(child)
		if !ok {
			continue
		}
		if boxesEqual(child.Box, detail.Box) || ocrBestBoxMatches(child, detail.Box) {
			return text, true
		}
	}
	for _, name := range ocrNodes {
		child := findDetailByName(detail, name)
		if child == nil {
			continue
		}
		if text, ok := ocrTextFromDetail(child); ok {
			return text, true
		}
	}
	return ocrTextFromDetail(detail)
}

func boxesEqual(a, b maa.Rect) bool {
	return a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3]
}

func ocrBestBoxMatches(detail *maa.RecognitionDetail, want maa.Rect) bool {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return false
	}
	ocrResult, ok := detail.Results.Best.AsOCR()
	if !ok {
		return false
	}
	return boxesEqual(ocrResult.Box, want)
}

func ocrTextFromDetail(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil {
		return "", false
	}
	if text, ok := ocrTextFromResults(detail.Results); ok {
		return text, true
	}
	return ocrTextFromDetailJSON(detail.DetailJson)
}

func ocrTextFromDetailJSON(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var parsed struct {
		Best struct {
			Text string `json:"text"`
		} `json:"best"`
		Filtered []struct {
			Text string `json:"text"`
		} `json:"filtered"`
		All []struct {
			Text string `json:"text"`
		} `json:"all"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", false
	}
	if text := strings.TrimSpace(parsed.Best.Text); text != "" {
		return text, true
	}
	for _, item := range parsed.Filtered {
		if text := strings.TrimSpace(item.Text); text != "" {
			return text, true
		}
	}
	for _, item := range parsed.All {
		if text := strings.TrimSpace(item.Text); text != "" {
			return text, true
		}
	}
	return "", false
}

func findDetailByName(detail *maa.RecognitionDetail, targetName string) *maa.RecognitionDetail {
	if detail == nil {
		return nil
	}
	if detail.Name == targetName {
		return detail
	}
	for _, child := range detail.CombinedResult {
		if found := findDetailByName(child, targetName); found != nil {
			return found
		}
	}
	return nil
}

func ocrTextFromResults(results *maa.RecognitionResults) (string, bool) {
	if results == nil {
		return "", false
	}
	tryOCR := func(result *maa.RecognitionResult) (string, bool) {
		if result == nil {
			return "", false
		}
		ocrResult, ok := result.AsOCR()
		if !ok {
			return "", false
		}
		text := strings.TrimSpace(ocrResult.Text)
		return text, text != ""
	}
	if text, ok := tryOCR(results.Best); ok {
		return text, true
	}
	for _, result := range results.Filtered {
		if text, ok := tryOCR(result); ok {
			return text, true
		}
	}
	for _, result := range results.All {
		if text, ok := tryOCR(result); ok {
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
	return trimNonEmpty(wrapper.Attach.Visited), nil
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
		wrapper.Attach = make(map[string]any)
	}
	wrapper.Attach[attachVisited] = visited

	return ctx.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": wrapper.Attach,
		},
	})
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
