package cultivation

import (
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
