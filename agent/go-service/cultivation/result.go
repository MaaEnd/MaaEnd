package cultivation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/rs/zerolog/log"
)

// Red is one operator's red-label reading for a game day.
type Red struct {
	Routes       []string `json:"routes"`       // core.Routes of red materials, sorted
	Unrecognized int      `json:"unrecognized"` // red labels whose icon was not identified
	Failed       bool     `json:"failed"`       // operator or expectation not found; caller must not filter
	Day          string   `json:"day"`          // game day YYYY-MM-DD, 04:00 local boundary
}

const gameDayBoundaryHour = 4

// GameDay returns the game day of now as YYYY-MM-DD; each day starts at 04:00 local time.
func GameDay(now time.Time) string {
	t := now.Local()
	if t.Hour() < gameDayBoundaryHour {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

// taskResults holds one task's target and readings; only the latest task is kept.
type taskResults struct {
	id      int64
	active  bool
	target  string
	results map[string]Red
}

var (
	resultMu      sync.Mutex
	current       taskResults
	cachePathFunc = defaultCachePath
)

func defaultCachePath() string {
	return fsutil.OutputPath("debug", "record", "CultivationRed.json")
}

// taskFor returns the state of taskID, dropping any other task's state. Callers hold resultMu.
func taskFor(taskID int64) *taskResults {
	if !current.active || current.id != taskID {
		current = taskResults{id: taskID, active: true, results: map[string]Red{}}
	}
	return &current
}

// SetTarget records the operator Name this task scans next.
func SetTarget(taskID int64, name string) {
	resultMu.Lock()
	defer resultMu.Unlock()
	taskFor(taskID).target = name
}

// target returns the operator Name set for taskID, if any.
func target(taskID int64) (string, bool) {
	resultMu.Lock()
	defer resultMu.Unlock()
	if !current.active || current.id != taskID || current.target == "" {
		return "", false
	}
	return current.target, true
}

// Record stores r as this task's result for name. A reading that did not
// fail is also written to the same-day cache file.
func Record(taskID int64, name string, r Red) {
	r.Routes = slices.Clone(r.Routes)
	resultMu.Lock()
	defer resultMu.Unlock()
	taskFor(taskID).results[name] = r
	if r.Failed {
		return
	}
	cache := readCache()
	cache[name] = r
	if err := writeCache(cache); err != nil {
		log.Warn().Err(err).Str("component", "Cultivation").Msg("failed to write red-label cache")
	}
}

// Checked returns a copy of every result recorded for this task.
func Checked(taskID int64) map[string]Red {
	resultMu.Lock()
	defer resultMu.Unlock()
	out := map[string]Red{}
	if !current.active || current.id != taskID {
		return out
	}
	for name, r := range current.results {
		r.Routes = slices.Clone(r.Routes)
		out[name] = r
	}
	return out
}

// Cached returns name's reading cached for the game day of now. A reading
// from another day or a failed one is not returned.
func Cached(name string, now time.Time) (Red, bool) {
	resultMu.Lock()
	defer resultMu.Unlock()
	r, ok := readCache()[name]
	if !ok || r.Failed || r.Day != GameDay(now) {
		return Red{}, false
	}
	r.Routes = slices.Clone(r.Routes)
	return r, true
}

// readCache loads the cache file; a missing, unreadable or corrupt file is no cache. Callers hold resultMu.
func readCache() map[string]Red {
	path := cachePathFunc()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("component", "Cultivation").Str("path", path).Msg("red-label cache unreadable; ignoring")
		}
		return map[string]Red{}
	}
	cache := map[string]Red{}
	if err := json.Unmarshal(data, &cache); err != nil || cache == nil {
		log.Warn().Err(err).Str("component", "Cultivation").Str("path", path).Msg("red-label cache corrupt; ignoring")
		return map[string]Red{}
	}
	return cache
}

// writeCache atomically replaces the cache file. Callers hold resultMu.
func writeCache(cache map[string]Red) error {
	path := cachePathFunc()
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}
