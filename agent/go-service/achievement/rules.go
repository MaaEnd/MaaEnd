package achievement

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

// rules is the package-level slice of loaded achievement rules.
// It is populated by loadRules() during Register() or lazily on first access.
var (
	rulesMu sync.RWMutex
	rules   []rule
)

// rulesFilePath is the path to the achievements JSON file.
// It is resolved through the shared resource loader for cross-target compatibility.
var rulesFilePath = "achievements/rules.json"

// rulesFileSchema defines the top-level JSON structure for the rules file.
type rulesFileSchema struct {
	Achievements []ruleSchema `json:"achievements"`
}

// ruleSchema is the JSON-serializable form of a rule.
type ruleSchema struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Event  string `json:"event"`
	Target int    `json:"target"`
	Icon   string `json:"icon,omitempty"`
}

// loadRules reads achievement rules from a JSON file and replaces the
// package-level rules slice. Existing rules are kept when loading fails.
func loadRules(path string) error {
	parsed, err := readRules(path)
	if err != nil {
		return err
	}
	rulesMu.Lock()
	defer rulesMu.Unlock()
	rules = parsed
	return nil
}

func readRules(path string) ([]rule, error) {
	content, err := resource.ReadResource(path)
	if err != nil {
		return []rule{}, fmt.Errorf("read achievement rules: %w", err)
	}

	var schema rulesFileSchema
	if err := json.Unmarshal(content, &schema); err != nil {
		return nil, fmt.Errorf("parse achievement rules: %w", err)
	}

	parsed := make([]rule, 0, len(schema.Achievements))
	for _, rs := range schema.Achievements {
		target := rs.Target
		if target <= 0 {
			target = 1
		}
		parsed = append(parsed, rule{
			ID:     rs.ID,
			Title:  rs.Title,
			Event:  rs.Event,
			Target: target,
		})
	}
	return parsed, nil
}

// ensureRulesLoaded loads rules from the default path if they haven't been
// loaded yet. This provides lazy initialization for code paths that may be
// called before Register() (e.g., in tests or direct use).
func ensureRulesLoaded() error {
	rulesMu.RLock()
	if rules != nil {
		rulesMu.RUnlock()
		return nil
	}
	rulesMu.RUnlock()
	if err := loadRules(rulesFilePath); err != nil {
		rulesMu.Lock()
		if rules == nil {
			rules = []rule{}
		}
		rulesMu.Unlock()
		log.Warn().
			Err(err).
			Str("component", "Achievement").
			Str("path", rulesFilePath).
			Msg("failed to lazily load achievement rules, using empty ruleset")
		return err
	}
	return nil
}

// rulesByEvent returns all rules matching the given event.
func rulesByEvent(event string) []rule {
	_ = ensureRulesLoaded()
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	matched := make([]rule, 0, 1)
	for _, r := range rules {
		if r.Event == event {
			matched = append(matched, r)
		}
	}
	return matched
}

// getRules returns the currently loaded rules slice.
func getRules() []rule {
	_ = ensureRulesLoaded()
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	result := make([]rule, len(rules))
	copy(result, rules)
	return result
}
