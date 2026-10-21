package cultivation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
)

func isolatedPlan(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "CultivationPlan.json")
	old := recordPath
	recordPath = func() string { return path }
	t.Cleanup(func() { recordPath = old })
	return path
}

func validPlan() core.Plan {
	return core.Plan{Version: 1, Source: core.SourceVersion, Header: core.Header{Operator: "管理员", Expectation: "卓越"}, ReadAt: time.Now().UTC(), Materials: []core.Material{{ID: "item_gold", Name: "折金票", Need: 80, Owned: 30}}}
}

func TestClearInvalidatesPendingScan(t *testing.T) {
	isolatedPlan(t)
	stored, err := loadPlan()
	if err != nil {
		t.Fatal(err)
	}
	revision := stored.Revision
	if err = clearPlan(); err != nil {
		t.Fatal(err)
	}
	if err = savePlan(validPlan(), revision); err == nil {
		t.Fatal("old scan resurrected cleared plan")
	}
	stored, err = loadPlan()
	if err != nil || stored.Plan != nil {
		t.Fatalf("%+v %v", stored, err)
	}
}

func TestCorruptRecordCannotBeOverwritten(t *testing.T) {
	path := isolatedPlan(t)
	for _, raw := range []string{"{broken", "null", "{}", "[]", ""} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := savePlan(validPlan(), 0); err == nil {
			t.Fatalf("overwrote %q", raw)
		}
		if err := clearPlan(); err == nil {
			t.Fatalf("clear overwrote %q", raw)
		}
		data, _ := os.ReadFile(path)
		if string(data) != raw {
			t.Fatal("corrupt original changed")
		}
	}
}

func TestReplaceCompletePlanAndRejectLostUpdate(t *testing.T) {
	isolatedPlan(t)
	p := validPlan()
	if err := savePlan(p, 0); err != nil {
		t.Fatal(err)
	}
	stored, err := loadPlan()
	if err != nil || stored.Plan == nil || stored.Revision != 1 {
		t.Fatalf("%+v %v", stored, err)
	}
	invalid := p
	invalid.Header.Operator = ""
	if err = savePlan(invalid, 1); err == nil {
		t.Fatal("invalid replacement destroyed the previous complete plan")
	}
	if err = savePlan(p, 0); err == nil {
		t.Fatal("accepted stale revision")
	}
	p.Header.Operator = "其他干员"
	if err = savePlan(p, 1); err != nil {
		t.Fatal(err)
	}
	stored, err = loadPlan()
	if err != nil || stored.Plan.Header.Operator != "其他干员" || stored.Revision != 2 {
		t.Fatalf("%+v %v", stored, err)
	}
}

func TestManualConfirmationBindsRevisionAndRescanClearsIt(t *testing.T) {
	isolatedPlan(t)
	if err := confirmPlan(); err == nil {
		t.Fatal("confirmed missing plan")
	}
	if err := savePlan(validPlan(), 0); err != nil {
		t.Fatal(err)
	}
	if err := confirmPlan(); err != nil {
		t.Fatal(err)
	}
	stored, err := loadPlan()
	if err != nil || stored.Plan.Completion == nil || stored.Plan.Completion.Method != "user_confirmed" || stored.Plan.Completion.Revision != stored.Revision {
		t.Fatalf("invalid confirmation: %+v %v", stored, err)
	}
	if err = savePlan(validPlan(), 1); err == nil {
		t.Fatal("in-flight scan erased newer user confirmation")
	}
	if err = savePlan(validPlan(), stored.Revision); err != nil {
		t.Fatal(err)
	}
	stored, err = loadPlan()
	if err != nil || stored.Plan.Completion != nil {
		t.Fatal("new scan inherited old completion")
	}
	if err = clearPlan(); err != nil {
		t.Fatal(err)
	}
	if err = confirmPlan(); err == nil {
		t.Fatal("confirmation resurrected cleared plan")
	}
}

func TestWeaponChoiceInvalidatesOnlyChangedCompletion(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"none_to_weapon", "", "武器A"},
		{"other_weapon", "武器A", "武器B"},
		{"same_weapon", "武器A", "武器A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedPlan(t)
			plan := validPlan()
			plan.Weapons = []core.Weapon{{Name: "武器A"}, {Name: "武器B"}}
			plan.WeaponChoice = tc.before
			if err := savePlan(plan, 0); err != nil {
				t.Fatal(err)
			}
			if err := confirmPlan(); err != nil {
				t.Fatal(err)
			}
			before, _ := loadPlan()
			if err := setWeaponChoice(before.Revision, tc.after); err != nil {
				t.Fatal(err)
			}
			after, err := loadPlan()
			if err != nil {
				t.Fatal(err)
			}
			if (after.Plan.Completion != nil) != (tc.before == tc.after) {
				t.Fatalf("completion must bind to the selected target: %+v", after.Plan)
			}
		})
	}
}

func TestScanWeaponChoiceValidatedBeforeReplacingOldPlan(t *testing.T) {
	path := isolatedPlan(t)
	old := validPlan()
	old.Weapons = []core.Weapon{{Name: "武器A"}}
	old.WeaponChoice = "武器A"
	if err := savePlan(old, 0); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	newPlan := validPlan()
	newPlan.Header.Operator = "新干员"
	newPlan.Weapons = []core.Weapon{{Name: "武器A"}}
	invalid := 4
	if err := savePlanWithWeapon(newPlan, 1, &invalid); err == nil {
		t.Fatal("accepted a weapon outside the newly scanned list")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid scan weapon selection replaced the previous plan")
	}
	none := 0
	if err := savePlanWithWeapon(newPlan, 1, &none); err != nil {
		t.Fatal(err)
	}
	stored, err := loadPlan()
	if err != nil || stored.Revision != 2 || stored.Plan.Header.Operator != "新干员" || stored.Plan.WeaponChoice != "" {
		t.Fatalf("explicit None inherited an old weapon: %+v %v", stored, err)
	}
}

func TestApplyWeaponChoiceClearsInMemoryCompletion(t *testing.T) {
	isolatedPlan(t)
	p := validPlan()
	p.Weapons = []core.Weapon{{Name: "武器A"}}
	if err := savePlan(p, 0); err != nil {
		t.Fatal(err)
	}
	if err := confirmPlan(); err != nil {
		t.Fatal(err)
	}
	stored, _ := loadPlan()
	if err := applyWeaponChoice(nil, stored.Plan, 1); err != nil {
		t.Fatal(err)
	}
	if stored.Plan.Completion != nil || stored.Plan.WeaponChoice != "武器A" {
		t.Fatal("current callback can still display the old confirmation")
	}
}
