// Package core contains device-independent cultivation recognition helpers.
package core

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

// Text is an OCR result in 1280x720 coordinates.
type Text struct {
	Text  string  `json:"text"`
	Box   [4]int  `json:"box"`
	Score float64 `json:"score"`
}

// Header binds this run to the selected operator and cultivation expectation.
type Header struct{ Operator, Expectation string }

// Material identifies a red missing-material label, without a stock snapshot.
type Material struct{ ID, Name string }

var ownedLabel = regexp.MustCompile(`^拥有[0-9][0-9,.，万亿]*$`)

// ReadHeader rejects ambiguous/missing identity; using the preserved 1280x720 overview layout.
func ReadHeader(texts []Text) (Header, error) {
	var h Header
	operators, expectations := map[string]bool{}, map[string]bool{}
	title := false
	for _, t := range texts {
		if t.Score < .6 {
			continue
		}
		s := strings.ReplaceAll(t.Text, " ", "")
		if t.Box[1] < 65 && strings.Contains(s, "培养总览") {
			title = true
		}
		if t.Box[0] >= 470 && t.Box[0] <= 700 && t.Box[1] >= 70 && t.Box[1] <= 110 {
			// These are the two OCR spellings observed for the same operator.
			// Keep all other names exact so a real identity change still stops.
			if s == "別礼" {
				s = "别礼"
			}
			operators[s] = true
		}
		if t.Box[0] >= 960 && t.Box[1] >= 90 && t.Box[1] <= 150 && strings.HasPrefix(s, "培养期望") {
			s = strings.TrimLeft(strings.TrimPrefix(s, "培养期望"), "：:")
			if s != "" {
				expectations[s] = true
			}
		}
	}
	if !title || len(operators) != 1 || len(expectations) != 1 {
		return h, errors.New("培养总览、干员或培养期望未确认")
	}
	for s := range operators {
		h.Operator = s
	}
	for s := range expectations {
		h.Expectation = s
	}
	return h, nil
}

// SplitOwned splits OCR results that merged neighbouring "拥有N" labels, such as
// "拥有2.73万 拥有4173", into one text per label. Each part gets a box estimated
// from its character offset; callers snap the icon cell to the card afterwards.
func SplitOwned(texts []Text) []Text {
	var out []Text
	for _, t := range texts {
		runes := []rune(t.Text)
		var starts []int
		for i := 0; i+1 < len(runes); i++ {
			if runes[i] == '拥' && runes[i+1] == '有' {
				starts = append(starts, i)
			}
		}
		if len(starts) < 2 {
			out = append(out, t)
			continue
		}
		width := float64(t.Box[2]) / float64(len(runes))
		for k, start := range starts {
			end := len(runes)
			if k+1 < len(starts) {
				end = starts[k+1]
			}
			part := strings.TrimSpace(string(runes[start:end]))
			x := t.Box[0] + int(float64(start)*width)
			w := int(float64(len([]rune(part))) * width)
			out = append(out, Text{Text: part, Box: [4]int{x, t.Box[1], w, t.Box[3]}, Score: t.Score})
		}
	}
	return out
}

// OwnedRows returns stable row/column order, excluding other screen text.
func OwnedRows(texts []Text, scrolled bool) []Text {
	// On a scrolled page the top row sits under the list's fade and its icon can
	// fail recognition; one 240 px swipe kept it whole near the previous page's
	// bottom, so it is skipped here.
	top := 335
	if scrolled {
		top = 355
	}
	var rows []Text
	for _, t := range texts {
		// The bottom limit is applied per row below; label boxes jitter by a few pixels.
		if t.Score < .6 || t.Box[0] < 420 || t.Box[0] > 1240 || t.Box[1] < top || t.Box[1] > 635+12 {
			continue
		}
		s := strings.ReplaceAll(t.Text, " ", "")
		if strings.HasPrefix(s, "拥有") {
			if ownedLabel.MatchString(s) {
				rows = append(rows, t)
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Box[1] < rows[j].Box[1] })
	var kept []Text
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].Box[1]-rows[i].Box[1] <= 12 {
			j++
		}
		// Keep or drop a row as a whole, so a row at the viewport edge is never
		// half read near the fade at the viewport edge.
		if rows[i].Box[1] <= 635 {
			sort.Slice(rows[i:j], func(a, b int) bool { return rows[i+a].Box[0] < rows[i+b].Box[0] })
			kept = append(kept, rows[i:j]...)
		}
		i = j
	}
	return kept
}

// NothingNeeded reports the page's own statement that this tab needs nothing,
// e.g. 「干员等级已达上限」 or 「干员技能与天赋培养已满级」 on a maxed operator. An empty read alone is never
// taken as no demand.
func NothingNeeded(texts []Text) bool {
	for _, text := range texts {
		// 「干员等级已达上限」「干员技能与天赋培养已满级」 (PC sample from the user).
		for _, marker := range []string{"已达上限", "已满级", "暂无养成需求", "暂无材料需求"} {
			if strings.Contains(strings.ReplaceAll(text.Text, " ", ""), marker) {
				return true
			}
		}
	}
	return false
}
