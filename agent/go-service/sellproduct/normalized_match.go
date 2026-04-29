// Package sellproduct 为「🛒售卖产品」任务提供 Go 自定义识别。
//
// 核心识别：SellProductNormalizedItemMatch —— 在指定 ROI 内跑一次 OCR，
// 对每条 OCR 文本与 candidates 做抗噪声匹配，命中后返回该文本的 box。
//
// 引入原因：见 MaaEnd issue #2344。原实现用锚定正则 `^紫晶质瓶$` 去
// 匹配 OCR 结果，遇到噪声前缀（如 "I紫晶质瓶"）就直接 miss，回退卖
// 默认货品。
//
// 关键约束：不得回退 PR #1790（issue #1793）修复的子串混淆问题，即
// 设置「柑实罐头」为优先货品时不应误匹配「优质柑实罐头」「精选柑实
// 罐头」「精选优质柑实罐头」。为此，匹配采用精确层级策略，完全不使
// 用通用编辑距离：
//
//  1. 分隔符归一化（Tier A）：剥除空白、方括号、竖线、连字符、点号、
//     顿号等常见分隔符并统一大小写后要求严格相等。用于 EN 名在 OCR
//     里多出 `[` `]` `|` 的情况，如 "Canned Citrome [C]"。
//  2. CJK 纯核归一化（Tier B）：在 Tier A 基础上，再从 OCR 文本里
//     剔除 ASCII 字母 / 数字（这些是 CJK 名称里的噪声）；候选做相同
//     处理。要求严格相等。用于 "I紫晶质瓶" → "紫晶质瓶"；而
//     "优质柑实罐头" 的 CJK 核心是 "优质柑实罐头"，与候选 "柑实罐头"
//     的 CJK 核心不相等，天然不会被误匹配。
//
// 以上两层都是 *严格相等* 比较，没有相似度阈值可调。候选 EN 名里自
// 带 ASCII 字母（如 "Canned Citrome C"）时 Tier B 会同时把候选和
// OCR 的字母都剥掉，此时 Tier B 对 EN 名退化为 Tier A 的等价形式，
// 不会引入新的风险。
package sellproduct

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "SellProductNormalizedItemMatch"

type params struct {
	Candidates []string `json:"candidates"`
}

type NormalizedMatchRecognition struct{}

var _ maa.CustomRecognitionRunner = (*NormalizedMatchRecognition)(nil)

func (r *NormalizedMatchRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Warn().Str("component", componentName).Msg("arg or image is nil")
		return nil, false
	}

	p, err := parseParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("raw_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	if len(p.Candidates) == 0 {
		log.Warn().Str("component", componentName).Msg("candidates is empty, nothing to match")
		return nil, false
	}

	ocrParam := maa.OCRParam{
		ROI: maa.NewTargetRect(arg.Roi),
	}
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, ocrParam, arg.Img)
	if err != nil || detail == nil {
		log.Warn().
			Err(err).
			Str("component", componentName).
			Interface("roi", arg.Roi).
			Msg("inner OCR failed")
		return nil, false
	}

	ocrItems := collectOCRResults(detail)
	if len(ocrItems) == 0 {
		log.Debug().
			Str("component", componentName).
			Interface("roi", arg.Roi).
			Msg("no OCR text in ROI")
		return nil, false
	}

	best := findBestMatch(ocrItems, p.Candidates)
	if best == nil {
		ocrTexts := make([]string, 0, len(ocrItems))
		for _, it := range ocrItems {
			ocrTexts = append(ocrTexts, it.text)
		}
		log.Debug().
			Str("component", componentName).
			Strs("ocr_texts", ocrTexts).
			Strs("candidates", p.Candidates).
			Msg("no candidate matched")
		return nil, false
	}

	log.Debug().
		Str("component", componentName).
		Str("ocr_text", best.ocrText).
		Str("matched_candidate", best.candidate).
		Str("match_tier", best.tier).
		Interface("box", best.box).
		Msg("normalized match hit")

	detailJSON, _ := json.Marshal(map[string]any{
		"ocr_text":          best.ocrText,
		"matched_candidate": best.candidate,
		"match_tier":        best.tier,
	})

	return &maa.CustomRecognitionResult{
		Box:    best.box,
		Detail: string(detailJSON),
	}, true
}

func parseParams(raw string) (*params, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("custom_recognition_param is empty")
	}
	var p params
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	return &p, nil
}

type ocrItem struct {
	text string
	box  maa.Rect
}

// collectOCRResults 优先使用 Filtered 结果（OCR expected 过滤后的结果）；
// 若为空则退回 All。不按文本去重：findBestMatch 会按 Y/X 排序选最靠上 / 靠左的
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
// OCR 结果按屏幕顺序排序，优先命中靠上 / 靠左的文本。
//
// Tier A 优先级最高：若某条 OCR 文本经 stripSeparators 后严格等于某
// 候选，直接返回。这保证完全正确的 OCR 结果不会被 Tier B 覆盖。
//
// Tier B 仅在 Tier A 无命中时启用，用于消化 "I紫晶质瓶" 这种 ASCII
// 字母 / 数字噪声。CJK 文本的 CJK 纯核不会改变；候选也做相同处理。
func findBestMatch(ocrItems []ocrItem, candidates []string) *matchResult {
	tierACandidates := make([]string, len(candidates))
	tierBCandidates := make([]string, len(candidates))
	for i, c := range candidates {
		tierACandidates[i] = stripSeparators(c)
		tierBCandidates[i] = stripASCIIAlnum(tierACandidates[i])
	}

	sortedItems := make([]ocrItem, len(ocrItems))
	copy(sortedItems, ocrItems)
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].box.Y() != sortedItems[j].box.Y() {
			return sortedItems[i].box.Y() < sortedItems[j].box.Y()
		}
		return sortedItems[i].box.X() < sortedItems[j].box.X()
	})

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

// stripSeparators 剥除 OCR / 候选里允许存在差异的分隔字符，并统一
// ASCII 大小写。保留字母、数字、CJK 等 "有效字符"。
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

// stripASCIIAlnum 从已去分隔符的字符串里再剥除 ASCII 字母与数字。
// 用于 Tier B：让 "I紫晶质瓶" 的纯 CJK 内核等于 "紫晶质瓶"。
// 对 EN 候选（如 "Canned Citrome C"）和 EN OCR 同时应用时，两侧
// 都会被剥成空串或剥到共同非字母字符，不会造成 false positive。
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
