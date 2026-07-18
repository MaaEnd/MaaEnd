package sellproduct

import (
	"encoding/json"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestOperatorListSignatureIgnoresOCRResultOrder(t *testing.T) {
	a := []ocrItem{
		{text: "陈千语", box: maa.Rect{300, 200, 80, 20}},
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
	}
	b := []ocrItem{
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
		{text: "陈千语", box: maa.Rect{300, 200, 80, 20}},
	}

	if got, want := operatorListSignature(a), operatorListSignature(b); got != want {
		t.Fatalf("signature mismatch: got %q, want %q", got, want)
	}
}

func TestOperatorListReachedBottomWhenSignatureUnchanged(t *testing.T) {
	previous := operatorListSignature([]ocrItem{
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
	})
	same := operatorListSignature([]ocrItem{
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
	})
	changed := operatorListSignature([]ocrItem{
		{text: "陈千语", box: maa.Rect{100, 100, 80, 20}},
	})

	if !operatorListReachedBottom(previous, same) {
		t.Fatal("unchanged operator list signature should mean bottom reached")
	}
	if operatorListReachedBottom(previous, changed) {
		t.Fatal("changed operator list signature should not mean bottom reached")
	}
	if operatorListReachedBottom("", same) {
		t.Fatal("empty previous signature should not mean bottom reached")
	}
}

func TestFindBestVisibleOperatorUsesCandidatePriority(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", CacheName: "备选", Expected: []string{"备选"}, Priority: 1},
	}
	items := []ocrItem{
		{text: "备选", box: maa.Rect{100, 100, 80, 20}},
		{text: "最优", box: maa.Rect{100, 200, 80, 20}},
	}

	candidate, match, ok := findBestVisibleOperator(candidates, items)
	if !ok {
		t.Fatal("expected visible operator match")
	}
	if candidate.Name != "Best" {
		t.Fatalf("candidate = %q, want Best", candidate.Name)
	}
	if match.ocrText != "最优" {
		t.Fatalf("ocr text = %q, want 最优", match.ocrText)
	}
}

func TestFindBestVisibleOperatorDoesNotFallBackOnCurrentPage(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", CacheName: "备选", Expected: []string{"备选"}, Priority: 1},
	}
	items := []ocrItem{{text: "备选", box: maa.Rect{100, 100, 80, 20}}}

	if _, _, ok := findBestVisibleOperator(candidates, items); ok {
		t.Fatal("visible fallback must not replace the global best candidate")
	}
}

func TestFindCurrentBestOperatorRequiresTopPriorityCandidate(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", CacheName: "备选", Expected: []string{"备选"}, Priority: 1},
	}
	fallbackItems := []ocrItem{
		{text: "备选", box: maa.Rect{100, 100, 80, 20}},
	}
	if _, _, ok := findCurrentBestOperator(candidates, candidates, fallbackItems); ok {
		t.Fatal("fallback candidate should not be treated as the current best operator")
	}

	bestItems := []ocrItem{
		{text: "最优", box: maa.Rect{100, 100, 80, 20}},
	}
	candidate, match, ok := findCurrentBestOperator(candidates, candidates, bestItems)
	if !ok {
		t.Fatal("expected current best operator match")
	}
	if candidate.Name != "Best" {
		t.Fatalf("candidate = %q, want Best", candidate.Name)
	}
	if match.ocrText != "最优" {
		t.Fatalf("ocr text = %q, want 最优", match.ocrText)
	}
}

// TestFindCurrentBestOperatorAllowsKnownNamePrefix 验证中英文名称与右侧界面文本粘连时都能按前缀命中。
func TestFindCurrentBestOperatorAllowsKnownNamePrefix(t *testing.T) {
	target := operatorCandidate{Name: "DaPan", Expected: []string{"大潘", "Da Pan", "ダパン", "판"}}
	tests := []struct {
		name    string
		ocrText string
	}{
		{name: "中文", ocrText: "大潘派"},
		{name: "英文", ocrText: "Da Pan Assignment Effect"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items := []ocrItem{{text: test.ocrText, box: maa.Rect{337, 568, 95, 35}}}
			candidate, match, ok := findCurrentBestOperator([]operatorCandidate{target}, []operatorCandidate{target}, items)
			if !ok || candidate.Name != "DaPan" || match == nil {
				t.Fatalf("当前干员匹配结果 = %+v，命中状态 = %v，期望命中 DaPan", match, ok)
			}
			if match.ocrText != test.ocrText || match.tier != "operator_prefix_noise" {
				t.Fatalf("OCR 文本 = %q，匹配层级 = %q", match.ocrText, match.tier)
			}
		})
	}
}

// TestFindCurrentBestOperatorRejectsAmbiguousLongerKnownName 验证存在更长已知名称时不会误认成短名称。
func TestFindCurrentBestOperatorRejectsAmbiguousLongerKnownName(t *testing.T) {
	target := operatorCandidate{Name: "DaPan", Expected: []string{"大潘", "Da Pan"}}
	knownOperators := []operatorCandidate{
		target,
		{Name: "DaPanPai", Expected: []string{"大潘派", "Da Pan Group"}},
	}
	items := []ocrItem{{text: "大潘派驻效果", box: maa.Rect{337, 568, 95, 35}}}

	if _, match, ok := findCurrentBestOperator([]operatorCandidate{target}, knownOperators, items); ok || match != nil {
		t.Fatalf("存在更长已知名称时不应按短名称前缀命中，实际结果 = %+v", match)
	}
}

func TestAllOperatorScanCandidatesIncludesTargetAndRestoreCandidates(t *testing.T) {
	data := &operatorSelectionData{
		TargetCandidates: map[string][]operatorCandidate{
			"A": {{Name: "Perlica", CacheName: "佩丽卡", Expected: []string{"佩丽卡"}, Priority: 2}},
			"B": {{Name: "Avywenna", CacheName: "陈千语", Expected: []string{"陈千语"}, Priority: 1}},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "A",
				Candidates: []operatorCandidate{
					{Name: "Restore", CacheName: "恢复干员", Expected: []string{"恢复干员"}, Priority: 3},
				},
			},
		},
	}

	got := allOperatorScanCandidates(data)
	want := []string{"陈千语", "佩丽卡", "恢复干员"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i, candidate := range got {
		if candidate.CacheName != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, candidate.CacheName, want[i])
		}
	}
}

func TestCandidatesForOwnershipUsesTheoreticalBestWhenCacheIsPartial(t *testing.T) {
	p := &operatorSelectionParam{
		Usage: operatorActionUsageTarget,
		Candidates: []operatorCandidate{
			{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
			{Name: "Observed", CacheName: "已观察", Expected: []string{"已观察"}, Priority: 1},
		},
		ScanCandidates: []operatorCandidate{
			{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
			{Name: "Observed", CacheName: "已观察", Expected: []string{"已观察"}, Priority: 1},
		},
	}
	candidates := candidatesForOwnership(p, operatorOwnership{
		Operators: operatorNameSet([]string{"已观察"}),
		Complete:  false,
	})
	if len(candidates) != 1 || candidates[0].Name != "Best" {
		t.Fatalf("candidates = %#v, want theoretical Best", candidates)
	}
}

func TestCandidatesForOwnershipUsesObservedBestWhenCacheIsComplete(t *testing.T) {
	p := &operatorSelectionParam{
		Usage: operatorActionUsageTarget,
		Candidates: []operatorCandidate{
			{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
			{Name: "Observed", CacheName: "已观察", Expected: []string{"已观察"}, Priority: 1},
		},
	}
	candidates := candidatesForOwnership(p, operatorOwnership{
		Operators: operatorNameSet([]string{"已观察"}),
		Complete:  true,
	})
	if len(candidates) != 1 || candidates[0].Name != "Observed" {
		t.Fatalf("candidates = %#v, want observed candidate", candidates)
	}
}

func TestOperatorCacheReadyForSelectionCacheModeAllowsProgressiveSearch(t *testing.T) {
	p := &operatorActionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorActionUsageTarget,
		Location: "TestLocation",
	}
	if !operatorCacheReadyForSelection(p) {
		t.Fatal("cache mode should enter progressive search even without a complete snapshot")
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeWaitsForScanComplete(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeRefresh)

	p := &operatorActionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorActionUsageTarget,
		Location: "TestLocation",
	}
	if operatorCacheReadyForSelection(p) {
		t.Fatal("refresh mode should not be ready before scan completion")
	}
	operatorSessionMarkRefreshed()
	if !operatorCacheReadyForSelection(p) {
		t.Fatal("refresh mode should be ready after scan completion")
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeUsesGlobalScanCompletion(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeRefresh)

	targetSelection := &operatorActionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorActionUsageTarget,
		Location: "SkyKingFlats",
	}
	operatorSessionMarkRefreshed()
	if !operatorCacheReadyForSelection(targetSelection) {
		t.Fatal("refresh mode selection should reuse the global operator scan completion")
	}
}

func TestParseOperatorActionParamAllowsGlobalScanUsage(t *testing.T) {
	got, err := parseOperatorActionParam(`{"mode":"cache","usage":"all","location":"global"}`)
	if err != nil {
		t.Fatalf("parseOperatorActionParam: %v", err)
	}
	if got.Usage != operatorActionUsageAll {
		t.Fatalf("usage = %q, want %q", got.Usage, operatorActionUsageAll)
	}
}

func TestOperatorListBottomNotFoundCanHitAfterRefreshScan(t *testing.T) {
	p := &operatorActionParam{
		Mode:   operatorCacheModeRefresh,
		Result: operatorListBottomResultNotFound,
	}
	if !shouldHitOperatorListBottomResult(p, false) {
		t.Fatal("not_found should hit when recomputation has no candidate")
	}
	if shouldHitOperatorListBottomResult(p, true) {
		t.Fatal("not_found should not hit when recomputation found a candidate")
	}
}

func TestOperatorScanOutcomeRecognitionConsumesCompletedScan(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	p := &operatorActionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorActionUsageTarget,
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
		CustomRecognitionParam: `{"mode":"cache","usage":"target","location":"TestLocation","result":"not_found"}`,
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
	p := &operatorActionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorActionUsageAll,
		Location: "global",
	}
	operatorListStateSet(operatorListScanState{
		Key:       operatorListScanStateKey(p),
		Completed: true,
		Error:     "cache is read-only",
	})

	r := &OperatorScanOutcomeRecognition{}
	result, ok := r.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"mode":"cache","usage":"all","location":"global","result":"error"}`,
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

func TestOperatorSessionResetClearsRefreshCompletion(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeRefresh)
	operatorSessionMarkRefreshed()
	if !operatorSessionRefreshed() {
		t.Fatal("session should be marked refreshed")
	}
	operatorSessionReset(operatorCacheModeRefresh)
	if operatorSessionRefreshed() {
		t.Fatal("new task session must not reuse a previous refresh marker")
	}
}

func resetOperatorSessionForTest(t *testing.T, mode string) {
	t.Helper()
	operatorStateMu.Lock()
	previousSession := operatorSession
	previousStates := operatorListScanStates
	operatorStateMu.Unlock()
	operatorSessionReset(mode)
	t.Cleanup(func() {
		operatorStateMu.Lock()
		operatorSession = previousSession
		operatorListScanStates = previousStates
		operatorStateMu.Unlock()
	})
}
