// Package ocrmatch 提供 OutpostTrading 各领域共享的严格规范化 OCR 匹配。
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
package ocrmatch

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

// Item 表示一项 OCR 文本结果及其画面区域。
type Item struct {
	Text string
	Box  maa.Rect
}

// CollectResults 优先使用 Filtered 结果（OCR expected 过滤后的结果），无结果时读取 All。
// 结果保留相同文本的所有坐标：FindBest 会按 Y/X 排序选最靠上 / 靠左的
// box，去重会丢失同一文本在多个位置的候选 box。
func CollectResults(detail *maa.RecognitionDetail) []Item {
	if detail == nil || detail.Results == nil {
		return nil
	}

	for _, group := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.All} {
		var items []Item
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
			items = append(items, Item{Text: text, Box: ocr.Box})
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

// Result 表示某一匹配层级选中的 OCR 项与候选项。
type Result struct {
	OCRText   string
	Candidate string
	Tier      string
	Box       maa.Rect
}

// FindBest 按 Tier A → Tier B 的顺序匹配，任一层命中即返回。
// OCR 结果按屏幕顺序排序，优先命中靠上 / 靠左的文本。Tier 划分见 package doc。
func FindBest(ocrItems []Item, candidates []string) *Result {
	tierACandidates := make([]string, len(candidates))
	tierBCandidates := make([]string, len(candidates))
	for i, c := range candidates {
		tierACandidates[i] = StripSeparators(c)
		tierBCandidates[i] = StripASCIIAlnum(tierACandidates[i])
	}

	sortedItems := SortItemsByPosition(ocrItems)

	for _, item := range sortedItems {
		ocrA := StripSeparators(item.Text)
		if ocrA == "" {
			continue
		}
		for i, candA := range tierACandidates {
			if candA != "" && ocrA == candA {
				return &Result{
					OCRText:   item.Text,
					Candidate: candidates[i],
					Tier:      "A",
					Box:       item.Box,
				}
			}
		}
	}

	// Tier A2: 与 Tier A 相同，只是额外容忍一个被 OCR 粘进名字的前导竖线字形。
	// 只在 Tier A 严格相等失败后才尝试，因此不会把本来就能匹配的名字改坏。
	for _, item := range sortedItems {
		trimmed := stripLeadingBarGlyph(item.Text)
		if trimmed == "" {
			continue
		}
		ocrA := StripSeparators(trimmed)
		if ocrA == "" {
			continue
		}
		for i, candA := range tierACandidates {
			if candA != "" && ocrA == candA {
				return &Result{
					OCRText:   item.Text,
					Candidate: candidates[i],
					Tier:      "A2",
					Box:       item.Box,
				}
			}
		}
	}

	for _, item := range sortedItems {
		ocrB := StripASCIIAlnum(StripSeparators(item.Text))
		if ocrB == "" {
			continue
		}
		for i, candB := range tierBCandidates {
			if candB == "" {
				continue
			}
			if ocrB == candB {
				return &Result{
					OCRText:   item.Text,
					Candidate: candidates[i],
					Tier:      "B",
					Box:       item.Box,
				}
			}
		}
	}

	return nil
}

// SortItemsByPosition 复制 OCR 结果并按画面从上到下、从左到右稳定排序。
func SortItemsByPosition(items []Item) []Item {
	sortedItems := make([]Item, len(items))
	copy(sortedItems, items)
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].Box.Y() != sortedItems[j].Box.Y() {
			return sortedItems[i].Box.Y() < sortedItems[j].Box.Y()
		}
		return sortedItems[i].Box.X() < sortedItems[j].Box.X()
	})
	return sortedItems
}

// latinDiacriticFold 把带变音符号的拉丁字母折回基本字母。
// 库存货品名里出现过 "Jincão Tea" 这类 OCR 结果，而数据表里写的是 "Jincao Tea"；
// 只是字形差异，不是不同的货品，所以两侧统一折叠后再比较。
var latinDiacriticFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'ā': 'a', 'ă': 'a', 'ą': 'a',
	'ç': 'c', 'ć': 'c', 'č': 'c',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e', 'ę': 'e', 'ě': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i', 'į': 'i',
	'ñ': 'n', 'ń': 'n', 'ň': 'n',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o', 'ō': 'o', 'ő': 'o',
	'ś': 's', 'š': 's', 'ß': 's',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ū': 'u', 'ů': 'u', 'ű': 'u',
	'ý': 'y', 'ÿ': 'y',
	'ź': 'z', 'ż': 'z', 'ž': 'z',
}

// ocrBarGlyphs 是货品列表左侧竖线分隔符可能被 OCR 认成的字符。
// 这条竖线紧贴货品名左侧，识别后常被粘进名字里：
// "|LC Wuling Battery" -> "ILC Wuling Battery"。
const ocrBarGlyphs = "|IlL!1"

// stripLeadingBarGlyph 去掉最多一个前导竖线字形以及紧随其后的空白。
// 没有可去掉的内容时返回空字符串，调用方据此跳过。
func stripLeadingBarGlyph(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(t)
	if !strings.ContainsRune(ocrBarGlyphs, r) {
		return ""
	}
	rest := strings.TrimLeft(t[size:], " \t")
	if rest == "" || rest == t {
		return ""
	}
	return rest
}

// StripSeparators 剥除允许差异的分隔字符并统一 ASCII 大小写，保留字母 / 数字 / CJK。
func StripSeparators(s string) string {
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
		if folded, ok := latinDiacriticFold[unicode.ToLower(r)]; ok {
			b.WriteRune(folded)
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// StripASCIIAlnum 在 StripSeparators 基础上再剥除 ASCII 字母与数字，用于 Tier B。
func StripASCIIAlnum(s string) string {
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
