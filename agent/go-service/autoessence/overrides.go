package autoessence

import "fmt"

type engraveSelection struct {
	base1 skillEntry
	base2 skillEntry
	base3 skillEntry
	bonus skillEntry
}

func buildEngraveSelection(catalog *skillCatalog, raw map[string]any, attach *setupLocationAttach) (engraveSelection, error) {
	baseIDs, err := collectSelectedBaseAttributeIDs(raw)
	if err != nil {
		return engraveSelection{}, err
	}
	if len(baseIDs) != maxBaseAttributeSelections {
		return engraveSelection{}, fmt.Errorf("expected %d base attributes, got %d", maxBaseAttributeSelections, len(baseIDs))
	}

	base1, err := catalog.slot1Entry(baseIDs[0])
	if err != nil {
		return engraveSelection{}, fmt.Errorf("s1_%d: %w", baseIDs[0], err)
	}
	base2, err := catalog.slot1Entry(baseIDs[1])
	if err != nil {
		return engraveSelection{}, fmt.Errorf("s1_%d: %w", baseIDs[1], err)
	}
	base3, err := catalog.slot1Entry(baseIDs[2])
	if err != nil {
		return engraveSelection{}, fmt.Errorf("s1_%d: %w", baseIDs[2], err)
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
