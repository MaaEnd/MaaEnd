package autoessence

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
)

// locationKeyPattern matches what the key is used for: file names, node names and the option generator's
// rewrite patterns in tools/essence_filter/build_autoessence_tasks.py.
var locationKeyPattern = regexp.MustCompile(`^(VF|WL)[A-Za-z0-9]+$`)

var interfaceLocales = []string{"zh_cn", "zh_tw", "en_us", "ja_jp", "ko_kr"}

// TestLocationKeysFile checks what every registered region in location_keys.json needs before target mode
// can route to it: a unique level_id, a VF/WL key, the RegionNodes file with its anchor node, and the region
// label in all five interface locales. The option generator only runs in the sync job, so this is the check
// that runs on pull requests.
func TestLocationKeysFile(t *testing.T) {
	dataDir := testDataDir(t)
	root := filepath.Join(dataDir, "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(dataDir, locationKeysFile))
	if err != nil {
		t.Fatal(err)
	}
	checkNoDuplicateKeys(t, jsonclean.Clean(raw))

	loadTestLocationKeys(t)
	locales := make(map[string]map[string]any, len(interfaceLocales))
	for _, locale := range interfaceLocales {
		locales[locale] = readTestJSON(t, filepath.Join(root, "assets", "locales", "interface", locale+".json"))
	}
	for levelID, key := range locationLevelToKey {
		if !locationKeyPattern.MatchString(key) {
			t.Errorf("%s: key %q must be VF/WL followed by letters and digits", levelID, key)
			continue
		}
		nodes := readTestJSON(t, filepath.Join(root, "assets", "resource", "pipeline", "AutoEssence", "RegionNodes", key+".json"))
		if _, ok := nodes[SetAnchorNode(key)]; !ok {
			t.Errorf("%s: RegionNodes/%s.json has no %s", levelID, key, SetAnchorNode(key))
		}
		labelKey, _ := RegionLabelKey(key)
		for _, locale := range interfaceLocales {
			if label, _ := locales[locale][labelKey].(string); label == "" {
				t.Errorf("%s: %s.json has no %s", levelID, locale, labelKey)
			}
		}
	}
}

func checkNoDuplicateKeys(t *testing.T, raw []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		levelID, _ := tok.(string)
		if seen[levelID] {
			t.Errorf("duplicate level_id %s in %s", levelID, locationKeysFile)
		}
		seen[levelID] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
	}
}

func readTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(jsonclean.Clean(raw), &out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}
