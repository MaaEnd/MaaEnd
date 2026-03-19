package matchapi

import (
	"strings"
	"testing"
)

func TestMatchOCR_ExactMatch(t *testing.T) {
	engine, err := NewDefaultEngine()
	if err != nil {
		t.Fatalf("NewDefaultEngine: %v", err)
	}

	var weapon *WeaponData
	for i := range engine.Weapons() {
		w := &engine.Weapons()[i]
		if w.Rarity == 6 {
			weapon = w
			break
		}
	}
	if weapon == nil {
		t.Fatalf("no rarity-6 weapon found in dataset")
	}

	ocr := OCRInput{
		Skills: [3]string{weapon.SkillsChinese[0], weapon.SkillsChinese[1], weapon.SkillsChinese[2]},
		Levels: [3]int{1, 1, 1},
	}
	opts := EssenceFilterOptions{
		Rarity6Weapon:            true,
		Rarity5Weapon:            false,
		Rarity4Weapon:            false,
		KeepFuturePromising:      false,
		FuturePromisingMinTotal:  0,
		LockFuturePromising:      false,
		KeepSlot3Level3Practical: false,
		Slot3MinLevel:            0,
		LockSlot3Practical:       false,
		DiscardUnmatched:         false,
	}

	res, err := engine.MatchOCR(ocr, opts)
	if err != nil {
		t.Fatalf("MatchOCR: %v", err)
	}
	if res == nil {
		t.Fatalf("MatchOCR returned nil result")
	}
	if res.Kind != MatchExact {
		t.Fatalf("expected Kind=MatchExact, got %v", res.Kind)
	}
	if !res.ShouldLock {
		t.Fatalf("expected ShouldLock=true for exact match")
	}
	if res.ShouldDiscard {
		t.Fatalf("expected ShouldDiscard=false for exact match")
	}
	if len(res.Weapons) == 0 {
		t.Fatalf("expected non-empty weapons list for exact match")
	}

	found := false
	for _, w := range res.Weapons {
		if w.InternalID == weapon.InternalID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("exact match weapons did not include internal_id=%s", weapon.InternalID)
	}

	if len(res.SkillIDs) != 3 {
		t.Fatalf("expected SkillIDs len=3, got %d", len(res.SkillIDs))
	}
	for i := 0; i < 3; i++ {
		if res.SkillIDs[i] != weapon.SkillIDs[i] {
			t.Fatalf("SkillIDs mismatch at %d: expected %d got %d", i, weapon.SkillIDs[i], res.SkillIDs[i])
		}
		if res.SkillsChinese[i] != weapon.SkillsChinese[i] {
			t.Fatalf("SkillsChinese mismatch at %d: expected %s got %s", i, weapon.SkillsChinese[i], res.SkillsChinese[i])
		}
	}
	if !strings.HasPrefix(res.Reason, "精准匹配") {
		t.Fatalf("expected Reason to start with 精准匹配, got %q", res.Reason)
	}
	if !strings.Contains(res.Reason, weapon.ChineseName) {
		t.Fatalf("expected Reason to contain weapon name %q, got %q", weapon.ChineseName, res.Reason)
	}
}

func TestMatchOCR_FuturePromising(t *testing.T) {
	engine, err := NewDefaultEngine()
	if err != nil {
		t.Fatalf("NewDefaultEngine: %v", err)
	}

	slot1 := engine.SkillPools().Slot1[0].Chinese
	slot2 := engine.SkillPools().Slot2[0].Chinese
	slot3 := engine.SkillPools().Slot3[0].Chinese

	ocr := OCRInput{
		Skills: [3]string{slot1, slot2, slot3},
		Levels: [3]int{2, 1, 3}, // sum=6
	}
	opts := EssenceFilterOptions{
		Rarity6Weapon:            false,
		Rarity5Weapon:            false,
		Rarity4Weapon:            false, // disable exact matching
		KeepFuturePromising:      true,
		FuturePromisingMinTotal:  6,
		LockFuturePromising:      true,
		KeepSlot3Level3Practical: false,
		Slot3MinLevel:            0,
		LockSlot3Practical:       false,
		DiscardUnmatched:         true,
	}

	res, err := engine.MatchOCR(ocr, opts)
	if err != nil {
		t.Fatalf("MatchOCR: %v", err)
	}
	if res == nil {
		t.Fatalf("MatchOCR returned nil result")
	}
	if res.Kind != MatchFuturePromising {
		t.Fatalf("expected Kind=MatchFuturePromising, got %v", res.Kind)
	}
	if !res.ShouldLock {
		t.Fatalf("expected ShouldLock=true when LockFuturePromising=true")
	}
	if res.ShouldDiscard {
		t.Fatalf("expected ShouldDiscard=false for a matched rule")
	}
	if res.Reason == "" {
		t.Fatalf("expected non-empty Reason for future promising")
	}
}

func TestMatchOCR_Slot3Practical(t *testing.T) {
	engine, err := NewDefaultEngine()
	if err != nil {
		t.Fatalf("NewDefaultEngine: %v", err)
	}

	s3 := engine.SkillPools().Slot3[0]
	s1 := engine.SkillPools().Slot1[0].Chinese
	s2 := engine.SkillPools().Slot2[0].Chinese

	// Put slot3 skill in slot1 OCR position to validate the "slot3 can appear anywhere" logic.
	ocr := OCRInput{
		Skills: [3]string{s3.Chinese, s1, s2},
		Levels: [3]int{3, 1, 1},
	}
	opts := EssenceFilterOptions{
		Rarity6Weapon:            false,
		Rarity5Weapon:            false,
		Rarity4Weapon:            false, // disable exact matching
		KeepFuturePromising:      false,
		FuturePromisingMinTotal:  0,
		LockFuturePromising:      false,
		KeepSlot3Level3Practical: true,
		Slot3MinLevel:            3,
		LockSlot3Practical:       false, // validate ShouldLock=false
		DiscardUnmatched:         true,
	}

	res, err := engine.MatchOCR(ocr, opts)
	if err != nil {
		t.Fatalf("MatchOCR: %v", err)
	}
	if res == nil {
		t.Fatalf("MatchOCR returned nil result")
	}
	if res.Kind != MatchSlot3Level3Practical {
		t.Fatalf("expected Kind=MatchSlot3Level3Practical, got %v", res.Kind)
	}
	if res.ShouldLock {
		t.Fatalf("expected ShouldLock=false when LockSlot3Practical=false")
	}
	if res.ShouldDiscard {
		t.Fatalf("expected ShouldDiscard=false for a matched rule")
	}
	if len(res.SkillIDs) != 3 || res.SkillIDs[2] != s3.ID {
		t.Fatalf("expected SkillIDs to end with slot3 id=%d, got %+v", s3.ID, res.SkillIDs)
	}
	if res.SkillsChinese[2] != s3.Chinese {
		t.Fatalf("expected SkillsChinese[2] == %s, got %s", s3.Chinese, res.SkillsChinese[2])
	}
	if res.Reason == "" {
		t.Fatalf("expected non-empty Reason for slot3 practical")
	}
}

func TestMatchOCR_UnorderedExact(t *testing.T) {
	engine, err := NewDefaultEngine()
	if err != nil {
		t.Fatalf("NewDefaultEngine: %v", err)
	}

	var weapon *WeaponData
	for i := range engine.Weapons() {
		w := &engine.Weapons()[i]
		if w.Rarity == 6 {
			weapon = w
			break
		}
	}
	if weapon == nil {
		t.Fatalf("no rarity-6 weapon found in dataset")
	}

	// Permute OCR order: exact matching should still succeed
	// because MatchOCR will reorder OCR skills into slot1/2/3 by pool assignment.
	ocr := OCRInput{
		Skills: [3]string{weapon.SkillsChinese[2], weapon.SkillsChinese[0], weapon.SkillsChinese[1]},
		Levels: [3]int{1, 1, 1},
	}

	opts := EssenceFilterOptions{
		Rarity6Weapon:            true,
		Rarity5Weapon:            false,
		Rarity4Weapon:            false,
		KeepFuturePromising:      false,
		FuturePromisingMinTotal:  0,
		LockFuturePromising:      false,
		KeepSlot3Level3Practical: false,
		Slot3MinLevel:            0,
		LockSlot3Practical:       false,
		DiscardUnmatched:         false,
	}

	res, err := engine.MatchOCR(ocr, opts)
	if err != nil {
		t.Fatalf("MatchOCR: %v", err)
	}
	if res == nil {
		t.Fatalf("MatchOCR returned nil result")
	}
	if res.Kind != MatchExact {
		t.Fatalf("expected Kind=MatchExact, got %v", res.Kind)
	}
	if !res.ShouldLock {
		t.Fatalf("expected ShouldLock=true for exact match")
	}
	if res.ShouldDiscard {
		t.Fatalf("expected ShouldDiscard=false for exact match")
	}

	found := false
	for _, w := range res.Weapons {
		if w.InternalID == weapon.InternalID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unordered exact match weapons did not include internal_id=%s", weapon.InternalID)
	}
	if !strings.HasPrefix(res.Reason, "精准匹配") {
		t.Fatalf("expected Reason to start with 精准匹配, got %q", res.Reason)
	}
	if !strings.Contains(res.Reason, weapon.ChineseName) {
		t.Fatalf("expected Reason to contain weapon name %q, got %q", weapon.ChineseName, res.Reason)
	}
}

func TestMatchOCR_MatchNoneReason(t *testing.T) {
	engine, err := NewDefaultEngine()
	if err != nil {
		t.Fatalf("NewDefaultEngine: %v", err)
	}

	ocr := OCRInput{
		Skills: [3]string{"__no_such_skill_1__", "__no_such_skill_2__", "__no_such_skill_3__"},
		Levels: [3]int{1, 1, 1},
	}
	baseOpts := EssenceFilterOptions{
		Rarity6Weapon:            false,
		Rarity5Weapon:            false,
		Rarity4Weapon:            false,
		KeepFuturePromising:      false,
		FuturePromisingMinTotal:  0,
		LockFuturePromising:      false,
		KeepSlot3Level3Practical: false,
		Slot3MinLevel:            0,
		LockSlot3Practical:       false,
	}

	t.Run("discard_unmatched_true", func(t *testing.T) {
		opts := baseOpts
		opts.DiscardUnmatched = true
		res, err := engine.MatchOCR(ocr, opts)
		if err != nil {
			t.Fatalf("MatchOCR: %v", err)
		}
		if res.Kind != MatchNone {
			t.Fatalf("expected Kind=MatchNone, got %v", res.Kind)
		}
		if res.Reason != "未匹配" {
			t.Fatalf("expected Reason=未匹配, got %q", res.Reason)
		}
		if !res.ShouldDiscard {
			t.Fatalf("expected ShouldDiscard=true")
		}
	})

	t.Run("discard_unmatched_false", func(t *testing.T) {
		opts := baseOpts
		opts.DiscardUnmatched = false
		res, err := engine.MatchOCR(ocr, opts)
		if err != nil {
			t.Fatalf("MatchOCR: %v", err)
		}
		if res.Kind != MatchNone {
			t.Fatalf("expected Kind=MatchNone, got %v", res.Kind)
		}
		if res.Reason != "未匹配" {
			t.Fatalf("expected Reason=未匹配, got %q", res.Reason)
		}
		if res.ShouldDiscard {
			t.Fatalf("expected ShouldDiscard=false")
		}
	})
}
