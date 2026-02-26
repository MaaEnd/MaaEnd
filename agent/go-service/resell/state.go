package resell

import "sync"

var (
	stateMu         sync.Mutex
	resellRecords   []ProfitRecord
	resellOverflow  int
	resellMinProfit int
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
