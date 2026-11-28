package ocrmatch

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 货品列表左侧的竖线分隔符会被 OCR 粘到名字前面并认成 I / l / | 等字形。
// 2026-11-08 的实机 OCR 结果就是 "ILC Wuling Battery"、"I Jincao Drink"、
// "ICuprium Part"、"I Xiranite"，导致 9 个格子只认出 5 个名字。
func TestFindBestToleratesLeadingBarGlyph(t *testing.T) {
	cases := []struct {
		ocr     string
		wantCnd string
	}{
		{"ILC Wuling Battery", "LC Wuling Battery"},
		{"I Jincao Drink", "Jincao Drink"},
		{"ICuprium Part", "Cuprium Part"},
		{"I Xiranite", "Xiranite"},
		{"|LC Wuling Battery", "LC Wuling Battery"},
		{"l Hetonite Part", "Hetonite Part"},
	}

	for _, c := range cases {
		items := []Item{{Text: c.ocr, Box: box(0, 0)}}
		got := FindBest(items, []string{c.wantCnd})
		if got == nil {
			t.Fatalf("%q 应当匹配到 %q，却返回 nil", c.ocr, c.wantCnd)
		}
		if got.Candidate != c.wantCnd {
			t.Fatalf("%q 匹配到 %q，期望 %q", c.ocr, got.Candidate, c.wantCnd)
		}
	}
}

// 变音符号只是字形差异："Jincão Tea" 与数据表里的 "Jincao Tea" 是同一件货。
func TestFindBestFoldsLatinDiacritics(t *testing.T) {
	items := []Item{{Text: "Jincão Tea", Box: box(0, 0)}}
	got := FindBest(items, []string{"Jincao Tea"})
	if got == nil || got.Candidate != "Jincao Tea" {
		t.Fatalf("带变音符号的名字应当匹配到 Jincao Tea，实际 %#v", got)
	}
}

// 严格匹配不能被放宽：真实存在的另一个名字不能因为剥掉前导字形而被误认。
func TestFindBestDoesNotOverMatch(t *testing.T) {
	// 候选里没有 Xiranite，只有别的名字；剥掉前导字形后也不该凭空匹配。
	items := []Item{{Text: "I Xiranite", Box: box(0, 0)}}
	if got := FindBest(items, []string{"Xiranite Chubby Lung"}); got != nil {
		t.Fatalf("不应当匹配到 %q", got.Candidate)
	}

	// 本来就能严格匹配的名字，Tier A 应当先命中，不被 Tier A2 改写。
	items = []Item{{Text: "LC Wuling Battery", Box: box(0, 0)}}
	got := FindBest(items, []string{"LC Wuling Battery"})
	if got == nil || got.Tier != "A" {
		t.Fatalf("严格匹配应当走 Tier A，实际 %#v", got)
	}
}

// CJK 名字行为不变。
func TestFindBestCJKUnchanged(t *testing.T) {
	items := []Item{{Text: "紫晶质瓶", Box: box(0, 0)}}
	got := FindBest(items, []string{"紫晶质瓶"})
	if got == nil || got.Candidate != "紫晶质瓶" {
		t.Fatalf("CJK 严格匹配失败: %#v", got)
	}

	// "I紫晶质瓶" 走 Tier B 的老路径仍然有效（Tier A2 会先命中，结果一致即可）。
	items = []Item{{Text: "I紫晶质瓶", Box: box(0, 0)}}
	got = FindBest(items, []string{"紫晶质瓶"})
	if got == nil || got.Candidate != "紫晶质瓶" {
		t.Fatalf("\"I紫晶质瓶\" 应当匹配到 紫晶质瓶: %#v", got)
	}
}

func box(x, y int) maa.Rect {
	return maa.Rect{x, y, 10, 10}
}
