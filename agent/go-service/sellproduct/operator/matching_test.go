package operator

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestFindBestVisibleOperatorUsesCandidatePriority(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", Expected: []string{"备选"}, Priority: 1},
	}
	items := []ocrmatch.Item{
		{Text: "备选", Box: maa.Rect{100, 100, 80, 20}},
		{Text: "最优", Box: maa.Rect{100, 200, 80, 20}},
	}

	candidate, match, ok := findBestVisibleOperator(candidates, items)
	if !ok {
		t.Fatal("expected visible operator match")
	}
	if candidate.Name != "Best" {
		t.Fatalf("candidate = %q, want Best", candidate.Name)
	}
	if match.OCRText != "最优" {
		t.Fatalf("ocr text = %q, want 最优", match.OCRText)
	}
}

func TestFindBestVisibleOperatorDoesNotFallBackOnCurrentPage(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", Expected: []string{"备选"}, Priority: 1},
	}
	items := []ocrmatch.Item{{Text: "备选", Box: maa.Rect{100, 100, 80, 20}}}

	if _, _, ok := findBestVisibleOperator(candidates, items); ok {
		t.Fatal("visible fallback must not replace the global best candidate")
	}
}

func TestFindCurrentBestOperatorRequiresTopBonusTier(t *testing.T) {
	allCandidates := []operatorCandidate{
		{Name: "Best", Expected: []string{"最优"}, Priority: 0, BonusTier: 0},
		{Name: "Fallback", Expected: []string{"备选"}, Priority: 1, BonusTier: 1},
	}
	candidates := bestBonusTierCandidates(allCandidates, false)
	fallbackItems := []ocrmatch.Item{{Text: "备选", Box: maa.Rect{100, 100, 80, 20}}}
	if _, _, ok := findCurrentBestOperator(candidates, allCandidates, fallbackItems); ok {
		t.Fatal("lower bonus tier candidate should not be treated as the current best operator")
	}

	bestItems := []ocrmatch.Item{{Text: "最优", Box: maa.Rect{100, 100, 80, 20}}}
	candidate, match, ok := findCurrentBestOperator(candidates, allCandidates, bestItems)
	if !ok {
		t.Fatal("expected current best operator match")
	}
	if candidate.Name != "Best" {
		t.Fatalf("candidate = %q, want Best", candidate.Name)
	}
	if match.OCRText != "最优" {
		t.Fatalf("ocr text = %q, want 最优", match.OCRText)
	}
}

func TestFindCurrentBestOperatorAcceptsEquivalentBonusTier(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Lifeng", Expected: []string{"黎风"}, Priority: 0, BonusTier: 0},
		{Name: "Arcane", Expected: []string{"诀"}, Priority: 1, BonusTier: 0},
	}
	items := []ocrmatch.Item{{Text: "诀", Box: maa.Rect{260, 569, 29, 23}}}

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
			items := []ocrmatch.Item{{Text: test.ocrText, Box: maa.Rect{337, 568, 95, 35}}}
			candidate, match, ok := findCurrentBestOperator([]operatorCandidate{target}, []operatorCandidate{target}, items)
			if !ok || candidate.Name != "DaPan" || match == nil {
				t.Fatalf("当前干员匹配结果 = %+v，命中状态 = %v，期望命中 DaPan", match, ok)
			}
			if match.OCRText != test.ocrText || match.Tier != "operator_prefix_noise" {
				t.Fatalf("OCR 文本 = %q，匹配层级 = %q", match.OCRText, match.Tier)
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
	items := []ocrmatch.Item{{Text: "大潘派驻效果", Box: maa.Rect{337, 568, 95, 35}}}

	if _, match, ok := findCurrentBestOperator([]operatorCandidate{target}, knownOperators, items); ok || match != nil {
		t.Fatalf("存在更长已知名称时不应按短名称前缀命中，实际结果 = %+v", match)
	}
}

func TestFindUncachedCurrentOperatorDetectsKnownOperatorMissingFromCache(t *testing.T) {
	knownOperators := []operatorCandidate{
		{Name: "Perlica", Expected: []string{"佩丽卡"}, Priority: 0},
		{Name: "Avywenna", Expected: []string{"陈千语"}, Priority: 1},
	}
	items := []ocrmatch.Item{{Text: "陈千语", Box: maa.Rect{100, 100, 80, 20}}}

	candidate, match, ok := findUncachedCurrentOperator(knownOperators, operatorOwnership{
		Operators: operatorIDSet([]string{"Perlica"}),
	}, items)
	if !ok {
		t.Fatal("expected uncached current operator to be detected")
	}
	if candidate.Name != "Avywenna" {
		t.Fatalf("candidate = %q, want Avywenna", candidate.Name)
	}
	if match.OCRText != "陈千语" {
		t.Fatalf("ocr text = %q, want 陈千语", match.OCRText)
	}

	if _, _, ok := findUncachedCurrentOperator(knownOperators, operatorOwnership{
		Operators: operatorIDSet([]string{"Perlica", "Avywenna"}),
	}, items); ok {
		t.Fatal("cached current operator must not trigger a rescan")
	}

	unknownItems := []ocrmatch.Item{{Text: "未知干员", Box: maa.Rect{100, 100, 80, 20}}}
	if _, _, ok := findUncachedCurrentOperator(knownOperators, operatorOwnership{}, unknownItems); ok {
		t.Fatal("unrecognized current operator should not trigger a rescan")
	}
}
