package cultivation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
)

func TestEmptyPlanExportReplacesPreviousRevision(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", ".."))
	path := filepath.Join(t.TempDir(), "CultivationModules.preview.json")
	old := modulePreviewPath
	modulePreviewPath = func() string { return path }
	t.Cleanup(func() { modulePreviewPath = old })
	p := validPlan()
	p.Revision = 1
	if err := showModules(nil, p, true); err != nil {
		t.Fatal(err)
	}
	p.Revision = 2
	p.Materials = nil
	if err := showModules(nil, p, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got core.ModulePreview
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || len(got.Rows) != 0 || !got.MaterialsReady || got.CultivationDone {
		t.Fatalf("stale or incorrect empty-plan export: %+v", got)
	}
	// Empty material rows do not prove the selected weapon's essence is ready.
	p.Revision = 3
	p.Weapons = []core.Weapon{{Name: "武器A", Essence: &core.Essence{}}}
	p.WeaponChoice = "武器A"
	if err := showModules(nil, p, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 3 || got.MaterialsReady || got.CultivationDone {
		t.Fatalf("missing essence treated as ready: %+v", got)
	}
}
