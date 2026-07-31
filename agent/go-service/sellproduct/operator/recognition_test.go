package operator

import (
	"encoding/json"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseOperatorRecognitionParamAllowsGlobalScanUsage(t *testing.T) {
	got, err := parseOperatorRecognitionParam(`{"mode":"cache","usage":"all","location":"global","roi":[164,121,700,430]}`)
	if err != nil {
		t.Fatalf("parseOperatorRecognitionParam: %v", err)
	}
	if got.Usage != operatorUsageAll {
		t.Fatalf("usage = %q, want %q", got.Usage, operatorUsageAll)
	}
}

func TestParseOperatorRecognitionParamRequiresModeAndROI(t *testing.T) {
	for _, raw := range []string{
		`{"usage":"all","location":"global","roi":[164,121,700,430]}`,
		`{"mode":"cache","usage":"all","location":"global"}`,
	} {
		if _, err := parseOperatorRecognitionParam(raw); err == nil {
			t.Fatalf("incomplete params should be rejected: %s", raw)
		}
	}
}

func TestOperatorScanOutcomeRecognitionConsumesCompletedScan(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	p := &operatorRecognitionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorUsageTarget,
		Location: "TestLocation",
	}
	operatorListStateSet(operatorListScanState{
		Key:                operatorListScanStateKey(p),
		ExpectedCandidates: []string{"最优", "备选"},
		ObservedCandidates: []string{"备选"},
		Completed:          true,
		HasCandidate:       false,
	})

	r := &OperatorScanOutcomeRecognition{}
	result, ok := r.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"mode":"cache","usage":"target","location":"TestLocation","result":"not_found","roi":[164,121,700,430]}`,
	})
	if !ok || result == nil {
		t.Fatal("completed scan without a candidate should hit the unavailable branch")
	}
	var detail operatorScanOutcomeDetail
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("unmarshal result detail: %v", err)
	}
	if detail.Result != operatorListBottomResultNotFound || detail.Reason != "no_owned_candidate" {
		t.Fatalf("detail = %#v, want target not-found outcome", detail)
	}
	if len(detail.ExpectedCandidates) != 2 || detail.ExpectedCandidates[0] != "最优" {
		t.Fatalf("expected candidates = %#v", detail.ExpectedCandidates)
	}
	if len(detail.ObservedCandidates) != 1 || detail.ObservedCandidates[0] != "备选" {
		t.Fatalf("observed candidates = %#v", detail.ObservedCandidates)
	}
	if _, exists := operatorListStateGet(operatorListScanStateKey(p)); exists {
		t.Fatal("unavailable branch should consume the completed scan state")
	}
}

func TestOperatorScanOutcomeRecognitionReportsScanError(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	p := &operatorRecognitionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorUsageAll,
		Location: "global",
	}
	operatorListStateSet(operatorListScanState{
		Key:       operatorListScanStateKey(p),
		Completed: true,
		Error:     "cache is read-only",
	})

	r := &OperatorScanOutcomeRecognition{}
	result, ok := r.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"mode":"cache","usage":"all","location":"global","result":"error","roi":[164,121,700,430]}`,
	})
	if !ok || result == nil {
		t.Fatalf("result = %#v, ok = %v, want scan error", result, ok)
	}
	var detail operatorScanOutcomeDetail
	if err := json.Unmarshal([]byte(result.Detail), &detail); err != nil {
		t.Fatalf("unmarshal result detail: %v", err)
	}
	if detail.Result != operatorListBottomResultError || detail.Reason != "scan_error" || detail.Error != "cache is read-only" {
		t.Fatalf("detail = %#v, want scan error", detail)
	}
}
