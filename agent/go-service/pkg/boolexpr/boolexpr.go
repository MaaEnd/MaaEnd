// Package boolexpr evaluates boolean expressions over integers.
// Used by ExpressionRecognition (OCR node placeholders) and IMS R1 (cached item IDs).
package boolexpr

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

var (
	// PlaceholderPattern matches {name} tokens in an expression.
	PlaceholderPattern = regexp.MustCompile(`\{([^{}]+)\}`)

	IntMax = int(^uint(0) >> 1)
	IntMin = -IntMax - 1
)

// ResolveFunc maps a placeholder name to an integer value.
type ResolveFunc func(name string) (int, error)

// ResolvePlaceholders replaces every {name} in expression via resolve.
// Returns the numeric expression string and a map of placeholder → value.
func ResolvePlaceholders(expression string, resolve ResolveFunc) (string, map[string]int, error) {
	if resolve == nil {
		return "", nil, fmt.Errorf("resolve func is nil")
	}

	values := make(map[string]int)
	var resolveErr error

	resolved := PlaceholderPattern.ReplaceAllStringFunc(expression, func(match string) string {
		if resolveErr != nil {
			return match
		}

		submatches := PlaceholderPattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			resolveErr = fmt.Errorf("invalid placeholder %q", match)
			return match
		}

		name := strings.TrimSpace(submatches[1])
		if name == "" {
			resolveErr = fmt.Errorf("placeholder must not be empty")
			return match
		}

		value, err := resolve(name)
		if err != nil {
			resolveErr = fmt.Errorf("%s: %w", name, err)
			return match
		}

		values[name] = value
		return strconv.Itoa(value)
	})

	if resolveErr != nil {
		return "", nil, resolveErr
	}

	return resolved, values, nil
}

// Evaluate parses and evaluates a boolean or integer expression.
// Callers that need a recognition hit must assert the result is bool.
func Evaluate(expression string) (any, error) {
	parsed, err := parser.ParseExpr(expression)
	if err != nil {
		return nil, err
	}
	return evaluateAST(parsed)
}

func evaluateAST(expr ast.Expr) (any, error) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.INT {
			return nil, fmt.Errorf("unsupported literal kind %s", node.Kind.String())
		}
		return ParseIntLiteral(node.Value)
	case *ast.ParenExpr:
		return evaluateAST(node.X)
	case *ast.UnaryExpr:
		value, err := evaluateAST(node.X)
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
		left, err := evaluateAST(node.X)
		if err != nil {
			return nil, err
		}
		right, err := evaluateAST(node.Y)
		if err != nil {
			return nil, err
		}
		return evaluateBinary(left, right, node.Op)
	default:
		return nil, fmt.Errorf("unsupported expression type %T", expr)
	}
}

func evaluateBinary(left any, right any, op token.Token) (any, error) {
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

// ParseIntLiteral parses an integer literal; out-of-range values clamp to IntMax/IntMin.
func ParseIntLiteral(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err == nil {
		return value, nil
	}

	var numErr *strconv.NumError
	if errors.As(err, &numErr) && numErr.Err == strconv.ErrRange {
		clamped := IntMax
		if strings.HasPrefix(strings.TrimSpace(raw), "-") {
			clamped = IntMin
		}
		log.Warn().
			Str("component", "boolexpr").
			Str("literal", raw).
			Int("clamped_value", clamped).
			Msg("expression integer literal out of int range, clamped")
		return clamped, nil
	}

	return 0, err
}

// ClampInt clamps a float64 into platform int range.
func ClampInt(value float64) int {
	if value > float64(IntMax) {
		return IntMax
	}
	if value < float64(IntMin) {
		return IntMin
	}
	return int(value)
}
