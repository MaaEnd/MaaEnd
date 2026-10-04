// Package boolexpr evaluates boolean expressions over integers.
// Used by ExpressionRecognition (OCR node placeholders) and IMS R1 (cached item IDs).
package boolexpr

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
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
// Integer arithmetic uses arbitrary precision so intermediate values cannot overflow.
// A final integer result is clamped to the platform int range.
// Callers that need a recognition hit must assert the result is bool.
func Evaluate(expression string) (any, error) {
	parsed, err := parser.ParseExpr(expression)
	if err != nil {
		return nil, err
	}
	result, err := evaluateAST(parsed)
	if err != nil {
		return nil, err
	}
	if intValue, ok := result.(*big.Int); ok {
		return ParseIntLiteral(intValue.String())
	}
	return result, nil
}

func evaluateAST(expr ast.Expr) (any, error) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.INT {
			return nil, fmt.Errorf("unsupported literal kind %s", node.Kind.String())
		}
		value, ok := new(big.Int).SetString(node.Value, 10)
		if !ok {
			return nil, fmt.Errorf("invalid integer literal %q", node.Value)
		}
		return value, nil
	case *ast.ParenExpr:
		return evaluateAST(node.X)
	case *ast.UnaryExpr:
		value, err := evaluateAST(node.X)
		if err != nil {
			return nil, err
		}
		switch node.Op {
		case token.ADD:
			intValue, ok := value.(*big.Int)
			if !ok {
				return nil, fmt.Errorf("operator + expects int, got %T", value)
			}
			return intValue, nil
		case token.SUB:
			intValue, ok := value.(*big.Int)
			if !ok {
				return nil, fmt.Errorf("operator - expects int, got %T", value)
			}
			return new(big.Int).Neg(intValue), nil
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
			return new(big.Int).Add(leftInt, rightInt), nil
		case token.SUB:
			return new(big.Int).Sub(leftInt, rightInt), nil
		case token.MUL:
			return new(big.Int).Mul(leftInt, rightInt), nil
		case token.QUO:
			if rightInt.Sign() == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return new(big.Int).Quo(leftInt, rightInt), nil
		case token.REM:
			if rightInt.Sign() == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return new(big.Int).Rem(leftInt, rightInt), nil
		case token.LSS:
			return leftInt.Cmp(rightInt) < 0, nil
		case token.LEQ:
			return leftInt.Cmp(rightInt) <= 0, nil
		case token.GTR:
			return leftInt.Cmp(rightInt) > 0, nil
		case token.GEQ:
			return leftInt.Cmp(rightInt) >= 0, nil
		}
	case token.EQL, token.NEQ:
		switch leftValue := left.(type) {
		case *big.Int:
			rightValue, ok := right.(*big.Int)
			if !ok {
				return nil, fmt.Errorf("operator %s expects same-type operands, got %T and %T", op.String(), left, right)
			}
			if op == token.EQL {
				return leftValue.Cmp(rightValue) == 0, nil
			}
			return leftValue.Cmp(rightValue) != 0, nil
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

func requireInts(left any, right any, op token.Token) (*big.Int, *big.Int, error) {
	leftInt, ok := left.(*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("operator %s expects int operands, got %T and %T", op.String(), left, right)
	}
	rightInt, ok := right.(*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("operator %s expects int operands, got %T and %T", op.String(), left, right)
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
