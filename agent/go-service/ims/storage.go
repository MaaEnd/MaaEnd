package ims

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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
)

func defaultRecordPath() string {
	return filepath.Join("debug", "record", recordFileName)
}

func loadRecord() (recordFile, error) {
	path := recordPathFunc()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return recordFile{Items: map[string]int{}}, nil
		}
		return recordFile{}, fmt.Errorf("read ims record: %w", err)
	}
	if len(raw) == 0 {
		return recordFile{Items: map[string]int{}}, nil
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
	return nil
}
