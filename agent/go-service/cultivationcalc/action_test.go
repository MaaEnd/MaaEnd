package cultivationcalc

import (
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
)

func TestSumCultivationNeedsLevelAndSkill(t *testing.T) {
	need, err := sumCultivationNeeds(defaultCultivationCosts, []string{partLevel, partSkill})
	if err != nil {
		t.Fatal(err)
	}
	if need["T_CREDS"] != 537620+172200 {
		t.Fatalf("T_CREDS=%d", need["T_CREDS"])
	}
	if need[costKeyCombatEXP] != 747060 {
		t.Fatalf("combat=%d", need[costKeyCombatEXP])
	}
	if need[costKeyAdvancedMaterial] != 20+58 {
		t.Fatalf("adv=%d", need[costKeyAdvancedMaterial])
	}
	if need["PROTOHEDRON"] != 118 {
		t.Fatalf("PROTOHEDRON=%d", need["PROTOHEDRON"])
	}
}

func TestCultivationCapacityShortestBoard(t *testing.T) {
	ims.ClearCache()
	t.Cleanup(ims.ClearCache)
	ims.MarkSynced(time.Now(), map[string]int{
		"T_CREDS":                         709820 * 2,
		"ADVANCED_COMBAT_RECORD":          75,
		"INTERMEDIATE_COMBAT_RECORD":      0,
		"ELEMENTARY_COMBAT_RECORD":        0,
		"ADVANCED_COGNITIVE_CARRIER":      105,
		"ELEMENTARY_COGNITIVE_CARRIER":    0,
		"PROTOSET":                        120,
		"PROTODISK":                       66,
		"TRIPHASIC_NANOFLAKE":             78 * 2,
		"QUADRANT_FITTING_FLUID":          78 * 2,
		"TACHYON_SCREENING_LATTICE":       78,
		"D96_STEEL_SAMPLE_4":              78 * 2,
		"METADIASTIMA_PHOTOEMISSION_TUBE": 78 * 2,
		"PROTOHEDRON":                     118 * 2,
		"PROTOPRISM":                      82 * 2,
		"MARK_OF_PERSEVERANCE":            12,
	})

	need, err := sumCultivationNeeds(defaultCultivationCosts, []string{partLevel, partSkill})
	if err != nil {
		t.Fatal(err)
	}
	sets := cultivationCapacity(need)
	if sets != 1 {
		t.Fatalf("sets=%d want 1", sets)
	}
}

func TestCultivationDeficit(t *testing.T) {
	ims.ClearCache()
	t.Cleanup(ims.ClearCache)
	ims.MarkSynced(time.Now(), map[string]int{
		"T_CREDS":             100,
		"PROTOHEDRON":         10,
		"PROTOPRISM":          5,
		"TRIPHASIC_NANOFLAKE": 0,
	})
	need := map[string]int{
		"T_CREDS":               50,
		"PROTOHEDRON":           30,
		costKeyAdvancedMaterial: 10,
	}
	missing := cultivationDeficit(need, 2)
	if _, ok := missing["T_CREDS"]; ok {
		t.Fatalf("T_CREDS should be enough, got %v", missing)
	}
	if missing["PROTOHEDRON"] != 50 {
		t.Fatalf("PROTOHEDRON missing=%d", missing["PROTOHEDRON"])
	}
	if missing["TRIPHASIC_NANOFLAKE"] != 20 {
		t.Fatalf("TRIPHASIC missing=%d", missing["TRIPHASIC_NANOFLAKE"])
	}
}

func TestExpandExpDeficitGreedy(t *testing.T) {
	// 747060 → 74*10000 + 7*1000 + 1*200 (60 leftover rounds up)
	got := expandExpDeficit(costKeyCombatEXP, 747060)
	if got["ADVANCED_COMBAT_RECORD"] != 74 {
		t.Fatalf("adv=%d", got["ADVANCED_COMBAT_RECORD"])
	}
	if got["INTERMEDIATE_COMBAT_RECORD"] != 7 {
		t.Fatalf("mid=%d", got["INTERMEDIATE_COMBAT_RECORD"])
	}
	if got["ELEMENTARY_COMBAT_RECORD"] != 1 {
		t.Fatalf("low=%d", got["ELEMENTARY_COMBAT_RECORD"])
	}
}

func TestCultivationDeficitExpAsItems(t *testing.T) {
	ims.ClearCache()
	t.Cleanup(ims.ClearCache)
	ims.MarkSynced(time.Now(), map[string]int{
		"ADVANCED_COMBAT_RECORD":     10, // 100000 EXP
		"INTERMEDIATE_COMBAT_RECORD": 0,
		"ELEMENTARY_COMBAT_RECORD":   0,
	})
	need := map[string]int{
		costKeyCombatEXP: 747060, // want 1 set
	}
	missing := cultivationDeficit(need, 1)
	// gap = 647060 → 64 adv + 7 mid + 1 low
	if missing["ADVANCED_COMBAT_RECORD"] != 64 {
		t.Fatalf("adv=%d want 64, missing=%v", missing["ADVANCED_COMBAT_RECORD"], missing)
	}
	if missing["INTERMEDIATE_COMBAT_RECORD"] != 7 {
		t.Fatalf("mid=%d", missing["INTERMEDIATE_COMBAT_RECORD"])
	}
	if missing["ELEMENTARY_COMBAT_RECORD"] != 1 {
		t.Fatalf("low=%d", missing["ELEMENTARY_COMBAT_RECORD"])
	}
	if _, ok := missing[costKeyCombatEXP]; ok {
		t.Fatalf("should expand EXP key, got %v", missing)
	}
}

func TestSelectedCultivationParts(t *testing.T) {
	tru := true
	fal := false
	got := selectedCultivationParts(cultivationAttach{
		Level: &tru,
		Skill: &tru,
		Trust: &fal,
	})
	if len(got) != 2 || got[0] != partLevel || got[1] != partSkill {
		t.Fatalf("got=%v", got)
	}
}
