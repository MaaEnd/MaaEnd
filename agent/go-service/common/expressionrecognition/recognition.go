package expressionrecognition

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"regexp"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &Recognition{}

type Recognition struct{}

type Params struct {
	Expression string `json:"expression"`
	BoxNode    string `json:"box_node"`
}

type nodeDefinition struct {
	Recognition recognitionDefinition `json:"recognition"`
}

type recognitionDefinition struct {
	Type        string            `json:"type"`
	Recognition string            `json:"recognition"`
	Param       json.RawMessage   `json:"param"`
	AllOf       []json.RawMessage `json:"all_of"`
	BoxIndex    *int              `json:"box_index"`
}

type andRecognitionParam struct {
	AllOf    []json.RawMessage `json:"all_of"`
	BoxIndex *int              `json:"box_index"`
}

var (
	expressionNodePattern = regexp.MustCompile(`\{([^{}]+)\}`)
	ocrNumericPattern     = regexp.MustCompile(`(?i)[+-]?(?:\d+(?:[.,]\d+)?|[.,]\d+)\s*(?:[a-z]+|万|亿)?`)
	asciiLetterPattern    = regexp.MustCompile(`[A-Za-z]+$`)
)

// Run evaluates a boolean expression composed of numeric recognition nodes.
func (r *Recognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	params, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse expression recognition params")
		return nil, false
	}

	resolvedExpression, values, err := resolveExpressionValues(ctx, arg, params.Expression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Msg("failed to resolve expression values")
		return nil, false
	}

	result, err := evaluateExpression(resolvedExpression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Msg("failed to evaluate expression")
		return nil, false
	}

	matched, ok := result.(bool)
	if !ok {
		log.Error().
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Str("resolved_expression", resolvedExpression).
			Interface("result", result).
			Msg("expression result must be boolean")
		return nil, false
	}

	logEvaluationResult(params.Expression, resolvedExpression, values, matched)

	if !matched {
		return nil, false
	}

	resultBox, err := resolveResultBox(ctx, arg, params)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "ExpressionRecognition").
			Str("expression", params.Expression).
			Str("box_node", params.BoxNode).
			Msg("failed to resolve result box")
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"expression":          params.Expression,
		"resolved_expression": resolvedExpression,
		"values":              values,
		"matched":             matched,
	})

	return &maa.CustomRecognitionResult{
		Box:    resultBox,
		Detail: string(detailJSON),
	}, true
}

func logEvaluationResult(expression string, resolvedExpression string, values map[string]int, matched bool) {
	log.Info().
		Str("component", "ExpressionRecognition").
		Str("expression", expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Bool("matched", matched).
		Msg("expression evaluated")
}

func parseParams(raw string) (*Params, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("expression is required")
	}

	var params Params
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil, err
	}

	params.Expression = strings.TrimSpace(params.Expression)
	if params.Expression == "" {
		return nil, fmt.Errorf("expression is required")
	}
	params.BoxNode = strings.TrimSpace(params.BoxNode)

	return &params, nil
}

func resolveExpressionValues(ctx *maa.Context, arg *maa.CustomRecognitionArg, expression string) (string, map[string]int, error) {
	values := make(map[string]int)
	var resolveErr error

	resolvedExpression := expressionNodePattern.ReplaceAllStringFunc(expression, func(match string) string {
		if resolveErr != nil {
			return match
		}

		submatches := expressionNodePattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			resolveErr = fmt.Errorf("invalid node placeholder %q", match)
			return match
		}

		nodeName := strings.TrimSpace(submatches[1])
		if nodeName == "" {
			resolveErr = fmt.Errorf("node placeholder must not be empty")
			return match
		}

		value, err := runNumericRecognition(ctx, arg, nodeName)
		if err != nil {
			resolveErr = fmt.Errorf("%s: %w", nodeName, err)
			return match
		}

		values[nodeName] = value
		return strconv.Itoa(value)
	})

	if resolveErr != nil {
		return "", nil, resolveErr
	}

	return resolvedExpression, values, nil
}

func runNumericRecognition(ctx *maa.Context, arg *maa.CustomRecognitionArg, nodeName string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, arg.Img)
	if err != nil {
		return 0, err
	}

	value, err := extractRecognitionNumberFromNode(ctx, nodeName, detail)
	if err != nil {
		return 0, fmt.Errorf("failed to parse node result from %s: %w", nodeName, err)
	}

	return value, nil
}

func resolveResultBox(ctx *maa.Context, arg *maa.CustomRecognitionArg, params *Params) (maa.Rect, error) {
	if params == nil || params.BoxNode == "" {
		return arg.Roi, nil
	}

	detail, err := ctx.RunRecognition(params.BoxNode, arg.Img)
	if err != nil {
		return maa.Rect{}, err
	}

	return extractRecognitionBoxFromNode(ctx, params.BoxNode, detail)
}

func extractRecognitionNumberFromNode(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (int, error) {
	if detail == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return 0, fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("node %s json is empty", nodeName)
	}

	boxIndex, isAndNode, err := resolveAndNodeBoxIndex(raw)
	if err != nil {
		return 0, fmt.Errorf("resolve %s numeric source: %w", nodeName, err)
	}
	if !isAndNode {
		return extractRecognitionNumber(detail)
	}

	selectedDetail, err := extractAndSelectedDetail(detail, boxIndex)
	if err != nil {
		return 0, err
	}

	return extractRecognitionNumber(selectedDetail)
}

func extractRecognitionBoxFromNode(ctx *maa.Context, nodeName string, detail *maa.RecognitionDetail) (maa.Rect, error) {
	if detail == nil {
		return maa.Rect{}, fmt.Errorf("recognition detail is empty")
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return maa.Rect{}, fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	if strings.TrimSpace(raw) == "" {
		return maa.Rect{}, fmt.Errorf("node %s json is empty", nodeName)
	}

	boxIndex, isAndNode, err := resolveAndNodeBoxIndex(raw)
	if err != nil {
		return maa.Rect{}, fmt.Errorf("resolve %s box source: %w", nodeName, err)
	}
	if !isAndNode {
		return detail.Box, nil
	}

	selectedDetail, err := extractAndSelectedDetail(detail, boxIndex)
	if err != nil {
		return maa.Rect{}, err
	}
	return selectedDetail.Box, nil
}

func extractAndSelectedDetail(detail *maa.RecognitionDetail, boxIndex int) (*maa.RecognitionDetail, error) {
	if len(detail.CombinedResult) == 0 {
		return nil, fmt.Errorf("and node combined result is empty")
	}
	if boxIndex < 0 || boxIndex >= len(detail.CombinedResult) {
		return nil, fmt.Errorf("and node box_index %d out of range, combined result size=%d", boxIndex, len(detail.CombinedResult))
	}

	selectedDetail := detail.CombinedResult[boxIndex]
	if selectedDetail == nil {
		return nil, fmt.Errorf("and node box_index %d result is empty", boxIndex)
	}
	return selectedDetail, nil
}

func resolveAndNodeBoxIndex(raw string) (int, bool, error) {
	var node nodeDefinition
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return 0, false, fmt.Errorf("unmarshal node json: %w", err)
	}

	recognitionType := strings.TrimSpace(node.Recognition.Type)
	if recognitionType == "" {
		recognitionType = strings.TrimSpace(node.Recognition.Recognition)
	}
	if recognitionType != "And" {
		return 0, false, nil
	}

	allOf := node.Recognition.AllOf
	boxIndex := 0

	if len(node.Recognition.Param) > 0 {
		var param andRecognitionParam
		if err := json.Unmarshal(node.Recognition.Param, &param); err != nil {
			return 0, true, fmt.Errorf("unmarshal and param: %w", err)
		}
		allOf = param.AllOf
		if param.BoxIndex != nil {
			boxIndex = *param.BoxIndex
		}
	} else if node.Recognition.BoxIndex != nil {
		boxIndex = *node.Recognition.BoxIndex
	}

	if len(allOf) == 0 {
		return 0, true, fmt.Errorf("and node all_of is empty")
	}
	if boxIndex < 0 || boxIndex >= len(allOf) {
		return 0, true, fmt.Errorf("and node box_index %d out of range, all_of size=%d", boxIndex, len(allOf))
	}

	return boxIndex, true, nil
}

func extractRecognitionNumber(detail *maa.RecognitionDetail) (int, error) {
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("recognition detail is empty")
	}

	if best := detail.Results.Best; best != nil {
		if ocrResult, ok := best.AsOCR(); ok {
			return parseOCRNumericValue(ocrResult.Text)
		}
	}

	for _, result := range detail.Results.All {
		if ocrResult, ok := result.AsOCR(); ok {
			return parseOCRNumericValue(ocrResult.Text)
		}
	}

	return 0, fmt.Errorf("no ocr result found")
}

func evaluateExpression(expression string) (any, error) {
	parsedExpression, err := parser.ParseExpr(expression)
	if err != nil {
		return nil, err
	}

	return evaluateASTExpression(parsedExpression)
}

func evaluateASTExpression(expr ast.Expr) (any, error) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.INT {
			return nil, fmt.Errorf("unsupported literal kind %s", node.Kind.String())
		}
		return strconv.Atoi(node.Value)
	case *ast.ParenExpr:
		return evaluateASTExpression(node.X)
	case *ast.UnaryExpr:
		value, err := evaluateASTExpression(node.X)
		if err != nil {
			return nil, err
		}
		switch node.Op {
		case token.ADD:
			intValue, ok := value.(int)
			if !ok {
				return nil, fmt.Errorf("operator + expects int, got %T", value)
			}
			return intValue, nil
		case token.SUB:
			intValue, ok := value.(int)
			if !ok {
				return nil, fmt.Errorf("operator - expects int, got %T", value)
			}
			return -intValue, nil
		case token.NOT:
			boolValue, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("operator ! expects bool, got %T", value)
			}
			return !boolValue, nil
		default:
			return nil, fmt.Errorf("unsupported unary operator %s", node.Op.String())
		}
	case *ast.BinaryExpr:
		left, err := evaluateASTExpression(node.X)
		if err != nil {
			return nil, err
		}
		right, err := evaluateASTExpression(node.Y)
		if err != nil {
			return nil, err
		}
		return evaluateBinaryExpression(left, right, node.Op)
	default:
		return nil, fmt.Errorf("unsupported expression type %T", expr)
	}
}

func evaluateBinaryExpression(left any, right any, op token.Token) (any, error) {
	switch op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
		token.LSS, token.LEQ, token.GTR, token.GEQ:
		leftInt, rightInt, err := requireInts(left, right, op)
		if err != nil {
			return nil, err
		}
		switch op {
		case token.ADD:
			return leftInt + rightInt, nil
		case token.SUB:
			return leftInt - rightInt, nil
		case token.MUL:
			return leftInt * rightInt, nil
		case token.QUO:
			if rightInt == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return leftInt / rightInt, nil
		case token.REM:
			if rightInt == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return leftInt % rightInt, nil
		case token.LSS:
			return leftInt < rightInt, nil
		case token.LEQ:
			return leftInt <= rightInt, nil
		case token.GTR:
			return leftInt > rightInt, nil
		case token.GEQ:
			return leftInt >= rightInt, nil
		}
	case token.EQL, token.NEQ:
		switch leftValue := left.(type) {
		case int:
			rightValue, ok := right.(int)
			if !ok {
				return nil, fmt.Errorf("operator %s expects same-type operands, got %T and %T", op.String(), left, right)
			}
			if op == token.EQL {
				return leftValue == rightValue, nil
			}
			return leftValue != rightValue, nil
		case bool:
			rightValue, ok := right.(bool)
			if !ok {
				return nil, fmt.Errorf("operator %s expects same-type operands, got %T and %T", op.String(), left, right)
			}
			if op == token.EQL {
				return leftValue == rightValue, nil
			}
			return leftValue != rightValue, nil
		default:
			return nil, fmt.Errorf("unsupported equality operand type %T", left)
		}
	case token.LAND, token.LOR:
		leftBool, rightBool, err := requireBools(left, right, op)
		if err != nil {
			return nil, err
		}
		if op == token.LAND {
			return leftBool && rightBool, nil
		}
		return leftBool || rightBool, nil
	}

	return nil, fmt.Errorf("unsupported binary operator %s", op.String())
}

func requireInts(left any, right any, op token.Token) (int, int, error) {
	leftInt, ok := left.(int)
	if !ok {
		return 0, 0, fmt.Errorf("operator %s expects int operands, got %T and %T", op.String(), left, right)
	}
	rightInt, ok := right.(int)
	if !ok {
		return 0, 0, fmt.Errorf("operator %s expects int operands, got %T and %T", op.String(), left, right)
	}
	return leftInt, rightInt, nil
}

func requireBools(left any, right any, op token.Token) (bool, bool, error) {
	leftBool, ok := left.(bool)
	if !ok {
		return false, false, fmt.Errorf("operator %s expects bool operands, got %T and %T", op.String(), left, right)
	}
	rightBool, ok := right.(bool)
	if !ok {
		return false, false, fmt.Errorf("operator %s expects bool operands, got %T and %T", op.String(), left, right)
	}
	return leftBool, rightBool, nil
}

func parseOCRNumericValue(text string) (int, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return 0, fmt.Errorf("ocr text is empty")
	}

	matchIndex := ocrNumericPattern.FindStringIndex(cleaned)
	if matchIndex == nil {
		return 0, fmt.Errorf("ocr text %q contains no numeric value", cleaned)
	}
	match := cleaned[matchIndex[0]:matchIndex[1]]

	numberText, multiplier, err := normalizeOCRNumericToken(match)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseFloat(numberText, 64)
	if err != nil {
		return 0, err
	}

	scaled := math.Round(value * multiplier)
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if scaled > float64(maxInt) || scaled < float64(minInt) {
		return 0, fmt.Errorf("ocr text %q is out of int range", cleaned)
	}

	return int(scaled), nil
}

func normalizeOCRNumericToken(token string) (string, float64, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" {
		return "", 0, fmt.Errorf("ocr numeric token is empty")
	}

	multiplier := 1.0
	for _, suffix := range []struct {
		unit       string
		multiplier float64
	}{
		{unit: "亿", multiplier: 100000000},
		{unit: "万", multiplier: 10000},
		{unit: "K", multiplier: 1000},
		{unit: "k", multiplier: 1000},
		{unit: "M", multiplier: 1000000},
		{unit: "m", multiplier: 1000000},
		{unit: "B", multiplier: 1000000000},
		{unit: "b", multiplier: 1000000000},
	} {
		if strings.HasSuffix(normalized, suffix.unit) {
			normalized = strings.TrimSpace(strings.TrimSuffix(normalized, suffix.unit))
			multiplier = suffix.multiplier
			break
		}
	}

	if unsupportedSuffix := asciiLetterPattern.FindString(normalized); unsupportedSuffix != "" {
		return "", 0, fmt.Errorf("unsupported ocr numeric suffix %q in %q", unsupportedSuffix, token)
	}

	if normalized == "" {
		return "", 0, fmt.Errorf("ocr numeric token %q has no numeric part", token)
	}

	normalized = strings.ReplaceAll(normalized, " ", "")
	if strings.Contains(normalized, ".") {
		normalized = strings.ReplaceAll(normalized, ",", "")
	} else if strings.Count(normalized, ",") == 1 {
		parts := strings.Split(normalized, ",")
		if len(parts) == 2 && len(parts[1]) != 3 {
			normalized = parts[0] + "." + parts[1]
		} else {
			normalized = strings.ReplaceAll(normalized, ",", "")
		}
	} else {
		normalized = strings.ReplaceAll(normalized, ",", "")
	}

	return normalized, multiplier, nil
}
