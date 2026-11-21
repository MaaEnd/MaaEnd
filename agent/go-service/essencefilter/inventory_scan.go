package essencefilter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Coordinates belong only to this verified scan. Duplicate icons and identical
// affixes at different positions are separate inventory items.
type warehouseScan struct {
	positions    map[[2]int]int
	items        []inventoryItem
	current      gridSelection
	skippedReads int
	verified     bool
}

type gridSelection struct {
	StartRow          int    `json:"current_viewport_start_row"`
	Row               int    `json:"selected_row"`
	Col               int    `json:"selected_col"`
	Quality           string `json:"selected_quality"`
	Mark              string `json:"selected_thumb_state"`
	Issued            int    `json:"issued_cells"`
	Resolved          int    `json:"resolved_cells"`
	Expected          int    `json:"expected_cells"`
	BoundaryCandidate bool   `json:"boundary_candidate"`
	ReachedEnd        bool   `json:"reached_end"`
}

// OCR resolves names to IDs but retains their displayed order and levels.
type warehouseDetail struct {
	Quality string
	Skills  [3]matchapi.InventorySkill
}

type warehouseDetailRead struct {
	Clicks      int    `json:"clicks"`
	CoreChanged bool   `json:"core_changed"`
	Quality     string `json:"panel_quality"`
	previous    *warehouseDetail
	startRow    int
}

// Only comparable reads can trigger a second click. A known change in the
// panel's tier distinguishes two otherwise equal reads.
func (r *warehouseDetailRead) needsSecondRead(current *warehouseDetail) bool {
	prior := r.previous
	if r.Clicks != 1 || r.CoreChanged || prior == nil || current == nil || prior.Skills != current.Skills {
		return false
	}
	return prior.Quality == current.Quality || prior.Quality == "unknown" || current.Quality == "unknown"
}

func newWarehouseScan() *warehouseScan {
	return &warehouseScan{positions: make(map[[2]int]int), items: []inventoryItem{}}
}

func (s *warehouseScan) observe(engine *matchapi.Engine, ocr matchapi.OCRInput) (inventoryItem, error) {
	c := s.current
	if c.Row < 0 || c.Col < 0 {
		return inventoryItem{}, fmt.Errorf("no confirmed warehouse cell for OCR")
	}
	item, err := observeItem(engine, ocr, c.Quality, c.Mark)
	if err != nil {
		return inventoryItem{}, err
	}
	key := [2]int{c.Row, c.Col}
	if i, ok := s.positions[key]; ok {
		prev := s.items[i]
		if item.resolved != prev.resolved || item.Quality != prev.Quality {
			return inventoryItem{}, fmt.Errorf("warehouse cell %v changed", key)
		}
		return prev, nil
	}
	s.positions[key] = len(s.items)
	s.items = append(s.items, item)
	return item, nil
}

// EssenceFilterUnreadPurpleAction handles unread affixes during warehouse screening.
type EssenceFilterUnreadPurpleAction struct{}

func (*EssenceFilterUnreadPurpleAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		return false
	}
	st.warehouseDetail.previous = nil
	if st.PipelineOpts.Mode != InitModeFilter {
		return false
	}
	log.Warn().Msg("Skipping essence with unread affixes; marks unchanged")
	return true
}

func (s *warehouseScan) verify(c gridSelection) error {
	if c.Expected < 0 || c.Expected != c.Resolved || c.Issued != len(s.items)+s.skippedReads {
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
	if st == nil || st.TaskID != arg.TaskID || detail == nil || detail.Results == nil || detail.Results.Best == nil {
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
	if c.Row >= 0 && c.StartRow != st.warehouseDetail.startRow {
		st.warehouseDetail.previous = nil
		st.warehouseDetail.startRow = c.StartRow
	}
	if st.Scan != nil {
		switch arg.CurrentTaskName {
		case "EssenceGridVerifyLockedPrefix":
			if !c.ReachedEnd || c.Issued != len(st.Scan.items)+st.Scan.skippedReads {
				return inventoryFailed(fmt.Errorf("unverified locked inventory prefix"))
			}
			st.Scan.verified = true
		case "EssenceGridVerifyComplete":
			if err := st.Scan.verify(c); err != nil {
				return inventoryFailed(err)
			}
		}
		st.Scan.current = c
	} else if st.PipelineOpts.Mode != InitModeFilter {
		return false
	}
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
	if st.PipelineOpts.Mode == InitModeFilter {
		return true
	}
	if st.Loot == nil {
		return false
	}
	item, ok := st.Loot.items[st.RowIndex]
	if !ok || st.Loot.expectedMarks[st.RowIndex] != mark {
		return false
	}
	item.Mark = mark
	st.Loot.items[st.RowIndex] = item
	return true
}

type EssenceFilterReadDetailAction struct{}

func (*EssenceFilterReadDetailAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		return false
	}
	d := arg.RecognitionDetail
	if d == nil || d.Results == nil || d.Results.Best == nil {
		return false
	}
	result, ok := d.Results.Best.AsCustom()
	if !ok || result == nil {
		return false
	}
	var read warehouseDetailRead
	if err := json.Unmarshal([]byte(result.Detail), &read); err != nil {
		return inventoryFailed(err)
	}
	if read.Clicks < 1 || read.Clicks > 2 {
		return false
	}
	r := &st.warehouseDetail
	r.Clicks, r.CoreChanged, r.Quality = read.Clicks, read.CoreChanged, read.Quality
	if s := st.Scan; s != nil {
		next := "EssenceFilterCheckItemSlot1"
		if s.current.Mark != lockedMark || s.current.Quality == purple {
			s.skippedReads++
			r.previous = nil
			next = "EssenceGridAdvance"
			if s.current.BoundaryCandidate {
				img, err := taskController(ctx).CacheImage()
				if err != nil || img == nil {
					return false
				}
				equipped, err := ctx.RunRecognition("EssenceGridEquipSlotOccupied", img, nil)
				if err != nil {
					return inventoryFailed(err)
				}
				if equipped == nil || !equipped.Hit {
					next = "EssenceGridVerifyLockedPrefix"
				}
			}
		}
		return ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next}}) == nil
	}
	return true
}

func finishWarehouse(ctx *maa.Context, st *RunState) bool {
	if !st.Scan.verified {
		return inventoryFailed(fmt.Errorf("warehouse end not verified"))
	}
	now := time.Now().UTC()
	s := newSnapshot(st.Account.id)
	s.Coverage[lockedGold] = tierCoverage{CompleteAt: &now}
	for _, item := range st.Scan.items {
		s.appendLockedMatch(item)
	}
	weapons := st.MatchEngine.Weapons()
	explicit := st.PipelineOpts.Mode == InitModeScreenAndReport
	if err := st.Account.commit(s, explicit, st.Account.observedUID(ctx, taskController(ctx))); err != nil {
		return inventoryFailed(err)
	}
	groups := s.groups(weapons)
	matched, _ := inventoryTotals(groups)
	log.Info().Str("component", "EssenceInventory").Int("observed", len(s.Items)).Int("matched", matched).Msg("warehouse snapshot committed")
	if explicit {
		reportSimpleByKey(ctx, "inventory.saved", st.Account.path(), matched, len(groups))
	}
	return true
}
