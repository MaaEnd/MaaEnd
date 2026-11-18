package essencefilter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Coordinates belong only to this verified scan. Duplicate icons and identical
// affixes at different positions are separate inventory items.
type warehouseScan struct {
	positions map[[2]int]int
	items     []inventoryItem
	current   gridSelection
	verified  bool
}

type gridSelection struct {
	Row      int    `json:"selected_row"`
	Col      int    `json:"selected_col"`
	Quality  string `json:"selected_quality"`
	Mark     string `json:"selected_thumb_state"`
	Filter   bool   `json:"selected_for_filter"`
	Issued   int    `json:"issued_cells"`
	Resolved int    `json:"resolved_cells"`
	Expected int    `json:"expected_cells"`
}

func newWarehouseScan() *warehouseScan {
	return &warehouseScan{positions: make(map[[2]int]int), items: []inventoryItem{}}
}

func (s *warehouseScan) observe(engine *matchapi.Engine, ocr matchapi.OCRInput) error {
	c := s.current
	if c.Row < 0 || c.Col < 0 {
		return fmt.Errorf("no confirmed warehouse cell for OCR")
	}
	item, err := observeItem(engine, ocr, c.Quality, c.Mark)
	if err != nil {
		return err
	}
	key := [2]int{c.Row, c.Col}
	if i, ok := s.positions[key]; ok {
		prev := s.items[i]
		if item.Skills != prev.Skills || item.Quality != prev.Quality {
			return fmt.Errorf("warehouse cell %v changed", key)
		}
		return nil
	}
	s.positions[key] = len(s.items)
	s.items = append(s.items, item)
	return nil
}

func (s *warehouseScan) verify(c gridSelection) error {
	if c.Expected < 0 || c.Expected != c.Resolved || c.Expected != c.Issued || c.Issued != len(s.items) {
		return fmt.Errorf("inventory coverage: expected %d, resolved %d, issued %d, OCR %d", c.Expected, c.Resolved, c.Issued, len(s.items))
	}
	s.verified = true
	return nil
}

// EssenceFilterReadLockStateAction consumes the same recognition detail as the
// click path, including the final C++ count and viewport verification.
type EssenceFilterReadLockStateAction struct{}

func (*EssenceFilterReadLockStateAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	detail := arg.RecognitionDetail
	if st == nil || st.TaskID != arg.TaskID || st.Scan == nil || detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return false
	}
	custom, ok := detail.Results.Best.AsCustom()
	if !ok || custom == nil {
		return false
	}
	c := gridSelection{Row: -1, Col: -1, Issued: -1, Resolved: -1, Expected: -1}
	if err := json.Unmarshal([]byte(custom.Detail), &c); err != nil {
		return inventoryFailed(err)
	}
	if arg.CurrentTaskName == "EssenceGridVerifyComplete" {
		if err := st.Scan.verify(c); err != nil {
			return inventoryFailed(err)
		}
	}
	st.Scan.current = c
	st.CurrentLocked = c.Mark == lockedMark
	return true
}

// EssenceFilterConfirmMarkAction runs only on a successful lock/discard recognition.
type EssenceFilterConfirmMarkAction struct{}

func (*EssenceFilterConfirmMarkAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		return false
	}
	var mark string
	switch arg.CurrentTaskName {
	case "EssenceFilterCheckLocked", "EssenceFilterAfterBattleCheckLocked":
		mark = lockedMark
	case "EssenceFilterCheckDiscarded", "EssenceFilterAfterBattleCheckDiscarded":
		mark = discarded
	default:
		return false
	}
	if st.Scan != nil {
		c := st.Scan.current
		i, ok := st.Scan.positions[[2]int{c.Row, c.Col}]
		if !ok {
			return false
		}
		st.Scan.items[i].Mark = mark
		return true
	}
	if st.Loot == nil {
		return false
	}
	item, ok := st.Loot.items[st.RowIndex]
	if !ok {
		return false
	}
	item.Mark = mark
	st.Loot.items[st.RowIndex] = item
	return true
}

func finishWarehouse(ctx *maa.Context, st *RunState) bool {
	if !st.Account.stillCurrent() {
		return inventoryFailed(fmt.Errorf("account changed during warehouse scan"))
	}
	if !st.Scan.verified {
		return inventoryFailed(fmt.Errorf("warehouse end not verified"))
	}
	now := time.Now().UTC()
	s := newSnapshot(st.Account.id)
	s.DataVersion = st.MatchEngine.DataVersion()
	s.ScannedAt = map[string]time.Time{gold: now, purple: now}
	s.Items = st.Scan.items
	if err := st.Account.commit(s, st.PipelineOpts.Mode == InitModeInventory); err != nil {
		return inventoryFailed(err)
	}
	groups := s.groups()
	matched, _ := inventoryTotals(groups)
	log.Info().Str("component", "EssenceInventory").Int("observed", len(s.Items)).Int("matched", matched).Msg("warehouse snapshot committed")
	if st.PipelineOpts.Mode == InitModeInventory {
		reportSimpleByKey(ctx, "inventory.saved", filepath.Join(st.Account.dir, inventoryFullExportPath), len(s.Items), InventoryPath(), matched, len(groups))
	}
	return true
}
