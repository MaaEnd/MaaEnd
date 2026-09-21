package aicopilot

import (
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

// allowedEntriesResourcePath lists the pipeline entries an AI client may start.
const allowedEntriesResourcePath = "data/AICopilot/allowed_entries.json"

// allowedEntry describes one white-listed pipeline entry.
//
// The text fields take either a literal string or a "$key" reference into
// assets/locales, following the convention of assets/tasks/*.json.
type allowedEntry struct {
	Entry         string `json:"entry"`
	Summary       string `json:"summary"`
	Preconditions string `json:"preconditions,omitempty"`
	Limits        string `json:"limits,omitempty"`
	OnFailure     string `json:"on_failure,omitempty"`
}

type allowedEntriesData struct {
	Tasks []allowedEntry `json:"tasks"`
}

var (
	allowedEntriesOnce sync.Once
	allowedEntries     []allowedEntry
	allowedEntriesErr  error
)

func loadAllowedEntries() ([]allowedEntry, error) {
	allowedEntriesOnce.Do(func() {
		var data allowedEntriesData
		if err := resource.ReadJsonResource(allowedEntriesResourcePath, &data); err != nil {
			allowedEntriesErr = fmt.Errorf("load %s: %w", allowedEntriesResourcePath, err)
			return
		}

		allowedEntries = data.Tasks
		log.Info().
			Str("component", componentName).
			Int("entry_count", len(allowedEntries)).
			Msg("allowed entries loaded")
	})
	return allowedEntries, allowedEntriesErr
}

// findAllowedEntry resolves entry against the white-list so that an AI client
// cannot start arbitrary pipeline nodes.
func findAllowedEntry(entry string) (allowedEntry, error) {
	entries, err := loadAllowedEntries()
	if err != nil {
		return allowedEntry{}, err
	}

	for _, candidate := range entries {
		if candidate.Entry == entry {
			return candidate, nil
		}
	}
	return allowedEntry{}, fmt.Errorf("entry %q is not allowed, call list_tasks for the available entries", entry)
}

// localize resolves a "$key" reference through i18n and passes anything else
// through unchanged.
func localize(text string) string {
	if key, found := strings.CutPrefix(text, "$"); found {
		return i18n.T(key)
	}
	return text
}
