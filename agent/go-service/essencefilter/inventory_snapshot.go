package essencefilter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/rs/zerolog/log"
)

const (
	inventorySnapshotFileName      = "EssenceFilterInventory.json"
	inventorySnapshotSchemaVersion = 1
)

var (
	resolveInventorySnapshotPathFunc = defaultInventorySnapshotPath
	inventorySnapshotNowFunc         = func() time.Time { return time.Now() }
)

type inventorySnapshotFile struct {
	SchemaVersion int                    `json:"schema_version"`
	UpdatedAt     string                 `json:"updated_at"`
	UID           string                 `json:"uid,omitempty"`
	Complete      bool                   `json:"complete"`
	Records       []inventorySnapshotRow `json:"records"`
}

type inventorySnapshotRow struct {
	SkillIDs      []int                     `json:"skill_ids"`
	SkillsChinese []string                  `json:"skills_chinese"`
	OCRSkills     []string                  `json:"ocr_skills"`
	Count         int                       `json:"count"`
	Weapons       []inventorySnapshotWeapon `json:"weapons"`
}

type inventorySnapshotWeapon struct {
	ID     string `json:"id"`
	Rarity int    `json:"rarity"`
}

func defaultInventorySnapshotPath() string {
	return filepath.Join("debug", "record", inventorySnapshotFileName)
}

func resetInventorySnapshot() {
	if err := writeInventorySnapshotFile(false, nil); err != nil {
		log.Warn().
			Err(err).
			Str("component", "EssenceFilter").
			Str("step", "InventorySnapshot").
			Msg("clear inventory snapshot failed")
	}
}

func saveInventorySnapshot(st *RunState) {
	if st == nil {
		return
	}
	if err := writeInventorySnapshotFile(true, snapshotRowsFromSummary(st.MatchedCombinationSummary)); err != nil {
		log.Warn().
			Err(err).
			Str("component", "EssenceFilter").
			Str("step", "InventorySnapshot").
			Msg("save inventory snapshot failed")
	}
}

func snapshotRowsFromSummary(summary map[string]*matchapi.SkillCombinationSummary) []inventorySnapshotRow {
	if len(summary) == 0 {
		return []inventorySnapshotRow{}
	}
	keys := make([]string, 0, len(summary))
	for k := range summary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]inventorySnapshotRow, 0, len(keys))
	for _, k := range keys {
		item := summary[k]
		if item == nil {
			continue
		}
		weapons := make([]inventorySnapshotWeapon, 0, len(item.Weapons))
		for _, w := range item.Weapons {
			if w.InternalID == "" {
				continue
			}
			weapons = append(weapons, inventorySnapshotWeapon{ID: w.InternalID, Rarity: w.Rarity})
		}
		ocr := append([]string(nil), item.OCRSkills...)
		if len(ocr) == 0 {
			ocr = append([]string(nil), item.SkillsChinese...)
		}
		rows = append(rows, inventorySnapshotRow{
			SkillIDs:      append([]int(nil), item.SkillIDs...),
			SkillsChinese: append([]string(nil), item.SkillsChinese...),
			OCRSkills:     ocr,
			Count:         item.Count,
			Weapons:       weapons,
		})
	}
	return rows
}

func writeInventorySnapshotFile(complete bool, records []inventorySnapshotRow) error {
	if records == nil {
		records = []inventorySnapshotRow{}
	}
	path := resolveInventorySnapshotPathFunc()
	storage := inventorySnapshotFile{
		SchemaVersion: inventorySnapshotSchemaVersion,
		UpdatedAt:     inventorySnapshotNowFunc().UTC().Format(time.RFC3339),
		UID:           captureuid.GetCachedUID(captureuid.OutputTypeHashed),
		Complete:      complete,
		Records:       records,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create inventory snapshot dir: %w", err)
	}
	raw, err := json.MarshalIndent(storage, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal inventory snapshot: %w", err)
	}
	raw = append(raw, '\n')
	return writeInventorySnapshotAtomic(path, raw, 0644)
}

func writeInventorySnapshotAtomic(path string, content []byte, perm os.FileMode) error {
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
