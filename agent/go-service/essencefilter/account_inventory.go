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
	inventoryFormat  = "maaend.essence_inventory"
	inventoryVersion = 1
	gold             = "flawless_gold"
	lockedGold       = "locked_matching_flawless_gold"
	purple           = "high_purity_purple"
	unmarked         = "none"
	lockedMark       = "lock"
	discarded        = "discard"
)

type inventoryTerm struct {
	ID    string `json:"id"`
	Level int    `json:"level"`
}

type inventoryItem struct {
	Quality string          `json:"quality"`
	Mark    string          `json:"mark"`
	Skills  []inventoryTerm `json:"skills"`

	resolved  [3]matchapi.InventorySkill
	weaponIDs []string
}

// JSON field names in the protocol are case sensitive. Look up required fields
// explicitly so a future field such as "ID" cannot replace a known "id".
func readInventoryFields(data []byte, fields map[string]any) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for name, dest := range fields {
		raw := object[name]
		if len(raw) == 0 || string(raw) == "null" {
			return fmt.Errorf("missing inventory field %q", name)
		}
		if err := json.Unmarshal(raw, dest); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (s *inventoryTerm) UnmarshalJSON(data []byte) error {
	return readInventoryFields(data, map[string]any{"id": &s.ID, "level": &s.Level})
}

func (item *inventoryItem) UnmarshalJSON(data []byte) error {
	return readInventoryFields(data, map[string]any{
		"quality": &item.Quality, "mark": &item.Mark, "skills": &item.Skills,
	})
}

func observeItem(engine *matchapi.Engine, ocr matchapi.OCRInput, quality, mark string) (inventoryItem, error) {
	if !validQualityAndMark(quality, mark) {
		return inventoryItem{}, fmt.Errorf("invalid observed quality %q or mark %q", quality, mark)
	}
	skills, err := engine.InventorySkills(ocr)
	if err != nil {
		return inventoryItem{}, err
	}
	item := inventoryItem{Quality: quality, Mark: mark, resolved: skills}
	for _, skill := range skills {
		term, ok := engine.TermID(skill.Pool, skill.ID)
		if !ok {
			return inventoryItem{}, fmt.Errorf("missing term ID for skill %d in pool %d", skill.ID, skill.Pool)
		}
		item.Skills = append(item.Skills, inventoryTerm{ID: term, Level: skill.Level})
	}
	item.matchWeapons(engine)
	return item, nil
}

func (item *inventoryItem) matchWeapons(engine *matchapi.Engine) {
	if match := engine.InventoryMatch(item.resolved); match != nil {
		for _, weapon := range match.Weapons {
			if weapon.Rarity == 5 || weapon.Rarity == 6 {
				item.weaponIDs = append(item.weaponIDs, weapon.InternalID)
			}
		}
		slices.Sort(item.weaponIDs)
	}
}

func (item *inventoryItem) resolve(engine *matchapi.Engine) error {
	if !validQualityAndMark(item.Quality, item.Mark) || len(item.Skills) != len(item.resolved) {
		return errInvalidSnapshot
	}
	for i, skill := range item.Skills {
		pool, id, ok := engine.ResolveTermID(skill.ID)
		if !ok || skill.Level < 1 || skill.Level > 6 || pool == 3 && skill.Level > 3 {
			return errInvalidSnapshot
		}
		item.resolved[i] = matchapi.InventorySkill{Pool: pool, ID: id, Level: skill.Level}
	}
	item.matchWeapons(engine)
	return nil
}

func validQualityAndMark(quality, mark string) bool {
	return (quality == gold || quality == purple) && (mark == unmarked || mark == lockedMark || mark == discarded)
}

type tierCoverage struct {
	CompleteAt *time.Time `json:"complete_at,omitempty"`
	Unread     int        `json:"unread,omitempty"`
}

// Distinguish absent optional fields from explicit null or zero in the wire format.
func (c *tierCoverage) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return errInvalidSnapshot
	}
	var coverage tierCoverage
	if raw, ok := fields["complete_at"]; ok {
		if err := json.Unmarshal(raw, &coverage.CompleteAt); err != nil || coverage.CompleteAt == nil || !validInventoryTime(*coverage.CompleteAt) {
			return errInvalidSnapshot
		}
	}
	if raw, ok := fields["unread"]; ok {
		if err := json.Unmarshal(raw, &coverage.Unread); err != nil || coverage.Unread < 1 || coverage.CompleteAt != nil {
			return errInvalidSnapshot
		}
	}
	*c = coverage
	return nil
}

func validInventoryTime(t time.Time) bool {
	_, offset := t.Zone()
	return offset == 0
}

type inventorySnapshot struct {
	Format    string                  `json:"format"`
	Version   int                     `json:"version"`
	Account   string                  `json:"account_id,omitempty"`
	UpdatedAt time.Time               `json:"updated_at"`
	Coverage  map[string]tierCoverage `json:"coverage"`
	Items     []inventoryItem         `json:"items"`
}

func newSnapshot(id string) *inventorySnapshot {
	return &inventorySnapshot{
		Format:    inventoryFormat,
		Version:   inventoryVersion,
		Account:   id,
		UpdatedAt: time.Now().UTC(),
		Coverage:  make(map[string]tierCoverage),
		Items:     []inventoryItem{},
	}
}

func (s *inventorySnapshot) targetReady() bool {
	return s.Coverage[lockedGold].CompleteAt != nil || s.Coverage[gold].CompleteAt != nil
}

func (s *inventorySnapshot) appendLockedMatch(item inventoryItem) {
	if item.Quality == gold && item.Mark == lockedMark && len(item.weaponIDs) > 0 {
		s.Items = append(s.Items, item)
	}
}

// A confirmed purple position without observed skills contributes only to coverage.
func (s *inventorySnapshot) appendObservation(item inventoryItem) {
	if item.Quality == purple && len(item.Skills) == 0 {
		s.Coverage[purple] = tierCoverage{Unread: s.Coverage[purple].Unread + 1}
		return
	}
	s.Items = append(s.Items, item)
}

func (item inventoryItem) groupRow() inventoryEssence {
	row := inventoryEssence{Count: 1}
	for _, skill := range item.resolved {
		row.Levels[skill.Pool-1] = skill.Level
	}
	if item.Mark == lockedMark {
		row.Locked = 1
	}
	return row
}

// Only confirmed locks for five- and six-star weapons are available for farming.
func (s *inventorySnapshot) groups(weapons []matchapi.WeaponData) []inventoryGroup {
	eligible := make(map[string]bool, len(weapons))
	for _, w := range weapons {
		eligible[w.InternalID] = w.Rarity == 5 || w.Rarity == 6
	}
	merged := make(map[string]*inventoryGroup)
	for _, item := range s.Items {
		if item.Quality != gold || item.Mark != lockedMark || len(item.weaponIDs) == 0 {
			continue
		}
		var ids []string
		for _, id := range item.weaponIDs {
			if eligible[id] {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			mergeInventoryRows(merged, ids, []inventoryEssence{item.groupRow()})
		}
	}
	groups := make([]inventoryGroup, 0, len(merged))
	for _, group := range merged {
		groups = append(groups, *group)
	}
	sortInventoryGroups(groups)
	return groups
}

var errInvalidSnapshot = errors.New("invalid essence inventory snapshot")
var errInventoryAccountChanged = errors.New("cannot confirm the original essence inventory account")

// A task with no verified account can reuse only its own in-memory snapshot.
type inventoryAccount struct {
	id         string
	generation uint64
	dir        string
	local      *inventorySnapshot
	base       *inventorySnapshot
	lock       *flock.Flock
}

var inventoryTasks = struct {
	sync.Mutex
	byID map[int64]*inventoryAccount
}{byID: make(map[int64]*inventoryAccount)}

func taskController(ctx *maa.Context) *maa.Controller {
	if ctx == nil {
		return nil
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil
	}
	return tasker.GetController()
}

func currentInventoryUID(ctx *maa.Context, ctrl *maa.Controller) string {
	if ctrl == nil {
		return ""
	}
	uid, err := captureuid.Capture(ctx, ctrl, false, true, true, captureuid.OutputTypeHashed)
	if err != nil || uid == "unknown" {
		return ""
	}
	return uid
}

func accountFor(ctx *maa.Context, ctrl *maa.Controller, taskID int64) (*inventoryAccount, error) {
	return bindInventoryAccount(taskID, fsutil.UserDataPath("debug", "record"), func() string { return currentInventoryUID(ctx, ctrl) })
}

// Bind once per task. A switch requires a new task, even when the user switches back.
func bindInventoryAccount(taskID int64, dir string, sample func() string) (*inventoryAccount, error) {
	inventoryTasks.Lock()
	defer inventoryTasks.Unlock()
	if previous := inventoryTasks.byID[taskID]; previous != nil {
		if previous.generation != captureuid.CacheGeneration() {
			return nil, errInventoryAccountChanged
		}
		return previous, nil
	}
	uid := sample()
	account := &inventoryAccount{id: uid, generation: captureuid.CacheGeneration(), dir: dir}
	inventoryTasks.byID[taskID] = account
	return account, nil
}

func releaseInventoryTask(taskID int64) {
	inventoryTasks.Lock()
	defer inventoryTasks.Unlock()
	if account := inventoryTasks.byID[taskID]; account != nil {
		account.release()
		delete(inventoryTasks.byID, taskID)
	}
}

// Anonymous tasks never publish under a UID discovered later in the run.
func (a *inventoryAccount) observedUID(ctx *maa.Context, ctrl *maa.Controller) string {
	if a.id == "" {
		return ""
	}
	uid, err := captureuid.Observe(ctx, ctrl, captureuid.OutputTypeHashed)
	if err != nil {
		return ""
	}
	return uid
}

func (a *inventoryAccount) matchesUID(uid string) bool {
	return a.id == uid && a.generation == captureuid.CacheGeneration()
}

func (a *inventoryAccount) path() string {
	name := a.id
	if name == "" {
		name = "unverified"
	}
	return filepath.Join(a.dir, "EssenceInventory", name+".json")
}

func (a *inventoryAccount) load(engine *matchapi.Engine) (*inventorySnapshot, error) {
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
	// Only verified account files are read, so account_id is required here.
	if err := readInventoryFields(data, map[string]any{
		"format": &s.Format, "version": &s.Version, "account_id": &s.Account,
		"updated_at": &s.UpdatedAt, "coverage": &s.Coverage, "items": &s.Items,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidSnapshot, err)
	}
	if s.Format != inventoryFormat || s.Version != inventoryVersion || s.Account != a.id ||
		!validInventoryTime(s.UpdatedAt) {
		return nil, errInvalidSnapshot
	}
	for quality, coverage := range s.Coverage {
		if quality != gold && quality != purple && quality != lockedGold {
			return nil, errInvalidSnapshot
		}
		if coverage == (tierCoverage{}) {
			delete(s.Coverage, quality)
		}
	}
	for i := range s.Items {
		if err := s.Items[i].resolve(engine); err != nil {
			return nil, err
		}
	}
	return &s, nil
}

func writeInventoryJSON(path string, s *inventorySnapshot) error {
	data, err := json.MarshalIndent(s, "", "    ")
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

// Keep the last published snapshot intact while rewards are being read.
func (a *inventoryAccount) openBaseline(engine *matchapi.Engine) error {
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
	s, err := a.load(engine)
	if errors.Is(err, errInvalidSnapshot) {
		log.Warn().Err(err).Str("component", "EssenceInventory").Msg("discarding unusable inventory baseline")
		s, err = newSnapshot(a.id), nil
	}
	if err != nil {
		a.release()
		return err
	}
	a.base = s
	return nil
}

// A warehouse refresh rebuilds coverage from the current grid.
func (a *inventoryAccount) begin(engine *matchapi.Engine) error {
	if err := a.openBaseline(engine); err != nil {
		return err
	}
	s := a.base
	incomplete := *s
	incomplete.Coverage = make(map[string]tierCoverage)
	for quality, coverage := range s.Coverage {
		if coverage.Unread > 0 {
			incomplete.Coverage[quality] = tierCoverage{Unread: coverage.Unread}
		}
	}
	if err := a.save(&incomplete); err != nil {
		a.release()
		return err
	}
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

// commit rechecks the account before publishing the confirmed warehouse state.
func (a *inventoryAccount) commit(s *inventorySnapshot, explicitReport bool, observedUID string) error {
	if a.base == nil {
		return fmt.Errorf("inventory transaction missing")
	}
	defer a.release()
	if s.Account != a.id || !a.matchesUID(observedUID) {
		return errInventoryAccountChanged
	}
	s.UpdatedAt = time.Now().UTC()
	if a.id == "" && explicitReport {
		if err := writeInventoryJSON(a.path(), s); err != nil {
			return err
		}
	}
	if err := a.save(s); err != nil {
		return err
	}
	if a.id != "" {
		for _, name := range []string{"EssenceInventory.json", "EssenceInventoryFull.json"} {
			path := filepath.Join(a.dir, name)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Warn().Err(err).Str("path", path).Msg("could not remove legacy essence inventory")
			}
		}
	}
	return nil
}

// LoadAccountInventory reads verified gold stock using the current weapon data.
func LoadAccountInventory(ctx *maa.Context, taskID int64) (*InventoryStock, error) {
	engine, err := matchapi.NewEngineFromDir(essenceFilterDataDir)
	if err != nil {
		return nil, err
	}
	account, err := accountFor(ctx, taskController(ctx), taskID)
	if err != nil {
		return nil, err
	}
	s, err := account.load(engine)
	if err != nil {
		return nil, err
	}
	if !s.targetReady() {
		return nil, fmt.Errorf("gold inventory needs a verified scan")
	}
	return stockFromGroups(s.groups(engine.Weapons())), nil
}
