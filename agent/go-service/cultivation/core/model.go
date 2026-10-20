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
}

// Plan is a complete two-pass read of one operator; the prototype replaces it.
type Plan struct {
	Version    int         `json:"version"`
	Source     string      `json:"source"`
	Revision   uint64      `json:"revision"`
	Header     Header      `json:"header"`
	ReadAt     time.Time   `json:"read_at"`
	Materials  []Material  `json:"materials"`
	Weapon     *Weapon     `json:"weapon,omitempty"`
	Completion *Completion `json:"completion,omitempty"`
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

// Merge sums needs by stable item ID and counts account inventory once.
func Merge(rows []Material) ([]Material, error) {
	byID := map[string]Material{}
	for _, r := range rows {
		if r.ID == "" || r.Name == "" || r.Need < 0 || r.Owned < 0 {
			return nil, errors.New("invalid material identity or amount")
		}
		if old, ok := byID[r.ID]; ok {
			if old.Name != r.Name || old.Owned != r.Owned {
				return nil, fmt.Errorf("inconsistent identity/inventory for %s", r.ID)
			}
			if r.Need > int64(^uint64(0)>>1)-old.Need {
				return nil, errors.New("need overflow")
			}
			r.Need += old.Need
		}
		byID[r.ID] = r
	}
	result := make([]Material, 0, len(byID))
	for _, r := range byID {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
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
	rows, err := Merge(plan.Materials)
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
