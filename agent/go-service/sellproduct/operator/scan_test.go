package operator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestCurrentOperatorOCRCacheConsumesMatchingResultOnce(t *testing.T) {
	resetCurrentOperatorOCRCacheForTest(t)
	arg := &maa.CustomRecognitionArg{TaskID: 42}
	param := &operatorRecognitionParam{
		Location: "CardiacRemediationStation",
		ROI:      []int{260, 568, 280, 35},
	}
	key := makeCurrentOperatorOCRCacheKey(arg, param)
	want := []ocrmatch.Item{{Text: "佩丽卡", Box: maa.Rect{260, 568, 80, 20}}}
	storeCurrentOperatorOCRCache(key, want)

	got, err := recognizeCurrentOperatorList(nil, arg, param, true)
	if err != nil || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("cached OCR result = %#v, error = %v, want %#v", got, err, want)
	}
	if _, ok := takeCurrentOperatorOCRCache(key); ok {
		t.Fatal("current operator OCR cache must be consumed only once")
	}
}

func TestCurrentOperatorOCRCacheRejectsDifferentLocation(t *testing.T) {
	resetCurrentOperatorOCRCacheForTest(t)
	arg := &maa.CustomRecognitionArg{TaskID: 42}
	storeCurrentOperatorOCRCache(
		makeCurrentOperatorOCRCacheKey(arg, &operatorRecognitionParam{
			Location: "CardiacRemediationStation",
			ROI:      []int{260, 568, 280, 35},
		}),
		[]ocrmatch.Item{{Text: "佩丽卡"}},
	)

	if _, ok := takeCurrentOperatorOCRCache(
		makeCurrentOperatorOCRCacheKey(arg, &operatorRecognitionParam{
			Location: "RefugeeCamp",
			ROI:      []int{260, 568, 280, 35},
		}),
	); ok {
		t.Fatal("different location must not reuse current operator OCR results")
	}
}

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
		{Name: "ChenQianyu", Expected: []string{"陈千语"}},
		{Name: "Xaihi", Expected: []string{"赛希"}},
	}
	firstItems := []ocrmatch.Item{
		{Text: "赛希", Box: maa.Rect{100, 100, 80, 20}},
		{Text: "陈千语", Box: maa.Rect{300, 200, 80, 20}},
		{Text: "NN", Box: maa.Rect{200, 100, 30, 20}},
	}
	secondItems := []ocrmatch.Item{
		{Text: "赛希", Box: maa.Rect{100, 100, 80, 20}},
		{Text: "陈千语", Box: maa.Rect{300, 200, 80, 20}},
		{Text: "N", Box: maa.Rect{200, 100, 30, 20}},
	}

	first := operatorListSignature(observedOperatorIDs(firstItems, candidates))
	second := operatorListSignature(observedOperatorIDs(secondItems, candidates))
	if first != second {
		t.Fatalf("non-operator OCR noise changed signature: first %q, second %q", first, second)
	}
}

func TestOperatorCacheReadyForSelectionCacheModeRequiresCompleteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	setSellProductCachePathForTest(t, path)
	p := &operatorRecognitionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorUsageTarget,
		Location: "TestLocation",
	}
	status, err := operatorCacheStatusForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheStatusForSelection: %v", err)
	}
	if status.Ready {
		t.Fatal("cache mode should scan before selling when no complete snapshot exists")
	}
	if err := os.WriteFile(path, []byte(`{"accounts":{"unknown":{"operators":["佩丽卡"]}}}`), 0644); err != nil {
		t.Fatalf("write incompatible cache: %v", err)
	}
	status, err = operatorCacheStatusForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheStatusForSelection with incompatible cache: %v", err)
	}
	if status.Ready {
		t.Fatal("cache mode should rescan when the persisted cache is incompatible")
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			currentSellProductCacheUID(): {Operators: testOperatorSnapshotAt(updatedAt, "Perlica")},
		},
	}); err != nil {
		t.Fatalf("writeSellProductCache: %v", err)
	}
	status, err = operatorCacheStatusForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheStatusForSelection: %v", err)
	}
	if !status.Ready {
		t.Fatal("cache mode should reuse an existing complete snapshot")
	}
	if status.UpdatedAt.Format(time.RFC3339) != updatedAt {
		t.Fatalf("cache updated_at = %q, want %q", status.UpdatedAt, updatedAt)
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeWaitsForScanComplete(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeRefresh)

	p := &operatorRecognitionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorUsageTarget,
		Location: "TestLocation",
	}
	status, err := operatorCacheStatusForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheStatusForSelection: %v", err)
	}
	if status.Ready {
		t.Fatal("refresh mode should not be ready before scan completion")
	}
	operatorSessionMarkRefreshed()
	status, err = operatorCacheStatusForSelection(p)
	if err != nil {
		t.Fatalf("operatorCacheStatusForSelection: %v", err)
	}
	if !status.Ready {
		t.Fatal("refresh mode should be ready after scan completion")
	}
	if !status.UpdatedAt.IsZero() {
		t.Fatalf("refresh mode should not report a persisted cache time: %q", status.UpdatedAt)
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeUsesGlobalScanCompletion(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeRefresh)

	targetSelection := &operatorRecognitionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorUsageTarget,
		Location: "SkyKingFlats",
	}
	operatorSessionMarkRefreshed()
	status, err := operatorCacheStatusForSelection(targetSelection)
	if err != nil {
		t.Fatalf("operatorCacheStatusForSelection: %v", err)
	}
	if !status.Ready {
		t.Fatal("refresh mode selection should reuse the global operator scan completion")
	}
}

func TestShouldWriteOperatorCacheSnapshotOnlyForGlobalInitializationOrRefresh(t *testing.T) {
	uid := testCacheUID
	existing := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {Operators: testOperatorSnapshot("Wulfgard")},
		},
	}

	tests := []struct {
		name  string
		param *operatorRecognitionParam
		cache sellProductCache
		want  bool
	}{
		{
			name: "首次全局扫描允许建立缓存",
			param: &operatorRecognitionParam{
				Mode:     operatorCacheModeCache,
				Usage:    operatorUsageAll,
				Location: "global",
			},
			cache: sellProductCache{},
			want:  true,
		},
		{
			name: "已有缓存时普通全局扫描不得覆盖",
			param: &operatorRecognitionParam{
				Mode:     operatorCacheModeCache,
				Usage:    operatorUsageAll,
				Location: "global",
			},
			cache: existing,
			want:  false,
		},
		{
			name: "主动刷新允许覆盖已有缓存",
			param: &operatorRecognitionParam{
				Mode:     operatorCacheModeRefresh,
				Usage:    operatorUsageAll,
				Location: "global",
			},
			cache: existing,
			want:  true,
		},
		{
			name: "据点内局部扫描不得覆盖缓存",
			param: &operatorRecognitionParam{
				Mode:     operatorCacheModeRefresh,
				Usage:    operatorUsageRestore,
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
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	setSellProductCachePathForTest(t, path)
	uid := currentSellProductCacheUID()
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {Operators: testOperatorSnapshotAt(updatedAt, "Wulfgard")},
		},
	}); err != nil {
		t.Fatalf("写入初始干员缓存失败：%v", err)
	}

	// 据点内找人即使完成到底部，也不能用局部观察覆盖已有完整快照。
	if err := replaceObservedOperators(
		&operatorRecognitionParam{
			Mode:     operatorCacheModeCache,
			Usage:    operatorUsageRestore,
			Location: "SkyKingFlatsConstructionSite",
		},
		[]operatorCandidate{{Name: "Wulfgard"}},
		nil,
	); err != nil {
		t.Fatalf("处理据点局部扫描失败：%v", err)
	}

	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取干员缓存失败：%v", err)
	}
	operators := cachedOperatorIDsForUID(cache, uid)
	if len(operators) != 1 || operators[0] != "Wulfgard" {
		t.Fatalf("据点局部扫描后缓存 = %#v，期望仍保留 Wulfgard", operators)
	}
}

func TestOperatorListBottomNotFoundCanHitAfterRefreshScan(t *testing.T) {
	p := &operatorRecognitionParam{
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
	previousCachePath := resolveSellProductCachePathFunc
	cachePath := filepath.Join(t.TempDir(), sellProductCacheFileName)
	resolveSellProductCachePathFunc = func() string { return cachePath }
	operatorSessionReset(mode)
	t.Cleanup(func() {
		operatorStateMu.Lock()
		operatorSession = previousSession
		operatorListScanStates = previousStates
		operatorStateMu.Unlock()
		resolveSellProductCachePathFunc = previousCachePath
	})
}

func resetCurrentOperatorOCRCacheForTest(t *testing.T) {
	t.Helper()
	currentOperatorOCRCache.Lock()
	previous := currentOperatorOCRCache.entry
	currentOperatorOCRCache.entry = nil
	currentOperatorOCRCache.Unlock()
	t.Cleanup(func() {
		currentOperatorOCRCache.Lock()
		currentOperatorOCRCache.entry = previous
		currentOperatorOCRCache.Unlock()
	})
}

func setSellProductCachePathForTest(t *testing.T, path string) {
	t.Helper()
	previous := resolveSellProductCachePathFunc
	resolveSellProductCachePathFunc = func() string { return path }
	t.Cleanup(func() {
		resolveSellProductCachePathFunc = previous
	})
}
