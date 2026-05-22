package pullcount

import (
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// --- Voucher Totals --- //

type voucherSummary struct {
	CurrentOnlyPulls int
	CarryToNextPulls int
	NextOnlyPulls    int
}

// voucherKey builds a stable duplicate key from the template hit box passed by Pipeline.
func voucherKey(arg *maa.CustomActionArg, poolScope string) string {
	if arg == nil {
		return poolScope
	}
	box := arg.Box
	return fmt.Sprintf("%d:%d:%d:%d:%s", box.X(), box.Y(), box.Width(), box.Height(), poolScope)
}

// addVoucher adds one Pipeline-classified voucher record to the running total.
func addVoucher(session *runSession, key string, poolScope string, pullValue int) (int, bool, error) {
	if pullValue != 1 && pullValue != 10 {
		return 0, false, fmt.Errorf("pull_value must be 1 or 10")
	}

	quantity := session.PendingQuantity[poolScope]
	if quantity <= 0 {
		quantity = 1
	}
	if _, exists := session.VoucherHits[key]; exists {
		delete(session.PendingQuantity, poolScope)
		return quantity, false, nil
	}
	session.VoucherHits[key] = struct{}{}
	delete(session.PendingQuantity, poolScope)

	pulls := quantity * pullValue
	switch poolScope {
	case "current_only":
		session.Vouchers.CurrentOnlyPulls += pulls
	case "carry_to_next":
		session.Vouchers.CarryToNextPulls += pulls
	case "next_only":
		session.Vouchers.NextOnlyPulls += pulls
	default:
		return quantity, false, fmt.Errorf("pool_scope must be current_only, carry_to_next, or next_only")
	}
	return quantity, true, nil
}
