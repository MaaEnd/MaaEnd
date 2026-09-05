package autoessence

import "testing"

func TestValidateLocationModeAttachTooManyBaseCheckbox(t *testing.T) {
	raw := map[string]any{
		"menu_mode":  "location",
		"s1_1":       true,
		"s1_2":       true,
		"s1_3":       true,
		"s1_4":       true,
		"slot1_1_id": 2,
		"slot1_2_id": 3,
		"slot1_3_id": 4,
		"slot2_id":   2,
	}
	attach := &setupLocationAttach{
		MenuMode:  "location",
		Slot1_1ID: 2,
		Slot1_2ID: 3,
		Slot1_3ID: 4,
		Slot2ID:   2,
	}

	if err := validateLocationModeAttach(raw, attach); err == nil {
		t.Fatal("expected validation error for more than 3 base checkboxes")
	}
}

func TestValidateLocationModeAttachExactlyThreeSelections(t *testing.T) {
	raw := map[string]any{
		"menu_mode":  "location",
		"slot1_1_id": 2,
		"slot1_2_id": 3,
		"slot1_3_id": 4,
		"slot2_id":   2,
		"s2_2":       true,
	}
	attach := &setupLocationAttach{
		MenuMode:  "location",
		Slot1_1ID: 2,
		Slot1_2ID: 3,
		Slot1_3ID: 4,
		Slot2ID:   2,
	}

	if err := validateLocationModeAttach(raw, attach); err != nil {
		t.Fatalf("expected valid attach, got %v", err)
	}
}

func TestValidateLocationModeAttachTooManyBonusCheckbox(t *testing.T) {
	raw := map[string]any{
		"menu_mode":  "location",
		"slot1_1_id": 2,
		"slot1_2_id": 3,
		"slot1_3_id": 4,
		"slot2_id":   2,
		"s2_1":       true,
		"s2_2":       true,
	}
	attach := &setupLocationAttach{
		MenuMode:  "location",
		Slot1_1ID: 2,
		Slot1_2ID: 3,
		Slot1_3ID: 4,
		Slot2ID:   2,
	}

	if err := validateLocationModeAttach(raw, attach); err == nil {
		t.Fatal("expected validation error for more than 1 bonus checkbox")
	}
}

func TestCollectSelectedBaseAttributeIDsMerged(t *testing.T) {
	raw := map[string]any{
		"s1_5": true,
	}
	attach := &setupLocationAttach{
		Slot1_1ID: 2,
		Slot1_2ID: 3,
		Slot1_3ID: 4,
	}

	selected := collectSelectedBaseAttributeIDs(raw, attach)
	if len(selected) != 4 {
		t.Fatalf("expected 4 distinct base attribute ids, got %d", len(selected))
	}
}
