package creditshopping

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

const (
	shelfSnapshotFileName = "CreditShoppingShelfSnapshots.json"
	maxSnapshotRecords    = 400
	schemaVersion         = 1
)

var resolveShelfSnapshotPathFunc = defaultShelfSnapshotPath

func defaultShelfSnapshotPath() string {
	return filepath.Join("debug", "record", shelfSnapshotFileName)
}

type snapshotFile struct {
	SchemaVersion int             `json:"schema_version"`
	Records       []snapshotEntry `json:"records"`
}

type snapshotEntry struct {
	UID       string       `json:"uid"`
	UTCTime   string       `json:"utc_time"`
	LoopIndex int          `json:"loop_index"`
	Slots     []SlotRecord `json:"slots"`
}

func snapshotPayloadEqual(a, b snapshotEntry) bool {
	if a.UID != b.UID {
		return false
	}
	return slotsEqual(a.Slots, b.Slots)
}

func slotsEqual(a, b []SlotRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Index != b[i].Index ||
			a[i].NameRaw != b[i].NameRaw ||
			a[i].DiscountRaw != b[i].DiscountRaw {
			return false
		}
	}
	return true
}

func filterDuplicateSnapshots(existing []snapshotEntry, incoming []snapshotEntry) []snapshotEntry {
	var last snapshotEntry
	hasLast := len(existing) > 0
	if hasLast {
		last = existing[len(existing)-1]
	}
	out := make([]snapshotEntry, 0, len(incoming))
	for _, e := range incoming {
		if hasLast && snapshotPayloadEqual(e, last) {
			log.Info().
				Str("component", component).
				Str("uid", e.UID).
				Int("loop_index", e.LoopIndex).
				Msg("credit shopping shelf snapshot deduped (same as previous)")
			continue
		}
		out = append(out, e)
		last = e
		hasLast = true
	}
	return out
}

func appendShelfSnapshots(path string, entries []snapshotEntry) (appended int, err error) {
	if len(entries) == 0 {
		return 0, nil
	}
	storage, err := readSnapshotFile(path)
	if err != nil {
		return 0, err
	}
	filtered := filterDuplicateSnapshots(storage.Records, entries)
	deduped := len(entries) - len(filtered)
	if len(filtered) == 0 {
		log.Info().
			Str("component", component).
			Str("path", path).
			Int("incoming", len(entries)).
			Int("deduped", deduped).
			Msg("credit shopping shelf snapshots all deduped, skip write")
		return 0, nil
	}
	storage.Records = append(storage.Records, filtered...)
	if len(storage.Records) > maxSnapshotRecords {
		storage.Records = storage.Records[len(storage.Records)-maxSnapshotRecords:]
	}
	storage.SchemaVersion = schemaVersion
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return 0, fmt.Errorf("create snapshot dir: %w", err)
	}
	raw, err := json.MarshalIndent(storage, "", "    ")
	if err != nil {
		return 0, fmt.Errorf("marshal snapshots: %w", err)
	}
	raw = append(raw, '\n')
	if err := writeFileAtomic(path, raw, 0644); err != nil {
		return 0, err
	}
	return len(filtered), nil
}

func readSnapshotFile(path string) (snapshotFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshotFile{}, nil
		}
		return snapshotFile{}, fmt.Errorf("read snapshot file: %w", err)
	}
	if len(b) == 0 {
		return snapshotFile{}, nil
	}
	var s snapshotFile
	if err := json.Unmarshal(b, &s); err != nil {
		return snapshotFile{}, fmt.Errorf("parse snapshot file: %w", err)
	}
	return s, nil
}

func writeFileAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func logSnapshotSaved(path string, appended, deduped int) {
	log.Info().
		Str("component", component).
		Str("path", path).
		Int("appended", appended).
		Int("deduped", deduped).
		Msg("credit shopping shelf snapshots write done")
}
