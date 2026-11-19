package essencefilter

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

const vectorAccountID = "3f9c2a7b5d1e8a40"

func inventoryVector(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "inventory", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jsonValue(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeTestBaseline(t *testing.T, a *inventoryAccount, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(a.path()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.path(), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteInventoryVector(t *testing.T) {
	engine := inventoryTestEngine(t)
	a := testInventoryAccount(t.TempDir(), vectorAccountID)
	synctest.Test(t, func(t *testing.T) {
		if err := a.begin(engine); err != nil {
			t.Fatal(err)
		}
		defer a.release()
		scanTime := time.Date(2026, 11, 19, 12, 41, 10, 0, time.UTC)
		s := newSnapshot(a.id)
		s.Coverage[gold] = tierCoverage{CompleteAt: &scanTime}
		s.Coverage[purple] = s.Coverage[gold]
		first := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{2, 1, 1}}
		last := matchapi.OCRInput{Skills: [3]string{"力量", "生命", "医疗"}, Levels: [3]int{4, 5, 3}}
		for _, row := range []struct {
			quality, mark string
			input         matchapi.OCRInput
		}{{gold, lockedMark, first}, {gold, lockedMark, first}, {purple, lockedMark, first}, {gold, discarded, last}} {
			item, err := observeItem(engine, row.input, row.quality, row.mark)
			if err != nil {
				t.Fatal(err)
			}
			s.appendObservation(item)
		}
		s.appendObservation(inventoryItem{Quality: purple, Mark: unmarked})
		time.Sleep(time.Until(time.Date(2026, 11, 19, 12, 58, 3, 0, time.UTC)))
		if err := a.commit(s, false, a.id); err != nil {
			t.Fatal(err)
		}
	})
	got, err := os.ReadFile(a.path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(got, []byte("\n")) || !reflect.DeepEqual(jsonValue(t, got), jsonValue(t, inventoryVector(t, "valid_minimal"))) {
		t.Fatalf("unexpected inventory file: %s", got)
	}
}

func TestReadInventoryVectors(t *testing.T) {
	engine := inventoryTestEngine(t)
	a := testInventoryAccount(t.TempDir(), vectorAccountID)
	writeTestBaseline(t, a, inventoryVector(t, "valid_minimal"))
	minimal, err := a.load(engine)
	if err != nil {
		t.Fatal(err)
	}
	targets := []string{"wpn_sword_0018", "wpn_sword_0021"}
	stock := stockFromGroups(minimal.groups(engine.Weapons()))
	if len(minimal.Items) != 4 || !minimal.targetReady() || minimal.Coverage[purple] != (tierCoverage{Unread: 1}) ||
		stock.Count(targets) != 2 || len(stock.MissingWeapons(targets)) != 0 {
		t.Fatal("wrong coverage or shared stock", minimal, stock)
	}
	for _, name := range []string{"valid_future_fields", "valid_partial", "valid_future_values"} {
		t.Run(name, func(t *testing.T) {
			writeTestBaseline(t, a, inventoryVector(t, name))
			s, err := a.load(engine)
			switch name {
			case "valid_future_values":
				if !errors.Is(err, errInvalidSnapshot) {
					t.Fatal("accepted unsupported inventory", err)
				}
			case "valid_future_fields":
				if err != nil || !reflect.DeepEqual(minimal, s) {
					t.Fatal("future fields changed the known inventory", err)
				}
			case "valid_partial":
				if err != nil || len(s.Items) != 1 || s.targetReady() {
					t.Fatal("incorrect partial baseline", s, err)
				}
				reward := newRewardInventory()
				reward.items[1] = s.Items[0]
				updated := reward.appendTo(s)
				if updated.targetReady() || len(updated.Items) != 2 {
					t.Fatal("partial baseline lost during reward append", updated)
				}
			}
		})
	}
}

func TestInventoryFieldNamesAreCaseSensitive(t *testing.T) {
	engine := inventoryTestEngine(t)
	a := testInventoryAccount(t.TempDir(), vectorAccountID)
	raw := inventoryVector(t, "valid_partial")
	raw = bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 1, "Version": 9`), 1)
	raw = bytes.Replace(raw, []byte(`"mark": "lock"`), []byte(`"mark": "lock", "MARK": null`), 1)
	raw = bytes.Replace(raw, []byte(`"level": 1`), []byte(`"level": 1, "LEVEL": 0`), 1)
	writeTestBaseline(t, a, raw)
	if s, err := a.load(engine); err != nil || len(s.Items) != 1 {
		t.Fatal("unknown fields shadowed known values", err)
	}
}

func TestInvalidInventoryBaseline(t *testing.T) {
	engine := inventoryTestEngine(t)
	a := testInventoryAccount(t.TempDir(), vectorAccountID)
	for _, tc := range []struct{ field, value string }{
		{"format", `"other"`}, {"version", "2"}, {"account_id", `"other"`},
		{"updated_at", "null"}, {"coverage", "null"}, {"items", "null"},
		{"coverage", `{"future":{}}`}, {"coverage", `{"flawless_gold":null}`},
		{"coverage", `{"flawless_gold":{"complete_at":null}}`},
		{"coverage", `{"flawless_gold":{"unread":-1}}`}, {"coverage", `{"flawless_gold":{"unread":0}}`},
		{"coverage", `{"flawless_gold":{"complete_at":"2026-01-01T00:00:00Z","unread":1}}`},
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(inventoryVector(t, "valid_minimal"), &fields); err != nil {
			t.Fatal(err)
		}
		fields[tc.field] = json.RawMessage(tc.value)
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		writeTestBaseline(t, a, raw)
		if _, err := a.load(engine); !errors.Is(err, errInvalidSnapshot) {
			t.Errorf("accepted %s=%s, err %v", tc.field, tc.value, err)
		}
	}
	future := jsonValue(t, inventoryVector(t, "valid_future_values")).(map[string]any)
	items := future["items"].([]any)
	future["coverage"] = map[string]any{}
	first := items[0].(map[string]any)
	skills := first["skills"].([]any)
	invalid := append(items[1:], map[string]any{"quality": gold, "mark": lockedMark, "skills": append(skills, skills[0])})
	for _, item := range invalid {
		future["items"] = []any{item}
		raw, err := json.Marshal(future)
		if err != nil {
			t.Fatal(err)
		}
		writeTestBaseline(t, a, raw)
		if _, err := a.load(engine); !errors.Is(err, errInvalidSnapshot) {
			t.Errorf("accepted item %v, err %v", item, err)
		}
	}
	for _, tc := range []struct{ position, level int }{{0, 0}, {1, 7}, {2, 4}} {
		original := skills[tc.position].(map[string]any)["level"]
		skills[tc.position].(map[string]any)["level"] = tc.level
		future["items"] = []any{first}
		raw, err := json.Marshal(future)
		if err != nil {
			t.Fatal(err)
		}
		writeTestBaseline(t, a, raw)
		if _, err := a.load(engine); !errors.Is(err, errInvalidSnapshot) {
			t.Errorf("accepted level %d at position %d", tc.level, tc.position)
		}
		skills[tc.position].(map[string]any)["level"] = original
	}
}

func modifiedInventoryEngine(t *testing.T, modify func(string, []byte) []byte) *matchapi.Engine {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"matcher_config.json", "skill_pools.json", "weapons_output.json", "locations.json"} {
		raw, err := os.ReadFile(filepath.Join("../../../assets/data/EssenceFilter", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), modify(name, raw), 0644); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := matchapi.NewEngineFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestInventoryUsesCurrentMatcherData(t *testing.T) {
	original := inventoryTestEngine(t)
	changed := modifiedInventoryEngine(t, func(name string, raw []byte) []byte {
		switch name {
		case "matcher_config.json":
			raw = bytes.Replace(raw, []byte(original.DataVersion()), []byte("new test version"), 1)
		case "weapons_output.json":
			raw = bytes.ReplaceAll(raw, []byte("wpn_sword_0018"), []byte("new_test_weapon"))
		}
		return raw
	})
	if original.DataVersion() == changed.DataVersion() {
		t.Fatal("matcher data version did not change")
	}
	a := testInventoryAccount(t.TempDir(), vectorAccountID)
	writeTestBaseline(t, a, inventoryVector(t, "valid_minimal"))
	for _, tc := range []struct {
		engine *matchapi.Engine
		id     string
	}{{original, "wpn_sword_0018"}, {changed, "new_test_weapon"}} {
		s, err := a.load(tc.engine)
		if err != nil || !s.targetReady() || stockFromGroups(s.groups(tc.engine.Weapons())).Count([]string{tc.id}) != 2 {
			t.Fatal("current weapon data did not resolve verified stock", tc.id, err)
		}
	}
}

func TestMissingTermIDOnlyPreventsInventoryObservation(t *testing.T) {
	engine := modifiedInventoryEngine(t, func(name string, raw []byte) []byte {
		if name == "skill_pools.json" {
			raw = bytes.Replace(raw, []byte(`"gat_passive_attr_agi"`), []byte(`""`), 1)
		}
		return raw
	})
	ocr := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{2, 1, 1}}
	match, err := engine.MatchOCR(ocr, matchapi.EssenceFilterOptions{Rarity6Weapon: true})
	if err != nil || match.Kind != matchapi.MatchExact {
		t.Fatal("missing game ID changed normal matching", err)
	}
	if _, ok := engine.TermID(1, 4); ok {
		t.Fatal("empty term ID accepted")
	}
	if _, _, ok := engine.ResolveTermID(""); ok {
		t.Fatal("empty term ID resolved")
	}
	if _, err := observeItem(engine, ocr, gold, lockedMark); err == nil {
		t.Fatal("accepted observation with missing game ID")
	}
}
