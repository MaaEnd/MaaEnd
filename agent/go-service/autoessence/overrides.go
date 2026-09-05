package autoessence

import "fmt"

type baseEngraveSelection struct {
	base1 skillEntry
	base2 skillEntry
	base3 skillEntry
}

func buildBaseEngraveSelection(catalog *skillCatalog, raw map[string]any) (baseEngraveSelection, error) {
	baseIDs, err := collectSelectedBaseAttributeIDs(raw)
	if err != nil {
		return baseEngraveSelection{}, err
	}
	if len(baseIDs) != maxBaseAttributeSelections {
		return baseEngraveSelection{}, fmt.Errorf("expected %d base attributes, got %d", maxBaseAttributeSelections, len(baseIDs))
	}

	base1, err := catalog.slot1Entry(baseIDs[0])
	if err != nil {
		return baseEngraveSelection{}, fmt.Errorf("s1_%d: %w", baseIDs[0], err)
	}
	base2, err := catalog.slot1Entry(baseIDs[1])
	if err != nil {
		return baseEngraveSelection{}, fmt.Errorf("s1_%d: %w", baseIDs[1], err)
	}
	base3, err := catalog.slot1Entry(baseIDs[2])
	if err != nil {
		return baseEngraveSelection{}, fmt.Errorf("s1_%d: %w", baseIDs[2], err)
	}

	return baseEngraveSelection{
		base1: base1,
		base2: base2,
		base3: base3,
	}, nil
}

func buildLocationEngraveOverride(selection baseEngraveSelection) map[string]any {
	baseEntries := [3]skillEntry{selection.base1, selection.base2, selection.base3}
	return map[string]any{
		nodeEngraveCondition1OCR: buildOCROverride(combinedBaseExpectedPatterns(baseEntries)),
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
