// Package sellproduct 为「🛒售卖产品」任务提供 Go 自定义识别和状态算法。
//
// 货品和干员名称使用分层严格匹配，不使用通用编辑距离：
//
//  1. 分隔符归一化（Tier A）：剥除空白、方括号、竖线、连字符、点号、顿号等常见分隔符并统
//     一大小写后要求严格相等。用于 EN 名在 OCR 里多出 `[` `]` `|` 的情况。
//  2. CJK 纯核归一化（Tier B）：在 Tier A 基础上再剔除 ASCII 字母 / 数字（这些是 CJK 名称
//     里的噪声）；候选做相同处理后要求严格相等。用于 "I紫晶质瓶" → "紫晶质瓶"；而「优质柑
//     实罐头」的 CJK 核心与「柑实罐头」不相等，天然不会被误匹配。
//
// 两层均为严格相等比较，无相似度阈值。候选 EN 名自带 ASCII 字母时 Tier B 会同时剥掉两侧的
// 字母，对 EN 名退化为 Tier A 的等价形式，不会引入新风险。
package sellproduct

import (
	"sort"
	"strings"
	"unicode"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

type ocrItem struct {
	text string
	box  maa.Rect
}

// collectOCRResults 优先使用 Filtered 结果（OCR expected 过滤后的结果），无结果时读取 All。
// 结果保留相同文本的所有坐标：findBestMatch 会按 Y/X 排序选最靠上 / 靠左的
// box，去重会丢失同一文本在多个位置的候选 box。
func collectOCRResults(detail *maa.RecognitionDetail) []ocrItem {
	if detail == nil || detail.Results == nil {
		return nil
	}

	for _, group := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.All} {
		var items []ocrItem
		for _, r := range group {
			if r == nil {
				continue
			}
			ocr, ok := r.AsOCR()
			if !ok {
				continue
			}
			text := strings.TrimSpace(ocr.Text)
			if text == "" {
				continue
			}
			items = append(items, ocrItem{text: text, box: ocr.Box})
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

type matchResult struct {
	ocrText   string
	candidate string
	tier      string
	box       maa.Rect
}

// findBestMatch 按 Tier A → Tier B 的顺序匹配，任一层命中即返回。
// OCR 结果按屏幕顺序排序，优先命中靠上 / 靠左的文本。Tier 划分见 package doc。
func findBestMatch(ocrItems []ocrItem, candidates []string) *matchResult {
	tierACandidates := make([]string, len(candidates))
	tierBCandidates := make([]string, len(candidates))
	for i, c := range candidates {
		tierACandidates[i] = stripSeparators(c)
		tierBCandidates[i] = stripASCIIAlnum(tierACandidates[i])
	}

	sortedItems := sortOCRItemsByPosition(ocrItems)

	for _, item := range sortedItems {
		ocrA := stripSeparators(item.text)
		if ocrA == "" {
			continue
		}
		for i, candA := range tierACandidates {
			if candA != "" && ocrA == candA {
				return &matchResult{
					ocrText:   item.text,
					candidate: candidates[i],
					tier:      "A",
					box:       item.box,
				}
			}
		}
	}

	for _, item := range sortedItems {
		ocrB := stripASCIIAlnum(stripSeparators(item.text))
		if ocrB == "" {
			continue
		}
		for i, candB := range tierBCandidates {
			if candB == "" {
				continue
			}
			if ocrB == candB {
				return &matchResult{
					ocrText:   item.text,
					candidate: candidates[i],
					tier:      "B",
					box:       item.box,
				}
			}
		}
	}

	return nil
}

// sortOCRItemsByPosition 复制 OCR 结果并按画面从上到下、从左到右稳定排序。
func sortOCRItemsByPosition(items []ocrItem) []ocrItem {
	sortedItems := make([]ocrItem, len(items))
	copy(sortedItems, items)
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].box.Y() != sortedItems[j].box.Y() {
			return sortedItems[i].box.Y() < sortedItems[j].box.Y()
		}
		return sortedItems[i].box.X() < sortedItems[j].box.X()
	})
	return sortedItems
}

// stripSeparators 剥除允许差异的分隔字符并统一 ASCII 大小写，保留字母 / 数字 / CJK。
func stripSeparators(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '[', ']', '|', '(', ')', '-', '_', '.', ',', '、', '·', '/', '\\',
			'：', ':', '；', ';':
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// stripASCIIAlnum 在 stripSeparators 基础上再剥除 ASCII 字母与数字，用于 Tier B。
func stripASCIIAlnum(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}
