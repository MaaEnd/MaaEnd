package sellproduct

import (
	"testing"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

func TestStripSeparators(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"紫晶质瓶", "紫晶质瓶"},
		{"I紫晶质瓶", "i紫晶质瓶"},
		{"Canned Citrome [C]", "cannedcitromec"},
		{"Canned Citrome | C |", "cannedcitromec"},
		{"  紫晶质瓶  ", "紫晶质瓶"},
		{"优质柑实罐头", "优质柑实罐头"},
		{"精选·柑实罐头", "精选柑实罐头"},
		{"", ""},
		// 覆盖 stripSeparators 支持的全部分隔字符。
		{"精选(柑实罐头)", "精选柑实罐头"},
		{"精选-柑实罐头", "精选柑实罐头"},
		{"精选_柑实罐头", "精选柑实罐头"},
		{"精选.柑实罐头", "精选柑实罐头"},
		{"精选,柑实罐头", "精选柑实罐头"},
		{"精选、柑实罐头", "精选柑实罐头"},
		{"精选/柑实罐头", "精选柑实罐头"},
		{"精选\\柑实罐头", "精选柑实罐头"},
		{"精选:柑实罐头", "精选柑实罐头"},
		{"精选；柑实罐头", "精选柑实罐头"},
		{"精选;柑实罐头", "精选柑实罐头"},
		{"精选：柑实罐头", "精选柑实罐头"},
	}
	for _, c := range cases {
		if got := stripSeparators(c.in); got != c.want {
			t.Errorf("stripSeparators(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripASCIIAlnum(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"i紫晶质瓶", "紫晶质瓶"},
		{"紫晶质瓶", "紫晶质瓶"},
		{"cannedcitromec", ""},
		{"优质柑实罐头", "优质柑实罐头"},
		{"精选9柑实罐头2", "精选柑实罐头"},
	}
	for _, c := range cases {
		if got := stripASCIIAlnum(c.in); got != c.want {
			t.Errorf("stripASCIIAlnum(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFindBestMatchIgnoresASCIINoiseInCJKNames 验证 CJK 名称可以剔除 ASCII OCR 噪声后命中。
func TestFindBestMatchIgnoresASCIINoiseInCJKNames(t *testing.T) {
	ocr := []ocrItem{
		{text: "I紫晶质瓶", box: maa.Rect{200, 150, 120, 40}},
		{text: "柑实罐头", box: maa.Rect{200, 250, 120, 40}},
	}
	candidates := []string{"紫晶質瓶", "紫晶质瓶", "紫晶製ボトル", "Amethyst Bottle"}

	got := findBestMatch(ocr, candidates)
	if got == nil {
		t.Fatalf("expected a match for 'I紫晶质瓶' vs '紫晶质瓶', got nil")
	}
	if got.candidate != "紫晶质瓶" && got.candidate != "紫晶質瓶" {
		t.Errorf("matched wrong candidate: %q", got.candidate)
	}
	if got.ocrText != "I紫晶质瓶" {
		t.Errorf("matched wrong OCR text: %q", got.ocrText)
	}
	if got.box.Y() != 150 {
		t.Errorf("box should come from the matched OCR item, got Y=%d", got.box.Y())
	}
	if got.tier != "B" {
		t.Errorf("expected tier B match (ASCII noise stripped), got tier %q", got.tier)
	}
}

// TestFindBestMatchRejectsLongerCJKNames 验证严格匹配不会混淆带前缀的其他货品。
func TestFindBestMatchRejectsLongerCJKNames(t *testing.T) {
	candidates := []string{"柑實罐頭", "柑实罐头", "シトロームの缶詰", "Canned Citrome C"}

	cases := []struct {
		name    string
		ocrText string
	}{
		{"premium", "优质柑实罐头"},
		{"selected", "精选柑实罐头"},
		{"premium_selected", "精选优质柑实罐头"},
		{"premium_traditional", "優質柑實罐頭"},
		{"selected_noise", "I精选柑实罐头"},
		{"premium_with_brackets", "[优质]柑实罐头"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ocr := []ocrItem{{text: c.ocrText, box: maa.Rect{0, 0, 10, 10}}}
			got := findBestMatch(ocr, candidates)
			if got != nil {
				t.Errorf("must NOT match %q to candidates for 柑实罐头 (got %+v)", c.ocrText, got)
			}
		})
	}
}

// 优质柑实罐头作为候选时，OCR 正常读出应该命中。
func TestFindBestMatch_PremiumCandidateMatchesPremiumOCR(t *testing.T) {
	candidates := []string{"優質柑實罐頭", "优质柑实罐头", "特級シトロームの缶詰", "Premium Canned Citrome"}
	ocr := []ocrItem{{text: "优质柑实罐头", box: maa.Rect{0, 100, 10, 10}}}
	got := findBestMatch(ocr, candidates)
	if got == nil {
		t.Fatal("expected a match")
	}
	if got.candidate != "优质柑实罐头" && got.candidate != "優質柑實罐頭" {
		t.Errorf("unexpected matched candidate: %q", got.candidate)
	}
}

// 紫晶相关物品不应互相串扰。
func TestFindBestMatch_NoPurpleCrystalCrossMatch(t *testing.T) {
	amethystBottleCands := []string{"紫晶质瓶", "紫晶質瓶", "紫晶製ボトル", "Amethyst Bottle"}

	cases := []struct {
		name    string
		ocrText string
	}{
		{"part", "紫晶零件"},
		{"ore", "紫晶矿"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ocr := []ocrItem{{text: c.ocrText, box: maa.Rect{0, 0, 10, 10}}}
			if got := findBestMatch(ocr, amethystBottleCands); got != nil {
				t.Errorf("must NOT match %q to amethyst bottle candidates (got %+v)", c.ocrText, got)
			}
		})
	}
}

// 英文名括号噪声：OCR 读到 "Canned Citrome [C]" 应在 Tier A 命中
// 候选 "Canned Citrome C"。
func TestFindBestMatch_EnglishBracketsNoise(t *testing.T) {
	candidates := []string{
		"柑實罐頭",
		"柑实罐头",
		"シトロームの缶詰Ⅰ",
		"Canned Citrome [C]",
		"Canned Citrome C",
	}
	ocr := []ocrItem{{text: "Canned Citrome [C]", box: maa.Rect{0, 100, 10, 10}}}
	got := findBestMatch(ocr, candidates)
	if got == nil {
		t.Fatal("expected a match for English name")
	}
	if got.tier != "A" {
		t.Errorf("expected tier A (exact after separator-strip), got %q", got.tier)
	}
}

// Tier A 优先于 Tier B。
func TestFindBestMatch_TierAPreferredOverTierB(t *testing.T) {
	ocr := []ocrItem{
		{text: "I紫晶质瓶", box: maa.Rect{0, 100, 10, 10}},
		{text: "紫晶质瓶", box: maa.Rect{0, 200, 10, 10}},
	}
	candidates := []string{"紫晶质瓶"}
	got := findBestMatch(ocr, candidates)
	if got == nil {
		t.Fatal("expected a match")
	}
	if got.tier != "A" {
		t.Errorf("expected tier A for exact OCR, got tier %q", got.tier)
	}
	if got.box.Y() != 200 {
		t.Errorf("expected exact match at Y=200, got Y=%d", got.box.Y())
	}
}

func TestFindBestMatch_EmptyCandidates(t *testing.T) {
	ocr := []ocrItem{{text: "any", box: maa.Rect{}}}
	if got := findBestMatch(ocr, nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestFindBestMatch_NoOCR(t *testing.T) {
	if got := findBestMatch(nil, []string{"紫晶质瓶"}); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestFindBestMatchKeepsOperatorPrefixNoiseStrict 验证通用货品/列表匹配不会接受当前干员专用的前缀容错。
func TestFindBestMatchKeepsOperatorPrefixNoiseStrict(t *testing.T) {
	ocr := []ocrItem{{text: "大潘派", box: maa.Rect{0, 0, 10, 10}}}
	if got := findBestMatch(ocr, []string{"大潘"}); got != nil {
		t.Fatalf("通用严格匹配不应接受尾字噪声，实际结果 = %+v", got)
	}
}

// Tier A 稳定性：上方的 OCR 优先命中。
func TestFindBestMatch_PrefersTopLeftOCR(t *testing.T) {
	ocr := []ocrItem{
		{text: "紫晶质瓶", box: maa.Rect{100, 300, 10, 10}},
		{text: "紫晶质瓶", box: maa.Rect{100, 100, 10, 10}},
	}
	candidates := []string{"紫晶质瓶"}
	got := findBestMatch(ocr, candidates)
	if got == nil {
		t.Fatal("expected a match")
	}
	if got.box.Y() != 100 {
		t.Errorf("expected top-most match at Y=100, got Y=%d", got.box.Y())
	}
}

// 候选与 OCR 在 Tier B 都被 strip 成空字符串时不应命中（保护 candB == "" 的分支）。
func TestFindBestMatch_TierBEmptyDoesNotMatch(t *testing.T) {
	candidates := []string{"Canned Citrome C"}
	ocr := []ocrItem{{text: "Canned Citrome D", box: maa.Rect{0, 0, 10, 10}}}
	if got := findBestMatch(ocr, candidates); got != nil {
		t.Fatalf("expected no Tier B match when both sides strip to empty, got %+v", got)
	}
}

// 同一 OCR 文本出现在多个位置时，应选最靠上 / 靠左的 box。
func TestFindBestMatch_DuplicateTextPicksTopLeftBox(t *testing.T) {
	candidates := []string{"紫晶质瓶"}
	ocr := []ocrItem{
		{text: "紫晶质瓶", box: maa.Rect{300, 300, 10, 10}},
		{text: "紫晶质瓶", box: maa.Rect{100, 100, 10, 10}},
		{text: "紫晶质瓶", box: maa.Rect{500, 100, 10, 10}},
	}
	got := findBestMatch(ocr, candidates)
	if got == nil {
		t.Fatal("expected a match")
	}
	if got.box.Y() != 100 || got.box.X() != 100 {
		t.Errorf("expected top-left box (100,100), got (%d,%d)", got.box.X(), got.box.Y())
	}
}
