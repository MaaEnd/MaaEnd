package essencefilter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/gofrs/flock"
	"github.com/rs/zerolog/log"
)

const (
	inventoryVersion = 1
	gold             = "flawless_gold"
	purple           = "high_purity_purple"
	unmarked         = "none"
	lockedMark       = "lock"
	discarded        = "discard"
)

type inventoryItem struct {
	Quality   string                     `json:"quality"`
	Mark      string                     `json:"mark"`
	Skills    [3]matchapi.InventorySkill `json:"skills"`
	WeaponIDs []string                   `json:"weapon_ids"`
}

func observeItem(engine *matchapi.Engine, ocr matchapi.OCRInput, quality, mark string) (inventoryItem, error) {
	if !validQualityAndMark(quality, mark) {
		return inventoryItem{}, fmt.Errorf("invalid observed quality %q or mark %q", quality, mark)
	}
	skills, err := engine.InventorySkills(ocr)
	if err != nil {
		return inventoryItem{}, err
	}
	item := inventoryItem{Quality: quality, Mark: mark, Skills: skills, WeaponIDs: []string{}}
	if match := engine.InventoryMatch(skills); match != nil {
		for _, weapon := range match.Weapons {
			item.WeaponIDs = append(item.WeaponIDs, weapon.InternalID)
		}
		slices.Sort(item.WeaponIDs)
	}
	return item, nil
}

func validQualityAndMark(quality, mark string) bool {
	return (quality == gold || quality == purple) && (mark == unmarked || mark == lockedMark || mark == discarded)
}

type inventorySnapshot struct {
	Version     int       `json:"version"`
	Account     string    `json:"account_id,omitempty"`
	DataVersion string    `json:"data_version"`
	UpdatedAt   time.Time `json:"updated_at"`
	// The time of the last exhaustive warehouse observation for each still-covered tier.
	ScannedAt map[string]time.Time `json:"scanned_at"`
	Pending   bool                 `json:"pending"`
	Items     []inventoryItem      `json:"items"`
}

func newSnapshot(id string) *inventorySnapshot {
	return &inventorySnapshot{Version: inventoryVersion, Account: id, ScannedAt: map[string]time.Time{}, Items: []inventoryItem{}}
}

func (s *inventorySnapshot) targetReady(version string) bool {
	return !s.Pending && version != "" && s.DataVersion == version && !s.ScannedAt[gold].IsZero()
}

func (s *inventorySnapshot) groups() []inventoryGroup {
	merged := make(map[string]*inventoryGroup)
	for _, item := range s.Items {
		if item.Quality != gold || item.Mark == discarded || len(item.WeaponIDs) == 0 {
			continue
		}
		row := inventoryEssence{Count: 1}
		for _, skill := range item.Skills {
			row.Levels[skill.Pool-1] = skill.Level
		}
		if item.Mark == lockedMark {
			row.Locked = 1
		}
		mergeInventoryRows(merged, item.WeaponIDs, []inventoryEssence{row})
	}
	groups := make([]inventoryGroup, 0, len(merged))
	for _, group := range merged {
		groups = append(groups, *group)
	}
	sortInventoryGroups(groups)
	return groups
}

var errInvalidSnapshot = errors.New("invalid essence inventory snapshot")

// A task with no verified account can reuse only its own in-memory snapshot.
type inventoryAccount struct {
	id    string
	dir   string
	local *inventorySnapshot
	base  *inventorySnapshot
	lock  *flock.Flock
}

var inventoryTasks = struct {
	sync.Mutex
	byID map[int64]*inventoryAccount
}{byID: make(map[int64]*inventoryAccount)}

func accountFor(ctx *maa.Context, taskID int64) *inventoryAccount {
	cached := captureuid.GetCachedUID(captureuid.OutputTypeHashed)
	return bindInventoryAccount(taskID, filepath.Dir(InventoryPath()), cached, func() string {
		ctrl := ctx.GetTasker().GetController()
		sampled, err := captureuid.Capture(ctx, ctrl, true, true, true, captureuid.OutputTypeHashed)
		if err != nil || sampled == "unknown" {
			return ""
		}
		return sampled
	})
}

func bindInventoryAccount(taskID int64, dir, cached string, sample func() string) *inventoryAccount {
	inventoryTasks.Lock()
	defer inventoryTasks.Unlock()
	if previous := inventoryTasks.byID[taskID]; previous != nil {
		if previous.id == cached {
			return previous
		}
		previous.release()
	}
	if cached == "" {
		cached = sample()
	}
	account := &inventoryAccount{id: cached, dir: dir}
	inventoryTasks.byID[taskID] = account
	return account
}

func releaseInventoryTask(taskID int64) {
	inventoryTasks.Lock()
	defer inventoryTasks.Unlock()
	if account := inventoryTasks.byID[taskID]; account != nil {
		account.release()
		delete(inventoryTasks.byID, taskID)
	}
}

func (a *inventoryAccount) stillCurrent() bool {
	return a.id == captureuid.GetCachedUID(captureuid.OutputTypeHashed)
}

func (a *inventoryAccount) path() string {
	return filepath.Join(a.dir, "EssenceInventory", a.id+".json")
}

func (a *inventoryAccount) load() (*inventorySnapshot, error) {
	if a.id == "" {
		if a.local == nil {
			return newSnapshot(""), nil
		}
		return a.local, nil
	}
	data, err := os.ReadFile(a.path())
	if errors.Is(err, os.ErrNotExist) {
		return newSnapshot(a.id), nil
	}
	if err != nil {
		return nil, err
	}
	var s inventorySnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidSnapshot, err)
	}
	if s.Account != a.id || s.Version != inventoryVersion || s.ScannedAt == nil || s.Items == nil {
		return nil, errInvalidSnapshot
	}
	for _, item := range s.Items {
		if !validQualityAndMark(item.Quality, item.Mark) || item.WeaponIDs == nil {
			return nil, errInvalidSnapshot
		}
		for _, skill := range item.Skills {
			if skill.ID < 1 || skill.Pool < 1 || skill.Pool > 3 || skill.Level < 1 || skill.Level > 6 || skill.Pool == 3 && skill.Level > 3 {
				return nil, errInvalidSnapshot
			}
		}
	}
	return &s, nil
}

func writeInventoryJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "    ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, append(data, '\n'), 0644)
}

func (a *inventoryAccount) save(s *inventorySnapshot) error {
	if a.id == "" {
		a.local = s
		return nil
	}
	return writeInventoryJSON(a.path(), s)
}

// Mark the account before a scan or claim can change the warehouse. An OS lock
// covers game interactions and is released by the OS if the agent exits.
func (a *inventoryAccount) begin() error {
	if a.base != nil {
		return fmt.Errorf("inventory update already in progress")
	}
	if a.id != "" {
		if err := os.MkdirAll(filepath.Dir(a.path()), 0755); err != nil {
			return err
		}
		lock := flock.New(a.path() + ".lock")
		ok, err := lock.TryLock()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("account inventory is being updated by another task")
		}
		a.lock = lock
	}
	s, err := a.load()
	if errors.Is(err, errInvalidSnapshot) {
		log.Warn().Err(err).Str("component", "EssenceInventory").Msg("discarding unusable inventory baseline")
		s, err = newSnapshot(a.id), nil
	}
	if err != nil {
		a.release()
		return err
	}
	if s.Pending {
		clear(s.ScannedAt)
	}
	s.Pending = true
	if err := a.save(s); err != nil {
		a.release()
		return err
	}
	a.base = s
	return nil
}

func (a *inventoryAccount) release() {
	a.base = nil
	if a.lock != nil {
		if err := a.lock.Unlock(); err != nil {
			log.Error().Err(err).Msg("release essence inventory lock")
		}
		a.lock = nil
	}
}

// Keep the persisted pending marker until both public projections have been
// replaced. A publication failure therefore cannot authorize cache reuse.
func (a *inventoryAccount) commit(s *inventorySnapshot, explicitExport bool) error {
	if a.base == nil {
		return fmt.Errorf("inventory transaction missing")
	}
	defer a.release()
	s.Pending = false
	s.UpdatedAt = time.Now().UTC()
	if s.targetReady(s.DataVersion) && (a.id != "" || explicitExport) {
		if err := os.MkdirAll(a.dir, 0755); err != nil {
			return err
		}
		lock := flock.New(filepath.Join(a.dir, ".EssenceInventory-export.lock"))
		if err := lock.Lock(); err != nil {
			return err
		}
		defer lock.Unlock()
		if err := writeInventoryJSON(filepath.Join(a.dir, inventoryFullExportPath), s); err != nil {
			return err
		}
		if err := writeInventoryFile(filepath.Join(a.dir, inventoryExportPath), s.groups()); err != nil {
			return err
		}
	}
	return a.save(s)
}

// LoadAccountInventory reads covered gold stock for the active account. The public
// exports are intentionally portable and cannot establish an account identity.
func LoadAccountInventory(ctx *maa.Context, taskID int64) (*InventoryStock, error) {
	version, err := matchapi.InventoryDataVersion(essenceFilterDataDir)
	if err != nil {
		return nil, err
	}
	s, err := accountFor(ctx, taskID).load()
	if err != nil {
		return nil, err
	}
	if !s.targetReady(version) {
		return nil, fmt.Errorf("gold inventory needs a verified scan")
	}
	return stockFromGroups(s.groups()), nil
}
