package creditshopping

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

var _ maa.CustomRecognitionRunner = &reserveCreditRecognition{}

type reserveCreditRecognition struct{}

type reserveRecognitionConfig struct {
	Threshold  *int
	Expression string
}

var expressionNodePattern = regexp.MustCompile(`\{([^{}]+)\}`)

// Run checks whether current credits are below the configured reserve threshold,
// or evaluates a boolean expression composed from node OCR results.
func (r *reserveCreditRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	config, err := parseReserveRecognitionConfig(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse reserve recognition config")
		return nil, false
	}

	if strings.TrimSpace(config.Expression) != "" {
		return runReserveExpressionRecognition(ctx, arg, config.Expression)
	}

	threshold := 300
	if config.Threshold != nil {
		threshold = *config.Threshold
	}

	if threshold <= 0 {
		log.Debug().
			Str("component", "CreditShopping").
			Int("reserve_credit_threshold", threshold).
			Msg("reserve threshold disabled")
		return nil, false
	}

	credit, err := runNumericRecognition(ctx, arg, "CreditShoppingReserveCreditOCRInternal")
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Msg("failed to run reserve credit ocr")
		return nil, false
	}

	action := "pass"
	if credit >= threshold {
		action = "ignore"
	}

	log.Info().
		Str("component", "CreditShopping").
		Int("credit", credit).
		Int("reserve_credit_threshold", threshold).
		Str("result", action).
		Msgf("识别到%d,目标%d,%s", credit, threshold, action)

	if action == "ignore" {
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]int{
		"credit":    credit,
		"threshold": threshold,
	})

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

func parseReserveRecognitionConfig(raw string) (reserveRecognitionConfig, error) {
	defaultThreshold := 300
	config := reserveRecognitionConfig{
		Threshold: &defaultThreshold,
	}

	if strings.TrimSpace(raw) == "" {
		return config, nil
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return reserveRecognitionConfig{}, err
	}

	if expressionRaw, ok := params["expression"]; ok {
		expression, ok := expressionRaw.(string)
		if !ok {
			return reserveRecognitionConfig{}, fmt.Errorf("expression: unsupported type %T", expressionRaw)
		}
		expression = strings.TrimSpace(expression)
		if expression == "" {
			return reserveRecognitionConfig{}, fmt.Errorf("expression: must not be empty")
		}
		return reserveRecognitionConfig{
			Expression: expression,
		}, nil
	}

	value, ok := params["threshold"]
	if !ok {
		return config, nil
	}

	threshold, err := parseFlexibleInt(value)
	if err != nil {
		return reserveRecognitionConfig{}, fmt.Errorf("threshold: %w", err)
	}

	if threshold < 0 {
		return reserveRecognitionConfig{}, fmt.Errorf("must be non-negative")
	}

	config.Threshold = &threshold
	return config, nil
}

func runReserveExpressionRecognition(ctx *maa.Context, arg *maa.CustomRecognitionArg, expression string) (*maa.CustomRecognitionResult, bool) {
	resolvedExpression, values, err := resolveExpressionValues(ctx, arg, expression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Str("expression", expression).
			Msg("failed to resolve reserve expression")
		return nil, false
	}

	result, err := evaluateExpression(resolvedExpression)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CreditShopping").
			Str("expression", expression).
			Str("resolved_expression", resolvedExpression).
			Msg("failed to evaluate reserve expression")
		return nil, false
	}

	matched, ok := result.(bool)
	if !ok {
		log.Error().
			Str("component", "CreditShopping").
			Str("expression", expression).
			Str("resolved_expression", resolvedExpression).
			Interface("result", result).
			Msg("reserve expression must evaluate to bool")
		return nil, false
	}

	log.Info().
		Str("component", "CreditShopping").
		Str("expression", expression).
		Str("resolved_expression", resolvedExpression).
		Interface("values", values).
		Bool("matched", matched).
		Msg("evaluated reserve expression")

	if !matched {
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"expression":          expression,
		"resolved_expression": resolvedExpression,
		"values":              values,
		"matched":             matched,
	})

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
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

func parseFlexibleInt(value any) (int, error) {
	switch v := value.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}
