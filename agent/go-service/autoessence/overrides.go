package autoessence

import (
	"fmt"
)

type engraveSelection struct {
	base1 skillEntry
	base2 skillEntry
	base3 skillEntry
	bonus skillEntry
}

func buildEngraveSelection(catalog *skillCatalog, attach *setupLocationAttach) (engraveSelection, error) {
	base1, err := catalog.slot1Entry(attach.Slot1_1ID)
	if err != nil {
		return engraveSelection{}, fmt.Errorf("slot1_1_id: %w", err)
	}
	base2, err := catalog.slot1Entry(attach.Slot1_2ID)
	if err != nil {
		return engraveSelection{}, fmt.Errorf("slot1_2_id: %w", err)
	}
	base3, err := catalog.slot1Entry(attach.Slot1_3ID)
	if err != nil {
		return engraveSelection{}, fmt.Errorf("slot1_3_id: %w", err)
	}
	bonus, err := catalog.slot2Entry(attach.Slot2ID)
	if err != nil {
		return engraveSelection{}, fmt.Errorf("slot2_id: %w", err)
	}

	return engraveSelection{
		base1: base1,
		base2: base2,
		base3: base3,
		bonus: bonus,
	}, nil
}

func buildLocationEngraveOverride(selection engraveSelection) map[string]any {
	baseEntries := [3]skillEntry{selection.base1, selection.base2, selection.base3}
	return map[string]any{
		nodeEngraveCondition1OCR:        buildOCROverride(combinedBaseExpectedPatterns(baseEntries)),
		nodeEngraveCondition2OCR:        buildOCROverride(skillExpectedTexts(selection.bonus)),
		nodeSelectEngraveBaseCondition1: buildOCROverride(skillExpectedTexts(selection.base1)),
		nodeSelectEngraveBaseCondition2: buildOCROverride(skillExpectedTexts(selection.base2)),
		nodeSelectEngraveBaseCondition3: buildOCROverride(skillExpectedTexts(selection.base3)),
		nodeSelectEngraveBonusCondition: buildOCROverride(skillExpectedTexts(selection.bonus)),
	}
}

func buildOCROverride(expected []string) map[string]any {
	return map[string]any{
		"recognition": map[string]any{
			"param": map[string]any{
				"expected": expected,
			},
		},
	}
}
