// Package quantitybudget 提供按「预算」反推滑条目标数量的通用动作。
//
// QuantityBudget 从调用节点的 attach 读取模式、当前余额、单价、目标余额与目标
// BetterSliding 节点，按需识别取值后算出目标数量，再覆写目标节点的
// attach.TargetQuantity。参数放在 attach 是为了让其他页面可以用 pipeline_override
// 换掉取值来源，复用同一个动作节点；中间值不落 attach。
package quantitybudget

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// 预算计算模式。
const (
	// ModeBuy 购买模式：余额高于目标余额时花掉差额。
	ModeBuy = "buy"
	// ModeSell 出售模式：余额低于目标余额时补足差额。
	ModeSell = "sell"
)

var (
	// ErrInvalidMode 表示模式既不是 buy 也不是 sell。
	ErrInvalidMode = errors.New("invalid mode")
	// ErrInvalidLimit 表示目标余额不是正数。
	ErrInvalidLimit = errors.New("target balance must be positive")
	// ErrInvalidPrice 表示单价不是正数。
	ErrInvalidPrice = errors.New("unit price must be positive")
	// ErrInvalidBalance 表示当前余额为负数。
	ErrInvalidBalance = errors.New("balance must not be negative")
	// ErrTargetOverflow 表示算出的目标数量超出滑条支持的范围。
	ErrTargetOverflow = errors.New("target quantity out of supported range")
)

// maxTargetQuantity 是目标数量上限：超过它的值不可能是真实档位，按配置错误处理。
const maxTargetQuantity = math.MaxInt32

// normalizeMode 归一化模式字符串，大小写与首尾空白不敏感。
func normalizeMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeBuy:
		return ModeBuy, nil
	case ModeSell:
		return ModeSell, nil
	default:
		return "", fmt.Errorf("%w: %q, expected %q or %q", ErrInvalidMode, raw, ModeBuy, ModeSell)
	}
}

// resolveBudgetTarget 按预算反推滑条目标数量。
//
// 购买：ceil((余额-目标余额)/单价)。向上取整保证花完余额严格低于目标余额，
// 且超出量小于一个单价；余额不高于目标余额时返回 0，表示本轮无需购买。
// 出售：floor((目标余额-余额)/单价)。向下取整保证卖完不越过目标余额；
// 剩余空间不足一个单价时返回 0，表示本轮无需出售。
func resolveBudgetTarget(mode string, balance int, limit int, unitPrice int) (int, error) {
	normalizedMode, err := normalizeMode(mode)
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, fmt.Errorf("%w: %d", ErrInvalidLimit, limit)
	}
	if unitPrice <= 0 {
		return 0, fmt.Errorf("%w: %d", ErrInvalidPrice, unitPrice)
	}
	if balance < 0 {
		return 0, fmt.Errorf("%w: %d", ErrInvalidBalance, balance)
	}

	// 统一用 int64 计算，避免 32 位平台上 int 溢出。
	balance64 := int64(balance)
	limit64 := int64(limit)
	unitPrice64 := int64(unitPrice)

	var target int64
	if normalizedMode == ModeBuy {
		need := balance64 - limit64
		if need <= 0 {
			return 0, nil
		}
		// 等价于 ceil(need/unitPrice)，但先除后补偿，避免 need+unitPrice-1 溢出。
		target = need / unitPrice64
		if need%unitPrice64 != 0 {
			target++
		}
	} else {
		room := limit64 - balance64
		if room < unitPrice64 {
			return 0, nil
		}
		target = room / unitPrice64
	}

	if target > int64(maxTargetQuantity) {
		return 0, fmt.Errorf("%w: %d", ErrTargetOverflow, target)
	}

	return int(target), nil
}
