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
	positions   map[[2]int]int
	items       []inventoryItem
	current     gridSelection
	pendingMark string
	verified    bool
}

type gridSelection struct {
	Row      int    `json:"selected_row"`
	Col      int    `json:"selected_col"`
	Quality  string `json:"selected_quality"`
	Mark     string `json:"selected_thumb_state"`
	Issued   int    `json:"issued_cells"`
	Resolved int    `json:"resolved_cells"`
	Expected int    `json:"expected_cells"`
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

// EssenceFilterUnreadPurpleAction handles a failed affix read after both the
// position and the purple tier have been confirmed.
type EssenceFilterUnreadPurpleAction struct{}

func (*EssenceFilterUnreadPurpleAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		return false
	}
	if s := st.Scan; s != nil {
		c := s.current
		key := [2]int{c.Row, c.Col}
		if c.Row < 0 || c.Col < 0 || c.Quality != purple || !validQualityAndMark(c.Quality, c.Mark) {
			return false
		}
		if _, recorded := s.positions[key]; recorded {
			return false
		}
		s.positions[key] = len(s.items)
		s.items = append(s.items, inventoryItem{Quality: purple, Mark: c.Mark})
		return true
	}
	r := st.Loot
	if r == nil || r.quality != purple || !validQualityAndMark(r.quality, r.mark) {
		return false
	}
	if _, decided := r.expectedMarks[st.RowIndex]; decided {
		return false
	}
	if err := r.observe(st.RowIndex, inventoryItem{Quality: purple, Mark: r.mark}); err != nil {
		return inventoryFailed(err)
	}
	r.expectedMarks[st.RowIndex] = r.mark
	return true
}

func (s *warehouseScan) verify(c gridSelection) error {
	if s.pendingMark != "" {
		return fmt.Errorf("warehouse mark %q not confirmed", s.pendingMark)
	}
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
	if st.Scan != nil {
		if st.Scan.pendingMark != "" {
			return inventoryFailed(fmt.Errorf("advance before warehouse mark confirmation"))
		}
		if arg.CurrentTaskName == "EssenceGridVerifyComplete" {
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
	if st.Scan != nil {
		c := st.Scan.current
		i, ok := st.Scan.positions[[2]int{c.Row, c.Col}]
		if !ok || st.Scan.pendingMark != mark {
			return false
		}
		st.Scan.items[i].Mark = mark
		st.Scan.pendingMark = ""
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

func finishWarehouse(ctx *maa.Context, st *RunState) bool {
	if !st.Scan.verified {
		return inventoryFailed(fmt.Errorf("warehouse end not verified"))
	}
	now := time.Now().UTC()
	s := newSnapshot(st.Account.id)
	s.Coverage[gold] = tierCoverage{CompleteAt: &now}
	s.Coverage[purple] = tierCoverage{CompleteAt: &now}
	for _, item := range st.Scan.items {
		s.appendObservation(item)
	}
	weapons := st.MatchEngine.Weapons()
	explicit := st.PipelineOpts.Mode == InitModeScreenAndReport
	if err := st.Account.commit(s, explicit, st.Account.observedUID(ctx, taskController(ctx))); err != nil {
		return inventoryFailed(err)
	}
	groups := s.groups(weapons)
	matched, _ := inventoryTotals(groups)
	log.Info().Str("component", "EssenceInventory").Int("observed", len(s.Items)).Int("matched", matched).Msg("warehouse snapshot committed")
	if s.Coverage[purple].Unread > 0 {
		reportSimpleByKey(ctx, "inventory.unread_purple", s.Coverage[purple].Unread)
	}
	if explicit {
		reportSimpleByKey(ctx, "inventory.saved", st.Account.path(), len(st.Scan.items), matched, len(groups))
	}
	return true
}
