// Package core contains device-independent cultivation parsing and planning.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// SourceVersion binds the prototype's layout and SupplyPlan contract.
const SourceVersion = "v2.35.0-beta.1/01d31cf39ef00a8bb5f3efecdfbd309f6648a418"

// Text is one OCR result in reference 1280x720 coordinates.
type Text struct {
	Text  string  `json:"text"`
	Box   [4]int  `json:"box"`
	Score float64 `json:"score"`
}

// Header identifies the selected operator and in-game target.
type Header struct {
	Operator    string `json:"operator"`
	Expectation string `json:"expectation"`
}

// Material stores actual item quantities, not experience points or a deficit.
type Material struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Need  int64  `json:"need"`
	Owned int64  `json:"owned"`
	// Unknown marks an icon missing from the IconRecognition catalog; it is kept for
	// display and never mapped to a farming module.
	Unknown bool `json:"unknown,omitempty"`
	// Weapon names the 干员可用武器 section a row belongs to; empty for the
	// operator's own needs. Only one weapon can be equipped, so the user picks one.
	Weapon string `json:"weapon,omitempty"`
}

// UnknownIDPrefix starts the synthetic ID of an icon missing from the catalog.
const UnknownIDPrefix = "unknown_"

// UnknownID names an uncatalogued icon by its icon hash, stable across passes and pages.
func UnknownID(hash uint64) string {
	return fmt.Sprintf("%s%016x", UnknownIDPrefix, hash)
}

// Plan is a complete two-pass read of one operator; the prototype replaces it.
type Plan struct {
	Version   int        `json:"version"`
	Source    string     `json:"source"`
	Revision  uint64     `json:"revision"`
	Header    Header     `json:"header"`
	ReadAt    time.Time  `json:"read_at"`
	Materials []Material `json:"materials"`
	// Weapons lists the usable weapons in on-screen order.
	Weapons []Weapon `json:"weapons,omitempty"`
	// WeaponChoice is the weapon name the user chose to develop; empty counts
	// only the operator's own materials. It is a setting, not scan data.
	WeaponChoice string      `json:"weapon_choice,omitempty"`
	Completion   *Completion `json:"completion,omitempty"`
}

// ChosenWeapon returns the chosen weapon, or nil when none is chosen.
func (p Plan) ChosenWeapon() *Weapon {
	for i := range p.Weapons {
		if p.Weapons[i].Name == p.WeaponChoice {
			return &p.Weapons[i]
		}
	}
	return nil
}

// Selected keeps the operator's rows and those of the chosen weapon, merged by item.
func (p Plan) Selected() ([]Material, error) {
	if p.WeaponChoice != "" && p.ChosenWeapon() == nil {
		return nil, fmt.Errorf("chosen weapon %q is not in the plan", p.WeaponChoice)
	}
	var picked []Material
	for _, r := range p.Materials {
		if r.Weapon == "" || r.Weapon == p.WeaponChoice {
			r.Weapon = ""
			picked = append(picked, r)
		}
	}
	return Merge(picked)
}

// Completion records explicit user attestation, never inferred module success.
type Completion struct {
	Method   string    `json:"method"`
	Revision uint64    `json:"revision"`
	Header   Header    `json:"header"`
	At       time.Time `json:"at"`
}

// Weapon is a recognized weapon identity; it never implies essence readiness.
type Weapon struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	CandidateIDs  []string `json:"candidate_ids,omitempty"`
	EssenceStatus string   `json:"essence_status"`
	Essence       *Essence `json:"essence,omitempty"`
}

// Essence is what the 武器基质 tab shows for one weapon: the adapted essence
// ("" when 暂无适配基质) and how many owned flawless (gold) essences match its
// three traits. The purple 高纯基质 count beside it does not matter for
// cultivation and is not read. Traits come from the upstream weapon data.
type Essence struct {
	Adapted  string `json:"adapted,omitempty"`
	Flawless int64  `json:"flawless"`
}

// EssenceLine locates one weapon's "无瑕基质 N 高纯基质 M" line on a page.
type EssenceLine struct {
	Weapon  string
	Y       int
	Adapted string
}

var amountPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)([万亿]?)$`)

// Amount accepts the entire numeric label, refusing partial matches and overflow.
// Unlike pkg/ocrnum (substring extraction and clamping), plan quantities require
// exact whole-label validation; they must never silently accept a partial number.
func Amount(s string) (int64, error) {
	s = strings.NewReplacer(" ", "", ",", "", "，", "").Replace(s)
	m := amountPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	x, ok := new(big.Rat).SetString(m[1])
	if !ok {
		return 0, errors.New("invalid rational amount")
	}
	mult := int64(1)
	if m[2] == "万" {
		mult = 10000
	}
	if m[2] == "亿" {
		mult = 100000000
	}
	x.Mul(x, new(big.Rat).SetInt64(mult))
	if !x.IsInt() || !x.Num().IsInt64() {
		return 0, errors.New("non-integral or overflowing amount")
	}
	return x.Num().Int64(), nil
}

// ReadHeader rejects ambiguous/missing identity; coordinates come from SRC fixtures.
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
func OwnedRows(texts []Text) []Text {
	var rows []Text
	for _, t := range texts {
		if t.Score < .6 || t.Box[0] < 420 || t.Box[0] > 1240 || t.Box[1] < 335 || t.Box[1] > 635 {
			continue
		}
		s := strings.ReplaceAll(t.Text, " ", "")
		if strings.HasPrefix(s, "拥有") {
			if _, err := Amount(strings.TrimPrefix(s, "拥有")); err == nil {
				rows = append(rows, t)
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Box[1] < rows[j].Box[1] })
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].Box[1]-rows[i].Box[1] <= 12 {
			j++
		}
		sort.Slice(rows[i:j], func(a, b int) bool { return rows[i+a].Box[0] < rows[i+b].Box[0] })
		i = j
	}
	return rows
}

// MergePage removes only adjacent viewport overlap, retaining repeated later costs.
func MergePage(seen, page []Material) []Material {
	for n := min(len(seen), len(page)); n > 0; n-- {
		if slices.Equal(seen[len(seen)-n:], page[:n]) {
			return append(slices.Clone(seen), page[n:]...)
		}
	}
	return append(slices.Clone(seen), page...)
}

// Merge sums needs by stable item ID within each weapon section and counts
// account inventory once.
func Merge(rows []Material) ([]Material, error) {
	byID := map[string]Material{}
	for _, r := range rows {
		key := r.Weapon + "|" + r.ID
		if r.ID == "" || (r.Name == "" && !r.Unknown) || r.Need < 0 || r.Owned < 0 {
			return nil, errors.New("invalid material identity or amount")
		}
		if old, ok := byID[key]; ok {
			if old.Name != r.Name || old.Owned != r.Owned {
				return nil, fmt.Errorf("inconsistent identity/inventory for %s", r.ID)
			}
			if r.Need > int64(^uint64(0)>>1)-old.Need {
				return nil, errors.New("need overflow")
			}
			r.Need += old.Need
		}
		byID[key] = r
	}
	result := make([]Material, 0, len(byID))
	for _, r := range byID {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Weapon != result[j].Weapon {
			return result[i].Weapon < result[j].Weapon
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// Confirm requires two identical complete reads before producing a new plan.
func Confirm(header Header, first, second []Material, revision uint64, now time.Time) (Plan, error) {
	a, err := Merge(first)
	if err != nil {
		return Plan{}, err
	}
	b, err := Merge(second)
	if err != nil {
		return Plan{}, err
	}
	if header.Operator == "" || header.Expectation == "" || len(a) == 0 || !slices.Equal(a, b) {
		return Plan{}, errors.New("两遍材料不一致、为空或目标未确认，保留旧计划")
	}
	return Plan{Version: 1, Source: SourceVersion, Revision: revision, Header: header, ReadAt: now.UTC(), Materials: a}, nil
}

// TargetSpec describes the upstream unit conversion for a farmable item.
type TargetSpec struct {
	Key    string
	Weight int64
}

// Targets enumerates every SupplyPlan input, including disabled zero goals.
var Targets = []string{"T_CREDS", "PROTOHEDRON", "PROTOPRISM", "COGNITIVE_CARRIER_EXP", "COMBAT_RECORD_EXP", "PROTOSET", "PROTODISK", "WEAPON_EXP", "HEAVY_CAST_DIE", "CAST_DIE", "WARP_PLASMA_COATED_SLIDE", "TRIPHASIC_NANOFLAKE", "QUADRANT_FITTING_FLUID", "TACHYON_SCREENING_LATTICE", "D96_STEEL_SAMPLE_4", "METADIASTIMA_PHOTOEMISSION_TUBE"}

// ItemTargets is bound to the upstream SupplyPlan expression item IDs.
var ItemTargets = map[string]TargetSpec{
	"item_gold": {"T_CREDS", 1}, "item_char_skill_level_7_12": {"PROTOHEDRON", 1}, "item_char_skill_level_1_6": {"PROTOPRISM", 1},
	"item_expcard_stage2_high": {"COGNITIVE_CARRIER_EXP", 10000}, "item_expcard_stage2_low": {"COGNITIVE_CARRIER_EXP", 1000},
	"item_expcard_stage1_high": {"COMBAT_RECORD_EXP", 10000}, "item_expcard_stage1_mid": {"COMBAT_RECORD_EXP", 1000}, "item_expcard_stage1_low": {"COMBAT_RECORD_EXP", 200},
	"item_char_break_stage_3_4": {"PROTOSET", 1}, "item_char_break_stage_1_2": {"PROTODISK", 1},
	"item_weapon_expcard_high": {"WEAPON_EXP", 10000}, "item_weapon_expcard_mid": {"WEAPON_EXP", 1000}, "item_weapon_expcard_low": {"WEAPON_EXP", 200},
	"item_weapon_break_high": {"HEAVY_CAST_DIE", 1}, "item_weapon_break_low": {"CAST_DIE", 1},
	"item_char_skill_specialize_6": {"WARP_PLASMA_COATED_SLIDE", 1}, "item_char_skill_specialize_5": {"TRIPHASIC_NANOFLAKE", 1},
	"item_char_skill_specialize_4": {"QUADRANT_FITTING_FLUID", 1}, "item_char_skill_specialize_3": {"TACHYON_SCREENING_LATTICE", 1},
	"item_char_skill_specialize_2": {"D96_STEEL_SAMPLE_4", 1}, "item_char_skill_specialize_1": {"METADIASTIMA_PHOTOEMISSION_TUBE", 1},
}

// BuildTargets emits total stock goals, never deficits; unsupported items remain manual.
func BuildTargets(plan Plan) (map[string]int64, []Material, error) {
	if plan.Version != 1 || plan.Source != SourceVersion || plan.Header.Operator == "" || plan.Header.Expectation == "" || plan.ReadAt.IsZero() || len(plan.Materials) == 0 {
		return nil, nil, errors.New("invalid/version-mismatched plan")
	}
	if done := plan.Completion; done != nil && (done.Method != "user_confirmed" || done.Revision != plan.Revision || done.Header != plan.Header || done.At.Before(plan.ReadAt)) {
		return nil, nil, errors.New("completion evidence does not bind to this plan")
	}
	rows, err := plan.Selected()
	if err != nil {
		return nil, nil, err
	}
	goals := map[string]int64{}
	for _, k := range Targets {
		goals[k] = 0
	}
	var manual []Material
	for _, r := range rows {
		spec, ok := ItemTargets[r.ID]
		if !ok {
			manual = append(manual, r)
			continue
		}
		if r.Need > (int64(^uint64(0)>>1)-goals[spec.Key])/spec.Weight {
			return nil, nil, errors.New("goal overflow")
		}
		goals[spec.Key] += r.Need * spec.Weight
	}
	return goals, manual, nil
}

// RenderOverrides substitutes only goal placeholders in the upstream option template.
func RenderOverrides(template []byte, goals map[string]int64) (map[string]any, error) {
	if len(goals) != len(Targets) {
		return nil, errors.New("incomplete goal set")
	}
	s := string(template)
	for _, key := range Targets {
		n, ok := goals[key]
		if !ok || n < 0 || n > 2147483647 {
			return nil, fmt.Errorf("invalid goal %s", key)
		}
		s = strings.ReplaceAll(s, "{SupplyPlanLimit_"+key+"}", fmt.Sprint(n))
	}
	if strings.Contains(s, "{SupplyPlanLimit_") {
		return nil, errors.New("unknown goal placeholder")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadEssencePanels finds the count line of every essence panel on the 武器基质
// tab and the weapon section it belongs to. headers are the weapon name texts;
// section is the weapon continuing from the previous page.
func ReadEssencePanels(texts []Text, headers []Text, section string) ([]EssenceLine, string, error) {
	sectionAt := func(y int) string {
		current := section
		for _, h := range headers {
			if h.Box[1] < y {
				current = h.Text
			}
		}
		return current
	}
	var lines []EssenceLine
	for _, t := range texts {
		// OCR may merge the line ("无瑕基质0高纯基质") or read the leading dot
		// icon as a digit ("0 无瑕基质0").
		if !strings.Contains(strings.ReplaceAll(t.Text, " ", ""), "无瑕基质") {
			continue
		}
		name := sectionAt(t.Box[1])
		if name == "" {
			return nil, "", errors.New("essence panel outside a weapon section")
		}
		line := EssenceLine{Weapon: name, Y: t.Box[1]}
		// The panel title sits one line above the counts.
		for _, title := range texts {
			if dy := t.Box[1] - title.Box[1]; dy > 12 && dy < 45 && title.Box[0] >= 540 && title.Box[0] < 900 {
				if text := strings.ReplaceAll(title.Text, " ", ""); text != "暂无适配基质" {
					line.Adapted = text
				}
			}
		}
		lines = append(lines, line)
	}
	return lines, sectionAt(720), nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
