package sellproduct

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestOperatorListSignatureIgnoresOperatorOrder(t *testing.T) {
	a := []string{"陈千语", "佩丽卡"}
	b := []string{"佩丽卡", "陈千语"}

	if got, want := operatorListSignature(a), operatorListSignature(b); got != want {
		t.Fatalf("signature mismatch: got %q, want %q", got, want)
	}
}

func TestOperatorListReachedBottomWhenSignatureUnchanged(t *testing.T) {
	previous := operatorListSignature([]string{"佩丽卡"})
	same := operatorListSignature([]string{"佩丽卡"})
	changed := operatorListSignature([]string{"陈千语"})

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

func TestOperatorListSignatureIgnoresNonOperatorOCRNoise(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "ChenQianyu", CacheName: "陈千语", Expected: []string{"陈千语"}},
		{Name: "Xaihi", CacheName: "赛希", Expected: []string{"赛希"}},
	}
	firstItems := []ocrItem{
		{text: "赛希", box: maa.Rect{100, 100, 80, 20}},
		{text: "陈千语", box: maa.Rect{300, 200, 80, 20}},
		{text: "NN", box: maa.Rect{200, 100, 30, 20}},
	}
	secondItems := []ocrItem{
		{text: "赛希", box: maa.Rect{100, 100, 80, 20}},
		{text: "陈千语", box: maa.Rect{300, 200, 80, 20}},
		{text: "N", box: maa.Rect{200, 100, 30, 20}},
	}

	first := operatorListSignature(observedOperatorCacheNames(firstItems, candidates))
	second := operatorListSignature(observedOperatorCacheNames(secondItems, candidates))
	if first != second {
		t.Fatalf("non-operator OCR noise changed signature: first %q, second %q", first, second)
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

func TestFindCurrentBestOperatorRequiresTopBonusTier(t *testing.T) {
	allCandidates := []operatorCandidate{
		{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0, BonusTier: 0},
		{Name: "Fallback", CacheName: "备选", Expected: []string{"备选"}, Priority: 1, BonusTier: 1},
	}
	candidates := bestBonusTierCandidates(allCandidates)
	fallbackItems := []ocrItem{
		{text: "备选", box: maa.Rect{100, 100, 80, 20}},
	}
	if _, _, ok := findCurrentBestOperator(candidates, allCandidates, fallbackItems); ok {
		t.Fatal("lower bonus tier candidate should not be treated as the current best operator")
	}

	bestItems := []ocrItem{
		{text: "最优", box: maa.Rect{100, 100, 80, 20}},
	}
	candidate, match, ok := findCurrentBestOperator(candidates, allCandidates, bestItems)
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

func TestFindCurrentBestOperatorAcceptsEquivalentBonusTier(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Lifeng", CacheName: "黎风", Expected: []string{"黎风"}, Priority: 0, BonusTier: 0},
		{Name: "Arcane", CacheName: "诀", Expected: []string{"诀"}, Priority: 1, BonusTier: 0},
	}
	items := []ocrItem{{text: "诀", box: maa.Rect{260, 569, 29, 23}}}

	candidate, match, ok := findCurrentBestOperator(candidates, candidates, items)
	if !ok || match == nil {
		t.Fatal("同档当前干员诀应直接命中")
	}
	if candidate.Name != "Arcane" {
		t.Fatalf("当前干员 = %q，期望 Arcane", candidate.Name)
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

func TestAllOperatorScanCandidatesUsesCompleteKnownOperatorList(t *testing.T) {
	data := &operatorSelectionData{
		KnownOperators: []operatorCandidate{
			{Name: "Other", CacheName: "其他干员", Expected: []string{"其他干员"}, Priority: 2},
			{Name: "Perlica", CacheName: "佩丽卡", Expected: []string{"佩丽卡"}, Priority: 0},
			{Name: "Avywenna", CacheName: "陈千语", Expected: []string{"陈千语"}, Priority: 1},
		},
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
	want := []string{"佩丽卡", "陈千语", "其他干员"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i, candidate := range got {
		if candidate.CacheName != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, candidate.CacheName, want[i])
		}
	}
}

func TestCandidatesForOwnershipUsesCachedOperatorsOnly(t *testing.T) {
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
	})
	if len(candidates) != 1 || candidates[0].Name != "Observed" {
		t.Fatalf("candidates = %#v, want cached Observed", candidates)
	}
}

func TestCandidatesForOwnershipUsesBestOwnedOperator(t *testing.T) {
	p := &operatorSelectionParam{
		Usage: operatorActionUsageTarget,
		Candidates: []operatorCandidate{
			{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
			{Name: "Observed", CacheName: "已观察", Expected: []string{"已观察"}, Priority: 1},
		},
	}
	candidates := candidatesForOwnership(p, operatorOwnership{
		Operators: operatorNameSet([]string{"已观察"}),
	})
	if len(candidates) != 1 || candidates[0].Name != "Observed" {
		t.Fatalf("candidates = %#v, want observed candidate", candidates)
	}
}

func TestOperatorCacheReadyForSelectionCacheModeRequiresCompleteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SellProductOwnedOperators.json")
	setOperatorCachePathForTest(t, path)
	p := &operatorActionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorActionUsageTarget,
		Location: "TestLocation",
	}
	ready, err := operatorCacheReadyForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheReadyForSelection: %v", err)
	}
	if ready {
		t.Fatal("cache mode should scan before selling when no complete snapshot exists")
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if err := writeOperatorCacheFile(path, operatorCacheFile{
		UpdatedAt: updatedAt,
		Accounts: map[string]operatorCacheAccount{
			currentOperatorCacheUID(): {UpdatedAt: updatedAt, Operators: []string{"佩丽卡"}},
		},
	}); err != nil {
		t.Fatalf("writeOperatorCacheFile: %v", err)
	}
	ready, err = operatorCacheReadyForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheReadyForSelection: %v", err)
	}
	if !ready {
		t.Fatal("cache mode should reuse an existing complete snapshot")
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeWaitsForScanComplete(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeRefresh)

	p := &operatorActionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorActionUsageTarget,
		Location: "TestLocation",
	}
	ready, err := operatorCacheReadyForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheReadyForSelection: %v", err)
	}
	if ready {
		t.Fatal("refresh mode should not be ready before scan completion")
	}
	operatorSessionMarkRefreshed()
	ready, err = operatorCacheReadyForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheReadyForSelection: %v", err)
	}
	if !ready {
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
	ready, err := operatorCacheReadyForSelection(targetSelection)
	if err != nil {
		t.Fatalf("operatorCacheReadyForSelection: %v", err)
	}
	if !ready {
		t.Fatal("refresh mode selection should reuse the global operator scan completion")
	}
}

func TestShouldWriteOperatorCacheSnapshotOnlyForGlobalInitializationOrRefresh(t *testing.T) {
	uid := "test_uid"
	existing := operatorCacheFile{
		Accounts: map[string]operatorCacheAccount{
			uid: {Operators: []string{"狼卫"}},
		},
	}

	tests := []struct {
		name  string
		param *operatorActionParam
		cache operatorCacheFile
		want  bool
	}{
		{
			name: "首次全局扫描允许建立缓存",
			param: &operatorActionParam{
				Mode:     operatorCacheModeCache,
				Usage:    operatorActionUsageAll,
				Location: "global",
			},
			cache: operatorCacheFile{},
			want:  true,
		},
		{
			name: "已有缓存时普通全局扫描不得覆盖",
			param: &operatorActionParam{
				Mode:     operatorCacheModeCache,
				Usage:    operatorActionUsageAll,
				Location: "global",
			},
			cache: existing,
			want:  false,
		},
		{
			name: "主动刷新允许覆盖已有缓存",
			param: &operatorActionParam{
				Mode:     operatorCacheModeRefresh,
				Usage:    operatorActionUsageAll,
				Location: "global",
			},
			cache: existing,
			want:  true,
		},
		{
			name: "据点内局部扫描不得覆盖缓存",
			param: &operatorActionParam{
				Mode:     operatorCacheModeRefresh,
				Usage:    operatorActionUsageRestore,
				Location: "SkyKingFlatsConstructionSite",
			},
			cache: existing,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldWriteOperatorCacheSnapshot(tt.param, tt.cache, uid); got != tt.want {
				t.Fatalf("缓存写入判定 = %v，期望 %v", got, tt.want)
			}
		})
	}
}

func TestReplaceObservedOperatorsKeepsExistingCacheDuringLocalScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SellProductOwnedOperators.json")
	setOperatorCachePathForTest(t, path)
	uid := currentOperatorCacheUID()
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if err := writeOperatorCacheFile(path, operatorCacheFile{
		UpdatedAt: updatedAt,
		Accounts: map[string]operatorCacheAccount{
			uid: {UpdatedAt: updatedAt, Operators: []string{"狼卫"}},
		},
	}); err != nil {
		t.Fatalf("写入初始干员缓存失败：%v", err)
	}

	// 据点内找人即使完成到底部，也不能用局部观察覆盖已有完整快照。
	if err := replaceObservedOperators(
		&operatorActionParam{
			Mode:     operatorCacheModeCache,
			Usage:    operatorActionUsageRestore,
			Location: "SkyKingFlatsConstructionSite",
		},
		[]operatorCandidate{{Name: "Wulfgard", CacheName: "狼卫"}},
		nil,
	); err != nil {
		t.Fatalf("处理据点局部扫描失败：%v", err)
	}

	cache, err := readOperatorCache(path)
	if err != nil {
		t.Fatalf("读取干员缓存失败：%v", err)
	}
	operators := operatorCacheOperatorsForUID(cache, uid)
	if len(operators) != 1 || operators[0] != "狼卫" {
		t.Fatalf("据点局部扫描后缓存 = %#v，期望仍保留狼卫", operators)
	}
}

func TestParseOperatorActionParamAllowsGlobalScanUsage(t *testing.T) {
	got, err := parseOperatorActionParam(`{"mode":"cache","usage":"all","location":"global","roi":[164,121,700,430]}`)
	if err != nil {
		t.Fatalf("parseOperatorActionParam: %v", err)
	}
	if got.Usage != operatorActionUsageAll {
		t.Fatalf("usage = %q, want %q", got.Usage, operatorActionUsageAll)
	}
}

func TestParseOperatorActionParamRequiresModeAndROI(t *testing.T) {
	for _, raw := range []string{
		`{"usage":"all","location":"global","roi":[164,121,700,430]}`,
		`{"mode":"cache","usage":"all","location":"global"}`,
	} {
		if _, err := parseOperatorActionParam(raw); err == nil {
			t.Fatalf("incomplete params should be rejected: %s", raw)
		}
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

func setOperatorCachePathForTest(t *testing.T, path string) {
	t.Helper()
	previous := resolveOperatorCachePathFunc
	resolveOperatorCachePathFunc = func(string) string { return path }
	t.Cleanup(func() {
		resolveOperatorCachePathFunc = previous
	})
}
