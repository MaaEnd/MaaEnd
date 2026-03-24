package expressionrecognition

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
}

var expressionNodePattern = regexp.MustCompile(`\{([^{}]+)\}`)

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

	log.Info().
		Str("component", "ExpressionRecognition").
		Str("expression", params.Expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Bool("matched", matched).
		Msg("expression evaluated")

	if !matched {
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"expression":          params.Expression,
		"resolved_expression": resolvedExpression,
		"values":              values,
		"matched":             matched,
	})

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
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

	value, err := extractRecognitionNumber(detail)
	if err != nil {
		return 0, fmt.Errorf("failed to parse node result: %w", err)
	}

	return value, nil
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

	var digits strings.Builder
	for _, ch := range cleaned {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}

	if digits.Len() == 0 {
		return 0, fmt.Errorf("ocr text %q contains no digits", cleaned)
	}

	value, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0, err
	}

	return value, nil
}
