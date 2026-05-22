package pullcount

import "fmt"

// --- Voucher Totals --- //

type voucherSummary struct {
	CurrentOnlyPulls int
	CarryToNextPulls int
	NextOnlyPulls    int
}

// addVoucher adds one Pipeline-classified voucher record to the running total.
func addVoucher(session *runSession, cell int, poolScope string, pullValue int) (int, bool, error) {
	if cell <= 0 {
		return 0, false, fmt.Errorf("cell must be positive")
	}
	if pullValue != 1 && pullValue != 10 {
		return 0, false, fmt.Errorf("pull_value must be 1 or 10")
	}

	quantity := session.CurrentPageCells[cell].Quantity
	if quantity <= 0 {
		quantity = 1
	}
	key := fmt.Sprintf("%d:%d:%s", session.PageCount+1, cell, poolScope)
	if _, exists := session.VoucherCells[key]; exists {
		return quantity, false, nil
	}
	session.VoucherCells[key] = struct{}{}

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
