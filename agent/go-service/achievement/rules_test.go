package achievement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRulesLoadsAssetsFile(t *testing.T) {
	t.Chdir("..")

	rules, err := readRules(rulesFilePath)
	if err != nil {
		t.Fatalf("readRules(%q) error = %v, want nil", rulesFilePath, err)
	}
	if len(rules) == 0 {
		t.Fatalf("readRules(%q) returned no rules", rulesFilePath)
	}

	foundStarter := false
	for _, r := range rules {
		if r.ID == firstOpenMXUAchievementID {
			foundStarter = true
			if r.Target != 1 {
				t.Fatalf("starter MXU achievement target = %d, want 1", r.Target)
			}
			break
		}
	}
	if !foundStarter {
		t.Fatalf("starter MXU achievement with ID %q not found in rules", firstOpenMXUAchievementID)
	}
}

func TestReadRulesNormalizesNonPositiveTargets(t *testing.T) {
	tmpPath := filepath.Join(t.TempDir(), "rules.json")
	content := `{
    "achievements": [
        {
            "id": "zero-target",
            "title": "Zero Target",
            "event": "mxu_open",
            "target": 0
        },
        {
            "id": "negative-target",
            "title": "Negative Target",
            "event": "mxu_open",
            "target": -5
        }
    ]
}`
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", tmpPath, err)
	}

	rules, err := readRules(tmpPath)
	if err != nil {
		t.Fatalf("readRules(%q) error = %v, want nil", tmpPath, err)
	}
	if len(rules) != 2 {
		t.Fatalf("readRules(%q) returned %d rules, want 2", tmpPath, len(rules))
	}
	for _, r := range rules {
		if r.Target != 1 {
			t.Fatalf("rule %q target = %d, want normalized target 1", r.ID, r.Target)
		}
	}
}

func TestLoadRulesReturnsErrorForInvalidJSON(t *testing.T) {
	invalidPath := writeInvalidRulesFile(t)
	restoreRulesForTest(t)

	if err := loadRules(invalidPath); err == nil {
		t.Fatal("loadRules with invalid JSON error = nil, want error")
	} else if !strings.Contains(err.Error(), "parse achievement rules") {
		t.Fatalf("loadRules error = %v, want parse achievement rules error", err)
	}
}

func TestEnsureRulesLoadedReturnsErrorForInvalidJSON(t *testing.T) {
	invalidPath := writeInvalidRulesFile(t)
	restoreRulesForTest(t)
	oldPath := rulesFilePath

	rulesMu.Lock()
	rules = nil
	rulesMu.Unlock()
	rulesFilePath = invalidPath

	t.Cleanup(func() {
		rulesFilePath = oldPath
	})

	if err := ensureRulesLoaded(); err == nil {
		t.Fatal("ensureRulesLoaded with invalid JSON error = nil, want error")
	} else if !strings.Contains(err.Error(), "parse achievement rules") {
		t.Fatalf("ensureRulesLoaded error = %v, want parse achievement rules error", err)
	}

	if err := os.Remove(invalidPath); err != nil {
		t.Fatalf("os.Remove(%q) error = %v", invalidPath, err)
	}
	if err := ensureRulesLoaded(); err != nil {
		t.Fatalf("second ensureRulesLoaded error = %v, want nil cached empty rules", err)
	}
}

func restoreRulesForTest(t *testing.T) {
	t.Helper()

	rulesMu.Lock()
	oldRules := rules
	rulesMu.Unlock()

	t.Cleanup(func() {
		rulesMu.Lock()
		rules = oldRules
		rulesMu.Unlock()
	})
}

func writeInvalidRulesFile(t *testing.T) string {
	t.Helper()

	invalidPath := filepath.Join(t.TempDir(), "invalid_rules.json")
	if err := os.WriteFile(invalidPath, []byte("{invalid-json"), 0644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", invalidPath, err)
	}
	return invalidPath
}
