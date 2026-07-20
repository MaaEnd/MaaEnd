// Package recogtarget 提供 Pipeline 识别节点的目标解析：
// 兼容扁平 / v2 节点写法，并按 And 原生 box_index 从 CombinedResult 选取子结果。
//
// ExpressionRecognition、ListCompleteRecognition 等需要「OCR 或 And→OCR」语义的组件应复用本包，
// 业务侧只负责从选中结果中提取数字、文本等。
package recogtarget

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Fields 是从节点 JSON 解析出的识别形状，用于目标选择。
type Fields struct {
	Type     string
	AllOf    []json.RawMessage
	BoxIndex int
}

// nodeJSONSource 抽象 ctx.GetNodeJSON，便于 EffectiveType 沿节点名引用解析，并在测试中注入假数据。
type nodeJSONSource interface {
	GetNodeJSON(nodeName string) (string, error)
}

// ParseNodeJSON 解析节点 JSON 的 recognition / all_of / box_index。
// 兼容：
//   - 扁平写法："recognition":"And","all_of":[...],"box_index":n
//   - v2 写法："recognition":{"type":"And","param":{"all_of":[...],"box_index":n}}
//   - all_of 内联项直接写 {"type":"OCR",...}
func ParseNodeJSON(raw []byte) (Fields, error) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil {
		return Fields{}, fmt.Errorf("unmarshal node json: %w", err)
	}
	return parseRecognitionFields(node)
}

// ResolveAndBoxIndex 解析节点是否为 And，以及其原生 box_index。
// 非 And 时返回 (0, false, nil)。
func ResolveAndBoxIndex(raw string) (int, bool, error) {
	fields, err := ParseNodeJSON([]byte(raw))
	if err != nil {
		return 0, false, err
	}
	if fields.Type != "And" {
		return 0, false, nil
	}
	if len(fields.AllOf) == 0 {
		return 0, true, fmt.Errorf("and node all_of is empty")
	}
	if fields.BoxIndex < 0 || fields.BoxIndex >= len(fields.AllOf) {
		return 0, true, fmt.Errorf("and node box_index %d out of range, all_of size=%d", fields.BoxIndex, len(fields.AllOf))
	}
	return fields.BoxIndex, true, nil
}

// SelectDetail 根据节点定义从识别结果中选取目标子结果。
// 非 And 返回 detail 本身；And 则按原生 box_index 取 CombinedResult 子项。
func SelectDetail(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (*maa.RecognitionDetail, error) {
	if detail == nil {
		return nil, fmt.Errorf("recognition detail is empty")
	}
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node name is empty")
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("node %s json is empty", nodeName)
	}

	return SelectDetailFromJSON([]byte(raw), detail)
}

// SelectDetailFromJSON 在已有节点 JSON 时选取目标子结果。
func SelectDetailFromJSON(raw []byte, detail *maa.RecognitionDetail) (*maa.RecognitionDetail, error) {
	if detail == nil {
		return nil, fmt.Errorf("recognition detail is empty")
	}

	fields, err := ParseNodeJSON(raw)
	if err != nil {
		return nil, err
	}
	return SelectDetailFromFields(fields, detail)
}

// SelectDetailFromFields 按已解析字段选取目标子结果。
func SelectDetailFromFields(fields Fields, detail *maa.RecognitionDetail) (*maa.RecognitionDetail, error) {
	if detail == nil {
		return nil, fmt.Errorf("recognition detail is empty")
	}
	if fields.Type != "And" {
		return detail, nil
	}
	if len(fields.AllOf) == 0 {
		return nil, fmt.Errorf("and node all_of is empty")
	}
	if fields.BoxIndex < 0 || fields.BoxIndex >= len(fields.AllOf) {
		return nil, fmt.Errorf("and node box_index %d out of range, all_of size=%d", fields.BoxIndex, len(fields.AllOf))
	}
	return SelectedDetail(detail, fields.BoxIndex)
}

// SelectedDetail 从 CombinedResult 中按索引取子识别结果。
func SelectedDetail(detail *maa.RecognitionDetail, boxIndex int) (*maa.RecognitionDetail, error) {
	if detail == nil {
		return nil, fmt.Errorf("recognition detail is empty")
	}
	if len(detail.CombinedResult) == 0 {
		return nil, fmt.Errorf("and node combined result is empty")
	}
	if boxIndex < 0 || boxIndex >= len(detail.CombinedResult) {
		return nil, fmt.Errorf("and node box_index %d out of range, combined result size=%d", boxIndex, len(detail.CombinedResult))
	}
	selected := detail.CombinedResult[boxIndex]
	if selected == nil {
		return nil, fmt.Errorf("and node box_index %d result is empty", boxIndex)
	}
	return selected, nil
}

// EffectiveType 沿 And.box_index 链解析节点的有效识别类型（如 "OCR"、"TemplateMatch"）。
// all_of 子项支持节点名引用与内联对象。
func EffectiveType(ctx *maa.Context, nodeName string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is nil")
	}
	return effectiveType(ctx, nodeName, map[string]struct{}{})
}

func effectiveType(src nodeJSONSource, nodeName string, visiting map[string]struct{}) (string, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return "", fmt.Errorf("node name is empty")
	}
	if _, seen := visiting[nodeName]; seen {
		return "", fmt.Errorf("node %s has cyclic and references", nodeName)
	}
	visiting[nodeName] = struct{}{}

	if src == nil {
		return "", fmt.Errorf("context is nil")
	}
	raw, err := src.GetNodeJSON(nodeName)
	if err != nil {
		return "", fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("node %s json is empty", nodeName)
	}
	return effectiveTypeFromJSON(src, []byte(raw), visiting)
}

func effectiveTypeFromJSON(src nodeJSONSource, raw []byte, visiting map[string]struct{}) (string, error) {
	fields, err := ParseNodeJSON(raw)
	if err != nil {
		return "", err
	}
	if fields.Type != "And" {
		return fields.Type, nil
	}
	if len(fields.AllOf) == 0 {
		return "", fmt.Errorf("and node all_of is empty")
	}
	if fields.BoxIndex < 0 || fields.BoxIndex >= len(fields.AllOf) {
		return "", fmt.Errorf("and node box_index %d out of range, all_of size=%d", fields.BoxIndex, len(fields.AllOf))
	}
	return allOfChildEffectiveType(src, fields.AllOf[fields.BoxIndex], visiting)
}

func allOfChildEffectiveType(src nodeJSONSource, raw json.RawMessage, visiting map[string]struct{}) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("all_of child is empty")
	}

	var refName string
	if err := json.Unmarshal(raw, &refName); err == nil {
		refName = strings.TrimSpace(refName)
		if refName == "" {
			return "", fmt.Errorf("all_of child node ref is empty")
		}
		return effectiveType(src, refName, visiting)
	}

	return effectiveTypeFromJSON(src, raw, visiting)
}

func parseRecognitionFields(node map[string]json.RawMessage) (Fields, error) {
	recognitionRaw, ok := node["recognition"]
	if !ok || len(recognitionRaw) == 0 || string(recognitionRaw) == "null" {
		return parseRecognitionObject(node, nil)
	}

	var asString string
	if err := json.Unmarshal(recognitionRaw, &asString); err == nil {
		allOf, err := decodeAllOf(node["all_of"])
		if err != nil {
			return Fields{}, err
		}
		boxIndex, err := decodeBoxIndex(node["box_index"], 0)
		if err != nil {
			return Fields{}, err
		}
		return Fields{
			Type:     strings.TrimSpace(asString),
			AllOf:    allOf,
			BoxIndex: boxIndex,
		}, nil
	}

	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(recognitionRaw, &asObject); err != nil {
		return Fields{}, fmt.Errorf("unmarshal recognition: %w", err)
	}
	return parseRecognitionObject(asObject, node["box_index"])
}

func parseRecognitionObject(obj map[string]json.RawMessage, fallbackBoxIndex json.RawMessage) (Fields, error) {
	recognitionType := ""
	if typeRaw, has := obj["type"]; has {
		_ = json.Unmarshal(typeRaw, &recognitionType)
	}
	if recognitionType == "" {
		if legacyRaw, has := obj["recognition"]; has {
			_ = json.Unmarshal(legacyRaw, &recognitionType)
		}
	}
	recognitionType = strings.TrimSpace(recognitionType)
	if recognitionType == "" {
		return Fields{}, fmt.Errorf("recognition type is missing")
	}

	allOf, err := decodeAllOf(obj["all_of"])
	if err != nil {
		return Fields{}, err
	}

	boxIndex, err := decodeBoxIndex(obj["box_index"], 0)
	if err != nil {
		return Fields{}, err
	}

	if len(obj["param"]) > 0 && string(obj["param"]) != "null" {
		var param map[string]json.RawMessage
		if err := json.Unmarshal(obj["param"], &param); err != nil {
			return Fields{}, fmt.Errorf("unmarshal recognition.param: %w", err)
		}
		if len(allOf) == 0 {
			allOf, err = decodeAllOf(param["all_of"])
			if err != nil {
				return Fields{}, err
			}
		}
		if _, has := param["box_index"]; has {
			boxIndex, err = decodeBoxIndex(param["box_index"], boxIndex)
			if err != nil {
				return Fields{}, err
			}
		}
	}

	if _, has := obj["box_index"]; !has && len(fallbackBoxIndex) > 0 {
		boxIndex, err = decodeBoxIndex(fallbackBoxIndex, boxIndex)
		if err != nil {
			return Fields{}, err
		}
	}

	return Fields{
		Type:     recognitionType,
		AllOf:    allOf,
		BoxIndex: boxIndex,
	}, nil
}

func decodeAllOf(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var allOf []json.RawMessage
	if err := json.Unmarshal(raw, &allOf); err != nil {
		return nil, fmt.Errorf("unmarshal all_of: %w", err)
	}
	return allOf, nil
}

func decodeBoxIndex(raw json.RawMessage, defaultValue int) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return defaultValue, nil
	}
	var boxIndex int
	if err := json.Unmarshal(raw, &boxIndex); err != nil {
		return 0, fmt.Errorf("unmarshal box_index: %w", err)
	}
	return boxIndex, nil
}
