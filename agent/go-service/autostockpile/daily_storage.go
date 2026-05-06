package autostockpile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	dailyStorageFileName     = "daily_storage.json"
	maxDailyStorageDateCount = 60
	maaEndDataDirEnvVar      = "MAAEND_DATA_DIR"
)

var resolveDailyStoragePathFunc = resolveDailyStoragePath

type dailyStorageFile struct {
	SchemaVersion int                  `json:"schema_version"`
	Records       []dailyStorageRecord `json:"records"`
}

type dailyStorageRecord struct {
	ServerDate string      `json:"server_date"`
	Weekday    int         `json:"weekday"`
	UTCTime    string      `json:"utc_time"`
	Region     string      `json:"region"`
	Goods      []GoodsItem `json:"goods"`
}

func serverDateInfo(now time.Time, loc *time.Location) (string, int) {
	serverTime := adjustedServerTime(now, loc)
	return serverTime.Format(time.DateOnly), maaWeekday(serverTime.Weekday())
}

func maaWeekday(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}

func storeDailyGoodsPrices(enabled bool, now time.Time, loc *time.Location, region string, data RecognitionData) error {
	if !enabled {
		return nil
	}

	serverDate, weekday := serverDateInfo(now, loc)
	record := dailyStorageRecord{
		ServerDate: serverDate,
		Weekday:    weekday,
		UTCTime:    now.UTC().Format(time.RFC3339),
		Region:     region,
		Goods:      cloneGoodsItems(data.Goods),
	}

	path := resolveDailyStoragePathFunc()
	return upsertDailyStorageRecord(path, record)
}

func resolveDailyStoragePath() string {
	return filepath.Join(resolveDailyStorageDataDir(), "AutoStockpile", dailyStorageFileName)
}

func resolveDailyStorageDataDir() string {
	if dir := strings.TrimSpace(os.Getenv(maaEndDataDirEnvVar)); dir != "" {
		return filepath.Clean(dir)
	}

	if cwd, err := os.Getwd(); err == nil {
		if dir := findExistingDataDirFromAncestors(cwd); dir != "" {
			return dir
		}
	}

	if exePath, err := os.Executable(); err == nil {
		if dir := findExistingDataDirFromAncestors(filepath.Dir(exePath)); dir != "" {
			return dir
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, "data")
	}
	return "data"
}

func findExistingDataDirFromAncestors(start string) string {
	base := filepath.Clean(start)
	for {
		for _, candidate := range []string{
			filepath.Join(base, "data"),
			filepath.Join(base, "assets", "data"),
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}

		parent := filepath.Dir(base)
		if parent == base {
			return ""
		}
		base = parent
	}
}

func upsertDailyStorageRecord(path string, record dailyStorageRecord) error {
	storage, err := readDailyStorageFile(path)
	if err != nil {
		return err
	}

	replaced := false
	for i := range storage.Records {
		if storage.Records[i].ServerDate == record.ServerDate && storage.Records[i].Region == record.Region {
			storage.Records[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		storage.Records = append(storage.Records, record)
	}

	storage.Records = retainRecentDailyStorageDates(storage.Records, maxDailyStorageDateCount)
	storage.SchemaVersion = 1
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create daily storage dir: %w", err)
	}

	content, err := json.MarshalIndent(storage, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal daily storage: %w", err)
	}
	content = append(content, '\n')
	if err := writeFileAtomic(path, content, 0644); err != nil {
		return fmt.Errorf("write daily storage: %w", err)
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

func readDailyStorageFile(path string) (dailyStorageFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return dailyStorageFile{}, nil
		}
		return dailyStorageFile{}, fmt.Errorf("read daily storage: %w", err)
	}
	if len(content) == 0 {
		return dailyStorageFile{}, nil
	}

	var storage dailyStorageFile
	if err := json.Unmarshal(content, &storage); err != nil {
		return dailyStorageFile{}, fmt.Errorf("parse daily storage: %w", err)
	}
	return storage, nil
}

func retainRecentDailyStorageDates(records []dailyStorageRecord, maxDateCount int) []dailyStorageRecord {
	if maxDateCount <= 0 || len(records) == 0 {
		return nil
	}

	dates := make(map[string]struct{})
	for _, record := range records {
		dates[record.ServerDate] = struct{}{}
	}
	if len(dates) <= maxDateCount {
		return records
	}

	sortedDates := make([]string, 0, len(dates))
	for date := range dates {
		sortedDates = append(sortedDates, date)
	}
	sort.Strings(sortedDates)
	retained := make(map[string]struct{}, maxDateCount)
	for _, date := range sortedDates[len(sortedDates)-maxDateCount:] {
		retained[date] = struct{}{}
	}

	filtered := records[:0]
	for _, record := range records {
		if _, ok := retained[record.ServerDate]; ok {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func cloneGoodsItems(goods []GoodsItem) []GoodsItem {
	if len(goods) == 0 {
		return nil
	}
	cloned := make([]GoodsItem, len(goods))
	copy(cloned, goods)
	return cloned
}
