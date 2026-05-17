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
	UID          string       `json:"uid"`
	GameDate     string       `json:"game_date"`
	RefreshIndex int          `json:"refresh_index"`
	RefreshCost  int          `json:"refresh_cost,omitempty"`
	UTCTime      string       `json:"utc_time"`
	Slots        []SlotRecord `json:"slots"`
}

func snapshotRecordKey(e snapshotEntry) string {
	return e.UID + "\x00" + e.GameDate + "\x00" + fmt.Sprintf("%d", e.RefreshIndex)
}

func upsertShelfSnapshots(path string, entries []snapshotEntry) (upserted int, err error) {
	if len(entries) == 0 {
		return 0, nil
	}
	storage, err := readSnapshotFile(path)
	if err != nil {
		return 0, err
	}
	indexByKey := make(map[string]int, len(storage.Records))
	for i, r := range storage.Records {
		indexByKey[snapshotRecordKey(r)] = i
	}
	for _, e := range entries {
		key := snapshotRecordKey(e)
		if i, exists := indexByKey[key]; exists {
			storage.Records[i] = e
			log.Info().
				Str("component", component).
				Str("uid", e.UID).
				Str("game_date", e.GameDate).
				Int("refresh_index", e.RefreshIndex).
				Msg("credit shopping shelf snapshot overwritten")
		} else {
			indexByKey[key] = len(storage.Records)
			storage.Records = append(storage.Records, e)
			log.Info().
				Str("component", component).
				Str("uid", e.UID).
				Str("game_date", e.GameDate).
				Int("refresh_index", e.RefreshIndex).
				Msg("credit shopping shelf snapshot appended")
		}
		upserted++
	}
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
	return upserted, nil
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

func logSnapshotSaved(path string, upserted int) {
	log.Info().
		Str("component", component).
		Str("path", path).
		Int("upserted", upserted).
		Msg("credit shopping shelf snapshots write done")
}
