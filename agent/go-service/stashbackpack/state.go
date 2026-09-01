package stashbackpack

import (
	"fmt"
	"sort"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	componentName = "stashbackpack"
	snapshotS0    = "s0"
	snapshotS1    = "s1"
	snapshotT     = "temporary"
	depotValleyIV = "ValleyIV"
	depotWuling   = "Wuling"
)

type snapshotItem struct {
	ItemID       string `json:"item_id"`
	CategoryType string `json:"category_type"`
	Row          int    `json:"row"`
	Column       int    `json:"column"`
}

type snapshotData struct {
	Items       []snapshotItem
	ColumnCount int
	Pages       [][]snapshotItemWithPosition
}

type restoreState struct {
	Pages     []map[string]int
	PageIndex int
	Ready     bool
}

type sessionState struct {
	Snapshots       map[string]snapshotData
	Targets         []snapshotItem
	Restore         restoreState
	SnapshotChanged bool
	FullComplete    bool
	Depot           string
}

type stateStore struct {
	mu      sync.Mutex
	session sessionState
}

func newStateStore() *stateStore {
	return &stateStore{session: newSessionState()}
}

func newSessionState() sessionState {
	return sessionState{Snapshots: make(map[string]snapshotData)}
}

var globalState = newStateStore()

func (s *stateStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = newSessionState()
}

func (s *stateStore) beginSnapshot(name string) error {
	if name == "" {
		return fmt.Errorf("snapshot name is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.Snapshots[name] = snapshotData{}
	s.session.SnapshotChanged = false
	return nil
}

func (s *stateStore) copySnapshot(sourceName, targetName string) error {
	if sourceName == "" {
		return fmt.Errorf("source snapshot name is empty")
	}
	if targetName == "" {
		return fmt.Errorf("target snapshot name is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.session.Snapshots[sourceName]
	if !ok {
		return fmt.Errorf("snapshot %q does not exist", sourceName)
	}
	copied := snapshotData{
		Items:       append([]snapshotItem(nil), source.Items...),
		ColumnCount: source.ColumnCount,
		Pages:       make([][]snapshotItemWithPosition, len(source.Pages)),
	}
	for index, page := range source.Pages {
		copied.Pages[index] = clonePositionedItems(page)
	}
	s.session.Snapshots[targetName] = copied
	return nil
}

func (s *stateStore) markSnapshotChanged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.SnapshotChanged = true
}

func (s *stateStore) snapshotChanged() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.SnapshotChanged
}

func (s *stateStore) appendSnapshotPage(name string, page []snapshotItemWithPosition) (int, error) {
	if name == "" {
		return 0, fmt.Errorf("snapshot name is empty")
	}
	normalizedPage, pageColumns := sortSnapshotPage(page)

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.session.Snapshots[name]
	if !ok {
		return 0, fmt.Errorf("snapshot %q has not begun", name)
	}
	if existing.ColumnCount == 0 {
		existing.ColumnCount = pageColumns
	}
	existing.Pages = append(existing.Pages, clonePositionedItems(page))
	existing.Items = mergeOrderedPages(existing.Items, normalizedPage)
	existing.Items = reindexSnapshot(existing.Items, existing.ColumnCount)
	s.session.Snapshots[name] = existing
	return len(existing.Items), nil
}

func (s *stateStore) prepareRestore(snapshotName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.session.Snapshots[snapshotName]
	if !ok {
		return fmt.Errorf("snapshot %q does not exist", snapshotName)
	}
	pages := make([]map[string]int, len(snapshot.Pages))
	for index, page := range snapshot.Pages {
		counts := make(map[string]int)
		for _, item := range page {
			if item.ItemID != "" {
				counts[item.ItemID]++
			}
		}
		pages[index] = counts
	}
	s.session.Restore = restoreState{Pages: pages, Ready: true}
	return nil
}

func (s *stateStore) advanceRestorePage() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.session.Restore.Ready {
		return fmt.Errorf("restore search has not been prepared")
	}
	s.session.Restore.PageIndex++
	return nil
}

func (s *stateStore) recordRetrievedItemCount(itemID string, currentCount int) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.session.Restore.Ready {
		return 0, false, fmt.Errorf("restore search has not been prepared")
	}
	if itemID == "" {
		return 0, false, fmt.Errorf("restore item ID is empty")
	}
	pageIndex := s.session.Restore.PageIndex
	for len(s.session.Restore.Pages) <= pageIndex {
		s.session.Restore.Pages = append(s.session.Restore.Pages, make(map[string]int))
	}
	counts := s.session.Restore.Pages[pageIndex]
	if counts == nil {
		counts = make(map[string]int)
		s.session.Restore.Pages[pageIndex] = counts
	}
	baselineCount := counts[itemID]
	if currentCount <= baselineCount {
		return baselineCount, false, nil
	}
	// 同一页后续恢复同 ID 时，以更新后的数量为基线，避免把已恢复物品重复计为成功。
	counts[itemID] = currentCount
	return baselineCount, true, nil
}

func clonePositionedItems(items []snapshotItemWithPosition) []snapshotItemWithPosition {
	return append([]snapshotItemWithPosition(nil), items...)
}

func (s *stateStore) completeFull() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.session.Snapshots[snapshotS0]; !ok {
		return fmt.Errorf("snapshot %q does not exist", snapshotS0)
	}
	if _, ok := s.session.Snapshots[snapshotS1]; !ok {
		return fmt.Errorf("snapshot %q does not exist", snapshotS1)
	}
	s.session.FullComplete = true
	return nil
}

func (s *stateStore) fullComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.FullComplete
}

func (s *stateStore) setDepot(depot string) error {
	if depot != depotValleyIV && depot != depotWuling {
		return fmt.Errorf("unsupported depot %q", depot)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.Depot = depot
	return nil
}

func (s *stateStore) depotIs(depot string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.Depot == depot
}

func (s *stateStore) prepareSnapshotTargets(snapshotName string, categories []string) ([]snapshotItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.session.Snapshots[snapshotName]
	if !ok {
		return nil, fmt.Errorf("snapshot %q does not exist", snapshotName)
	}
	targets := filterCategories(snapshot.Items, categories)
	s.session.Targets = append([]snapshotItem(nil), targets...)
	return append([]snapshotItem(nil), targets...), nil
}

func (s *stateStore) prepareDifferenceTargets(minuendName, subtrahendName string, categories []string) ([]snapshotItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	minuend, ok := s.session.Snapshots[minuendName]
	if !ok {
		return nil, fmt.Errorf("snapshot %q does not exist", minuendName)
	}
	subtrahend, ok := s.session.Snapshots[subtrahendName]
	if !ok {
		return nil, fmt.Errorf("snapshot %q does not exist", subtrahendName)
	}
	targets := filterCategories(orderedDifference(minuend.Items, subtrahend.Items), categories)
	s.session.Targets = append([]snapshotItem(nil), targets...)
	return append([]snapshotItem(nil), targets...), nil
}

func (s *stateStore) currentTarget() (snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.session.Targets) == 0 {
		return snapshotItem{}, false
	}
	return s.session.Targets[0], true
}

func (s *stateStore) consumeTarget() (snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.session.Targets) == 0 {
		return snapshotItem{}, false
	}
	item := s.session.Targets[0]
	s.session.Targets = s.session.Targets[1:]
	return item, true
}

func (s *stateStore) snapshot(name string) ([]snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.session.Snapshots[name]
	return append([]snapshotItem(nil), snapshot.Items...), ok
}

type snapshotItemWithPosition struct {
	ItemID       string
	CategoryType string
	Row          int
	Column       int
	CellBox      maa.Rect
}

func sortSnapshotPage(items []snapshotItemWithPosition) ([]snapshotItem, int) {
	sorted := append([]snapshotItemWithPosition(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Row != sorted[j].Row {
			return sorted[i].Row < sorted[j].Row
		}
		return sorted[i].Column < sorted[j].Column
	})

	columnKeys := make(map[int]struct{})
	result := make([]snapshotItem, 0, len(sorted))
	for _, item := range sorted {
		if item.ItemID == "" {
			continue
		}
		columnKeys[item.Column] = struct{}{}
		result = append(result, snapshotItem{ItemID: item.ItemID, CategoryType: item.CategoryType})
	}
	columnCount := len(columnKeys)
	if len(result) > 0 && columnCount == 0 {
		columnCount = 1
	}
	return reindexSnapshot(result, columnCount), columnCount
}

func reindexSnapshot(items []snapshotItem, columnCount int) []snapshotItem {
	result := append([]snapshotItem(nil), items...)
	if columnCount <= 0 {
		return result
	}
	for index := range result {
		result[index].Row = index / columnCount
		result[index].Column = index % columnCount
	}
	return result
}

// mergeOrderedPages removes the largest exact suffix/prefix overlap while preserving duplicates elsewhere.
func mergeOrderedPages(existing, page []snapshotItem) []snapshotItem {
	maxOverlap := min(len(existing), len(page))
	overlap := 0
	for size := maxOverlap; size > 0; size-- {
		matched := true
		for i := 0; i < size; i++ {
			if existing[len(existing)-size+i].ItemID != page[i].ItemID {
				matched = false
				break
			}
		}
		if matched {
			overlap = size
			break
		}
	}
	merged := append([]snapshotItem(nil), existing...)
	merged = append(merged, page[overlap:]...)
	return merged
}

// orderedDifference performs a multiset subtraction while retaining the minuend's logical grid order.
func orderedDifference(minuend, subtrahend []snapshotItem) []snapshotItem {
	counts := make(map[string]int, len(subtrahend))
	for _, item := range subtrahend {
		counts[item.ItemID]++
	}
	result := make([]snapshotItem, 0, len(minuend))
	for _, item := range minuend {
		if counts[item.ItemID] > 0 {
			counts[item.ItemID]--
			continue
		}
		result = append(result, item)
	}
	return result
}

func filterCategories(items []snapshotItem, categories []string) []snapshotItem {
	if len(categories) == 0 {
		return append([]snapshotItem(nil), items...)
	}
	allowed := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		allowed[category] = struct{}{}
	}
	result := make([]snapshotItem, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.CategoryType]; ok {
			result = append(result, item)
		}
	}
	return result
}
