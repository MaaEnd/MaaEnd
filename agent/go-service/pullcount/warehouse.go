package pullcount

import "sort"

// --- Warehouse Page State --- //

type scannedVoucher struct {
	Name     string
	Quantity int
}

type scannedCell struct {
	Name        string
	Quantity    int
	HasQuantity bool
}

type pageItem struct {
	Cell        int
	Name        string
	Quantity    int
	HasQuantity bool
}

// recordPageQuantity stores a visible cell quantity without relying on title OCR order.
func recordPageQuantity(session *runSession, cell int, quantity int) {
	if session.CurrentPageCells == nil {
		session.CurrentPageCells = make(map[int]scannedCell)
	}
	current := session.CurrentPageCells[cell]
	current.Quantity = quantity
	current.HasQuantity = true
	session.CurrentPageCells[cell] = current
}

// recordPageItem stores a visible cell title by cell index so repeated OCR cannot duplicate it.
func recordPageItem(session *runSession, cell int, title string) {
	if ignoredPageTitle(title) {
		return
	}
	if session.CurrentPageCells == nil {
		session.CurrentPageCells = make(map[int]scannedCell)
	}
	current := session.CurrentPageCells[cell]
	current.Name = title
	if current.Quantity <= 0 {
		current.Quantity = 1
		current.HasQuantity = false
	}
	session.CurrentPageCells[cell] = current
}

// recordVisiblePage accumulates recognized vouchers and stores the top-row probe for the next scroll.
func recordVisiblePage(session *runSession) int {
	items := currentPageItems(session)
	session.LastHeadProbe = headQuantityProbeFromCells(session.CurrentPageCells, warehouseProbeCellLimit)
	accumulatePageVouchers(session, items)
	session.PageCount++
	return len(items)
}

// accumulatePageVouchers keeps the largest visible quantity for each configured voucher title.
func accumulatePageVouchers(session *runSession, items []pageItem) {
	for _, item := range items {
		if _, ok := session.VoucherIndex[normalizeName(item.Name)]; !ok {
			continue
		}
		if item.Quantity > session.VoucherQuantities[item.Name] {
			session.VoucherQuantities[item.Name] = item.Quantity
		}
	}
}

// scannedVouchersFromSession returns a stable list of voucher titles accumulated during scanning.
func scannedVouchersFromSession(session *runSession) []scannedVoucher {
	result := make([]scannedVoucher, 0, len(session.VoucherQuantities))
	for name, quantity := range session.VoucherQuantities {
		result = append(result, scannedVoucher{Name: name, Quantity: quantity})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// currentPageItems returns the visible warehouse page from cell-indexed Pipeline OCR records.
func currentPageItems(session *runSession) []pageItem {
	cells := make([]int, 0, len(session.CurrentPageCells))
	for cell := range session.CurrentPageCells {
		cells = append(cells, cell)
	}
	sort.Ints(cells)

	items := make([]pageItem, 0, len(cells))
	for _, cell := range cells {
		item := session.CurrentPageCells[cell]
		if item.Name == "" || ignoredPageTitle(item.Name) {
			continue
		}
		quantity := item.Quantity
		if quantity <= 0 {
			quantity = 1
		}
		items = append(items, pageItem{
			Cell:        cell,
			Name:        item.Name,
			Quantity:    quantity,
			HasQuantity: item.HasQuantity,
		})
	}
	return items
}

// headQuantityProbeFromCells returns top quantity OCR results, including cells whose title OCR missed.
func headQuantityProbeFromCells(cells map[int]scannedCell, limit int) map[int]int {
	if limit <= 0 {
		return nil
	}
	probe := make(map[int]int)
	for cell, item := range cells {
		if cell <= 0 || cell > limit || !item.HasQuantity || item.Quantity <= 0 {
			continue
		}
		probe[cell] = item.Quantity
	}
	return probe
}

// scrollProbeUnchanged compares pre-scroll and post-scroll top quantity vectors.
func scrollProbeUnchanged(before map[int]int, after map[int]int) (bool, int, int) {
	comparable := 0
	matches := 0
	for cell, beforeValue := range before {
		afterValue, ok := after[cell]
		if !ok {
			continue
		}
		comparable++
		if beforeValue == afterValue {
			matches++
		}
	}
	return comparable >= minScrollProbeComparable && matches == comparable, comparable, matches
}
