package achievement

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxEventRecords         = 200
	maxRecentUnlocks        = 20
	maxPendingNotifications = 20
)

var (
	resolveStoragePathFunc = resolveStoragePath
	storageMu              sync.Mutex
)

func resolveStoragePath() string {
	return filepath.Join("debug", "record", achievementStorageFileName)
}

func recordEvent(path string, event string, increment int, dedupeKey string, now time.Time) ([]unlockLog, error) {
	if event == "" {
		return nil, fmt.Errorf("event is required")
	}
	if increment <= 0 {
		increment = 1
	}

	storageMu.Lock()
	defer storageMu.Unlock()

	storage, err := readStorageFileUnlocked(path)
	if err != nil {
		return nil, err
	}
	ensureStorageDefaults(&storage)

	if dedupeKey != "" && hasEventDedupeKey(storage.Events, dedupeKey) {
		return nil, nil
	}

	utcTime := now.UTC().Format(time.RFC3339)
	storage.Events = append(storage.Events, eventLog{
		Event:     event,
		Increment: increment,
		DedupeKey: dedupeKey,
		UTCTime:   utcTime,
	})
	if len(storage.Events) > maxEventRecords {
		storage.Events = storage.Events[len(storage.Events)-maxEventRecords:]
	}

	unlocks := applyRules(&storage, event, increment, utcTime)
	if len(unlocks) > 0 {
		storage.RecentUnlocks = append(storage.RecentUnlocks, unlocks...)
		if len(storage.RecentUnlocks) > maxRecentUnlocks {
			storage.RecentUnlocks = storage.RecentUnlocks[len(storage.RecentUnlocks)-maxRecentUnlocks:]
		}
		storage.PendingNotifications = append(storage.PendingNotifications, unlocks...)
		if len(storage.PendingNotifications) > maxPendingNotifications {
			storage.PendingNotifications = storage.PendingNotifications[len(storage.PendingNotifications)-maxPendingNotifications:]
		}
	}
	storage.UpdatedAt = utcTime

	if err := writeStorageFileUnlocked(path, storage); err != nil {
		return nil, err
	}
	return unlocks, nil
}

func consumePendingNotifications(path string, now time.Time) ([]unlockLog, error) {
	storageMu.Lock()
	defer storageMu.Unlock()

	storage, err := readStorageFileUnlocked(path)
	if err != nil {
		return nil, err
	}
	ensureStorageDefaults(&storage)
	if len(storage.PendingNotifications) == 0 {
		return nil, nil
	}

	pending := make([]unlockLog, len(storage.PendingNotifications))
	copy(pending, storage.PendingNotifications)
	storage.PendingNotifications = nil
	storage.UpdatedAt = now.UTC().Format(time.RFC3339)
	if err := writeStorageFileUnlocked(path, storage); err != nil {
		return nil, err
	}
	return pending, nil
}

func readStorageFile(path string) (storageFile, error) {
	storageMu.Lock()
	defer storageMu.Unlock()

	return readStorageFileUnlocked(path)
}

func readStorageFileUnlocked(path string) (storageFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return storageFile{}, nil
		}
		return storageFile{}, fmt.Errorf("read achievement storage: %w", err)
	}
	if len(content) == 0 {
		return storageFile{}, nil
	}

	var storage storageFile
	if err := json.Unmarshal(content, &storage); err != nil {
		return storageFile{}, fmt.Errorf("parse achievement storage: %w", err)
	}
	return storage, nil
}

func writeStorageFile(path string, storage storageFile) error {
	storageMu.Lock()
	defer storageMu.Unlock()

	return writeStorageFileUnlocked(path, storage)
}

func writeStorageFileUnlocked(path string, storage storageFile) error {
	ensureStorageDefaults(&storage)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create achievement storage dir: %w", err)
	}

	content, err := json.MarshalIndent(storage, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal achievement storage: %w", err)
	}
	content = append(content, '\n')
	if err := writeFileAtomic(path, content, 0644); err != nil {
		return fmt.Errorf("write achievement storage: %w", err)
	}
	return nil
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

	if n, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	} else if n != len(content) {
		_ = tmp.Close()
		return io.ErrShortWrite
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
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func ensureStorageDefaults(storage *storageFile) {
	if storage.SchemaVersion == 0 {
		storage.SchemaVersion = currentSchemaVersion
	}
	if storage.Achievements == nil {
		storage.Achievements = make(map[string]achievementLog)
	}
}

func hasEventDedupeKey(events []eventLog, dedupeKey string) bool {
	for _, e := range events {
		if e.DedupeKey == dedupeKey {
			return true
		}
	}
	return false
}

func applyRules(storage *storageFile, event string, increment int, utcTime string) []unlockLog {
	var unlocks []unlockLog
	for _, r := range rulesByEvent(event) {
		current := storage.Achievements[r.ID]
		if current.ID == "" {
			current = achievementLog{
				ID:     r.ID,
				Title:  r.Title,
				Event:  r.Event,
				Target: r.Target,
			}
		}
		current.Progress += increment

		if current.UnlockedAt == "" && current.Progress >= current.Target {
			current.UnlockedAt = utcTime
			unlocks = append(unlocks, unlockLog{
				ID:         current.ID,
				Title:      current.Title,
				UnlockedAt: current.UnlockedAt,
			})
		}
		storage.Achievements[r.ID] = current
	}
	return unlocks
}
