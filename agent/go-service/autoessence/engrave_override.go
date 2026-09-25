package autoessence

import (
	"fmt"
	"path/filepath"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

type skillPoolEntryJSON struct {
	ID int    `json:"id"`
	CN string `json:"cn"`
	TC string `json:"tc"`
	EN string `json:"en"`
	JP string `json:"jp"`
	KR string `json:"kr"`
}

type skillPoolsFile struct {
	Slot1 []skillPoolEntryJSON `json:"slot1"`
	Slot2 []skillPoolEntryJSON `json:"slot2"`
	Slot3 []skillPoolEntryJSON `json:"slot3"`
}

var skillExpectedCache map[string][]string // "slot:id" -> multilang expected for UI OCR

// ocrENDisplayOverrides replaces skill_pools EN abbreviations that differ from in-game OCR text.
// Match engine / locations.json keep short forms (e.g. "ULT"); only AutoEssence engrave OCR uses these.
var ocrENDisplayOverrides = map[string]string{
	"2:11": "Ultimate Gain",
}

func skillCacheKey(slot, id int) string {
	return fmt.Sprintf("%d:%d", slot, id)
}

func loadSkillExpected(dataDir string) error {
	if skillExpectedCache != nil {
		return nil
	}
	var raw skillPoolsFile
	if err := resource.ReadJsonResource(filepath.Join(dataDir, "skill_pools.json"), &raw); err != nil {
		return err
	}
	cache := make(map[string][]string)
	add := func(slot int, entries []skillPoolEntryJSON) {
		for _, e := range entries {
			expected := ocrExpectedNames(slot, e)
			if len(expected) == 0 {
				continue
			}
			cache[skillCacheKey(slot, e.ID)] = expected
		}
	}
	add(1, raw.Slot1)
	add(2, raw.Slot2)
	add(3, raw.Slot3)
	skillExpectedCache = cache
	return nil
}

// ocrExpectedNames builds multilang OCR expected from skill_pools, applying UI display overrides.
func ocrExpectedNames(slot int, e skillPoolEntryJSON) []string {
	en := e.EN
	if override, ok := ocrENDisplayOverrides[skillCacheKey(slot, e.ID)]; ok {
		en = override
	}
	return uniqueNonEmpty(e.CN, e.TC, en, e.JP, e.KR)
}

func uniqueNonEmpty(vals ...string) []string {
	seen := make(map[string]struct{}, len(vals))
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func expectedForSkill(slot, id int) ([]string, error) {
	if skillExpectedCache == nil {
		return nil, fmt.Errorf("skill expected cache not loaded")
	}
	v, ok := skillExpectedCache[skillCacheKey(slot, id)]
	if !ok || len(v) == 0 {
		return nil, fmt.Errorf("no expected names for slot=%d id=%d", slot, id)
	}
	return append([]string(nil), v...), nil
}

func buildEngravePipelineOverride(plan *FarmPlan) (map[string]any, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil plan")
	}
	selected := make(map[int]bool, 3)
	for _, id := range plan.Slot1IDs {
		selected[id] = true
	}

	patch := make(map[string]any)

	for id := 1; id <= 5; id++ {
		selectNode := fmt.Sprintf("AutoEssenceSelectEngraveBase_s1_%d", id)
		condNode := fmt.Sprintf("AutoEssenceEngraveCondition1Base_s1_%d", id)
		if selected[id] {
			expected, err := expectedForSkill(1, id)
			if err != nil {
				return nil, err
			}
			patch[selectNode] = map[string]any{
				"recognition": map[string]any{
					"type": "OCR",
					"param": map[string]any{
						"roi": []int{
							43,
							125,
							904,
							540,
						},
						"expected": expected,
					},
				},
				"action": map[string]any{
					"type": "Click",
				},
			}
			patch[condNode] = map[string]any{
				"recognition": map[string]any{
					"param": map[string]any{
						"expected": expected,
					},
				},
			}
			continue
		}
		// Unselected: skip click (DirectHit) and keep empty expected so And-check still passes.
		patch[selectNode] = map[string]any{
			"recognition": map[string]any{
				"type":  "DirectHit",
				"param": map[string]any{},
			},
			"action": "DoNothing",
		}
		patch[condNode] = map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"expected": []string{""},
				},
			},
		}
	}

	bonusExpected, err := expectedForSkill(plan.FixedSlot, plan.FixedID)
	if err != nil {
		return nil, err
	}
	patch["AutoEssenceEngraveCondition2OCR"] = map[string]any{
		"recognition": map[string]any{
			"param": map[string]any{
				"expected": bonusExpected,
			},
		},
	}
	patch["AutoEssenceSelectEngraveBonusCondition"] = map[string]any{
		"recognition": map[string]any{
			"type": "OCR",
			"param": map[string]any{
				"roi": []int{
					43,
					125,
					904,
					540,
				},
				"expected": bonusExpected,
			},
		},
	}

	return patch, nil
}
