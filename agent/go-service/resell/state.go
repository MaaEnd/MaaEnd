package resell

import "sync"

var (
	stateMu         sync.Mutex
	resellRecords   []ProfitRecord
	resellOverflow  int
	resellMinProfit int
	scanCostPrice   int
	scanRow         int
	scanCol         int
)

func getState() ([]ProfitRecord, int, int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	records := make([]ProfitRecord, len(resellRecords))
	copy(records, resellRecords)
	return records, resellOverflow, resellMinProfit
}

func setMinProfit(v int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	resellMinProfit = v
}

func setOverflow(v int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	resellOverflow = v
}

func clearRecords() {
	stateMu.Lock()
	defer stateMu.Unlock()
	resellRecords = resellRecords[:0]
}

func appendRecord(r ProfitRecord) {
	stateMu.Lock()
	defer stateMu.Unlock()
	resellRecords = append(resellRecords, r)
}

func setScanCostPrice(v int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	scanCostPrice = v
}

func getScanCostPrice() int {
	stateMu.Lock()
	defer stateMu.Unlock()
	return scanCostPrice
}

func setScanPos(row, col int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	scanRow, scanCol = row, col
}

func getScanPos() (int, int) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return scanRow, scanCol
}
