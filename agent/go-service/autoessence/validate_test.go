package autoessence

import "testing"

func TestValidateLocationModeAttachTooManyBaseCheckbox(t *testing.T) {
	raw := map[string]any{
		"menu_mode": "location",
		"s1_1":      true,
		"s1_2":      true,
		"s1_3":      true,
		"s1_4":      true,
		"slot2_id":  2,
	}
	attach := &setupLocationAttach{
		MenuMode: "location",
		Slot2ID:  2,
	}

	if err := validateLocationModeAttach(raw, attach); err == nil {
		t.Fatal("expected validation error for more than 3 base checkboxes")
	}
}

func TestValidateLocationModeAttachTooFewBaseCheckbox(t *testing.T) {
	raw := map[string]any{
		"menu_mode": "location",
		"s1_2":      true,
		"s1_3":      true,
		"slot2_id":  2,
	}
	attach := &setupLocationAttach{
		MenuMode: "location",
		Slot2ID:  2,
	}

	if err := validateLocationModeAttach(raw, attach); err == nil {
		t.Fatal("expected validation error for fewer than 3 base checkboxes")
	}
}

func TestValidateLocationModeAttachExactlyThreeSelections(t *testing.T) {
	raw := map[string]any{
		"menu_mode": "location",
		"s1_2":      true,
		"s1_3":      true,
		"s1_4":      true,
		"slot2_id":  2,
		"s2_2":      true,
	}
	attach := &setupLocationAttach{
		MenuMode: "location",
		Slot2ID:  2,
	}

	if err := validateLocationModeAttach(raw, attach); err != nil {
		t.Fatalf("expected valid attach, got %v", err)
	}
	if err := attach.validateForEngraveOverride(raw); err != nil {
		t.Fatalf("expected valid engrave attach, got %v", err)
	}
}

func TestValidateLocationModeAttachTooManyBonusCheckbox(t *testing.T) {
	raw := map[string]any{
		"menu_mode": "location",
		"s1_2":      true,
		"s1_3":      true,
		"s1_4":      true,
		"slot2_id":  2,
		"s2_1":      true,
		"s2_2":      true,
	}
	attach := &setupLocationAttach{
		MenuMode: "location",
		Slot2ID:  2,
	}

	if err := validateLocationModeAttach(raw, attach); err == nil {
		t.Fatal("expected validation error for more than 1 bonus checkbox")
	}
}

func TestCollectSelectedBaseAttributeIDs(t *testing.T) {
	raw := map[string]any{
		"s1_4": true,
		"s1_2": true,
		"s1_3": true,
	}

	selected, err := collectSelectedBaseAttributeIDs(raw)
	if err != nil {
		t.Fatalf("collectSelectedBaseAttributeIDs failed: %v", err)
	}
	if len(selected) != 3 {
		t.Fatalf("expected 3 ids, got %#v", selected)
	}
	if selected[0] != 2 || selected[1] != 3 || selected[2] != 4 {
		t.Fatalf("unexpected sorted ids: %#v", selected)
	}
}

func TestBuildEngraveSelectionFromCheckboxAttach(t *testing.T) {
	catalog := &skillCatalog{
		slot1ByID: map[int]skillEntry{
			2: {ID: 2, CN: "力量"},
			3: {ID: 3, CN: "意志"},
			4: {ID: 4, CN: "敏捷"},
		},
		slot2ByID: map[int]skillEntry{
			2: {ID: 2, CN: "攻击"},
		},
	}
	raw := map[string]any{
		"s1_2": true,
		"s1_3": true,
		"s1_4": true,
	}
	attach := &setupLocationAttach{Slot2ID: 2}

	selection, err := buildEngraveSelection(catalog, raw, attach)
	if err != nil {
		t.Fatalf("buildEngraveSelection failed: %v", err)
	}
	if selection.base1.CN != "力量" || selection.base2.CN != "意志" || selection.base3.CN != "敏捷" {
		t.Fatalf("unexpected base selection: %#v", selection)
	}
	if selection.bonus.CN != "攻击" {
		t.Fatalf("unexpected bonus selection: %#v", selection.bonus)
	}
}
