package ims

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	recordFileName = "IMS.json"
)

// recordFile is the on-disk snapshot at debug/record/IMS.json.
type recordFile struct {
	UpdatedAt time.Time      `json:"updated_at"`
	Items     map[string]int `json:"items"`
}

var (
	recordPathFunc = defaultRecordPath
	recordMu       sync.Mutex
	// hydrated is true after a successful one-shot disk→memory load, an intentional
	// ClearCache, or a successful persist. Subsequent reads use memory only.
	hydrated bool
)

func defaultRecordPath() string {
	return filepath.Join("debug", "record", recordFileName)
}

func emptyRecord() recordFile {
	return recordFile{Items: map[string]int{}}
}

func loadRecord() (recordFile, error) {
	path := recordPathFunc()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyRecord(), nil
		}
		return recordFile{}, fmt.Errorf("read ims record: %w", err)
	}
	if len(raw) == 0 {
		return emptyRecord(), nil
	}
	var rec recordFile
	if err := json.Unmarshal(raw, &rec); err != nil {
		return recordFile{}, fmt.Errorf("unmarshal ims record: %w", err)
	}
	if rec.Items == nil {
		rec.Items = map[string]int{}
	}
	return rec, nil
}

// resetCorruptRecord overwrites a damaged IMS.json with an empty valid snapshot.
// Save failure is logged only: callers continue with empty in-memory cache.
func resetCorruptRecord(cause error) {
	path := recordPathFunc()
	log.Warn().
		Err(cause).
		Str("path", path).
		Msg("ims record corrupt, resetting file")
	if err := saveRecord(emptyRecord()); err != nil {
		log.Error().
			Err(err).
			Str("path", path).
			Msg("failed to reset corrupt ims record, continuing with empty memory")
	}
}

func saveRecord(rec recordFile) error {
	path := recordPathFunc()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create ims record dir: %w", err)
	}
	if rec.Items == nil {
		rec.Items = map[string]int{}
	}
	raw, err := json.MarshalIndent(rec, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal ims record: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+recordFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create ims record temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write ims record temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close ims record temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename ims record: %w", err)
	}
	cleanup = false
	return nil
}

// ensureHydrated loads debug/record/IMS.json into memory at most once per process
// (until ClearCache). Hot-path reads stay in memory afterwards.
// A missing or empty file is treated as an empty cache. A corrupt file is reset
// to empty JSON so every IMS component can continue instead of failing the task.
func ensureHydrated() error {
	recordMu.Lock()
	defer recordMu.Unlock()
	if hydrated {
		return nil
	}

	rec, err := loadRecord()
	if err != nil {
		resetCorruptRecord(err)
		globalCache.clear()
		hydrated = true
		return nil
	}
	if !rec.UpdatedAt.IsZero() {
		globalCache.markSynced(rec.UpdatedAt, rec.Items)
		log.Info().
			Time("updated_at", rec.UpdatedAt.UTC()).
			Int("item_count", len(rec.Items)).
			Msg("ims cache hydrated from disk")
	} else if len(rec.Items) > 0 {
		globalCache.setItemsOnly(rec.Items)
		log.Info().
			Int("item_count", len(rec.Items)).
			Msg("ims item quantities hydrated from disk without sync timestamp")
	}
	hydrated = true
	return nil
}

// persistSynced writes debug/record/IMS.json and updates the in-process cache.
func persistSynced(at time.Time, items map[string]int) error {
	recordMu.Lock()
	defer recordMu.Unlock()

	copied := make(map[string]int, len(items))
	for k, v := range items {
		copied[k] = v
	}
	if err := saveRecord(recordFile{UpdatedAt: at.UTC(), Items: copied}); err != nil {
		return err
	}
	MarkSynced(at, copied)
	hydrated = true
	return nil
}
