package stashbackpack

import (
	"fmt"
	"sort"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const (
	componentName   = "stashbackpack"
	snapshotS0      = "s0"
	snapshotS1      = "s1"
	snapshotT       = "temporary"
	snapshotWorking = "working"
	depotValleyIV   = "ValleyIV"
	depotWuling     = "Wuling"

	// storeReasonManualStored 与流水线手动存放确认节点的 reason 保持一致；
	// 仅该 reason 的确认点击会计入取回记录，存放新物品等其余流程不参与取回。
	storeReasonManualStored = "manual_item_stored"

	// bagStoreMaxAttempts 包含首次点击；每个快照目标尝试耗尽后跳过，避免无进展循环。
	bagStoreMaxAttempts = 3
)

// storedItem 记录一次已确认存入仓库的物品堆叠，供取回任务按存放顺序回放。
type storedItem struct {
	ItemID       string `json:"item_id"`
	CategoryType string `json:"category_type"`
}

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

type bagPageMatch struct {
	ItemID       string   `json:"item_id"`
	CategoryType string   `json:"category_type"`
	Row          int      `json:"row"`
	Column       int      `json:"column"`
	CellBox      maa.Rect `json:"cell_box"`
}

type bagClickedTarget struct {
	Item     snapshotItem
	Reason   string
	Attempts int
}

type bagPageState struct {
	Matches        []bagPageMatch
	Selected       *snapshotItem
	BaselineCounts map[string]int
	Clicked        []bagClickedTarget
	// 用快照逻辑位置区分同 ID 的多个目标，计数仅保留在本轮存放中。
	ClickAttempts     map[snapshotItem]int
	PageIndex         int
	RecognitionFailed bool
}

type sessionState struct {
	Snapshots map[string]snapshotData
	Targets   []snapshotItem
	// Stored 是本存取对中已确认存入仓库的物品记录，取回任务按此顺序回放；
	// 取回中止时剩余目标会写回这里（仅本批次内有效，Agent 重启后失效）。
	Stored []storedItem
	// repoBaseline 记录转移前仓库当前页的物品格子数，取回验证以“少一格”判定成功，
	// 从而正确处理仓库中存在多个同物品堆叠的情况。
	repoBaselineCount int
	repoBaselineItem  storedItem
	repoBaselineValid bool
	// bagBaseline 与 repoBaseline 同理，用于手动存放时验证背包源物品已移入仓库；
	// 背包存在多个同物品堆叠时，存在性判定会误判，必须按数量差判定。
	bagBaselineCount int
	bagBaselineItem  storedItem
	bagBaselineValid bool
	BagPage          bagPageState
	FullComplete     bool
	Depot            string
	QuickStash       bool
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
	s.resetForStash(false)
}

// resetForStash 记录本次存放的一键存放设置，供后续任务的独立 Context 复用。
func (s *stateStore) resetForStash(quickStash bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = newSessionState()
	s.session.QuickStash = quickStash
}

func (s *stateStore) quickStashEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.FullComplete && s.session.QuickStash
}

func (s *stateStore) beginSnapshot(name string) error {
	if name == "" {
		return fmt.Errorf("snapshot name is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.Snapshots[name] = snapshotData{}
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

// replaceSnapshotPages 先在锁外构造完整快照，再一次性替换目标，避免扫描失败留下半成品。
func (s *stateStore) replaceSnapshotPages(name string, pages [][]snapshotItemWithPosition) (int, error) {
	if name == "" {
		return 0, fmt.Errorf("snapshot name is empty")
	}

	replacement := snapshotData{Pages: make([][]snapshotItemWithPosition, 0, len(pages))}
	for _, page := range pages {
		normalizedPage, pageColumns := sortSnapshotPage(page)
		if replacement.ColumnCount == 0 {
			replacement.ColumnCount = pageColumns
		}
		replacement.Pages = append(replacement.Pages, clonePositionedItems(page))
		replacement.Items = mergeOrderedPages(replacement.Items, normalizedPage)
	}
	replacement.Items = reindexSnapshot(replacement.Items, replacement.ColumnCount)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.Snapshots[name] = replacement
	// 初始快照落盘时同步建立 working 副本（不含分页明细，取回已改为记录驱动，
	// 分页数据没有消费方），后续由手动存放确认增量扣减，
	// 因此存放完成后不再需要任何扫描或派生步骤。
	if name == snapshotS0 {
		s.session.Snapshots[snapshotWorking] = snapshotData{
			Items:       append([]snapshotItem(nil), replacement.Items...),
			ColumnCount: replacement.ColumnCount,
		}
	}
	return len(replacement.Items), nil
}

// recordStoredItem 在手动存放确认成功时调用：记入取回记录，并从 working 快照中
// 实时扣掉对应物品。调用方必须已持有状态锁。
func (s *stateStore) recordStoredItem(item storedItem) {
	s.session.Stored = append(s.session.Stored, item)
	working, ok := s.session.Snapshots[snapshotWorking]
	if !ok {
		return
	}
	for index, candidate := range working.Items {
		if candidate.ItemID != item.ItemID {
			continue
		}
		working.Items = append(working.Items[:index], working.Items[index+1:]...)
		working.Items = reindexSnapshot(working.Items, working.ColumnCount)
		s.session.Snapshots[snapshotWorking] = working
		break
	}
}

// noteRepoItemCount 记录转移前仓库当前页的物品格子数基线。
func (s *stateStore) noteRepoItemCount(item storedItem, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.repoBaselineItem = item
	s.session.repoBaselineCount = count
	s.session.repoBaselineValid = true
}

// repoItemMoved 以“格子数比转移前至少少一”判定转移成功，
// 仓库中存在多个同物品堆叠时依然成立。基线缺失时按未移动处理（走安全中止路径）。
func (s *stateStore) repoItemMoved(item storedItem, count int) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.session.repoBaselineValid ||
		s.session.repoBaselineItem.ItemID != item.ItemID ||
		s.session.repoBaselineItem.CategoryType != item.CategoryType {
		return false, 0
	}
	return count < s.session.repoBaselineCount, s.session.repoBaselineCount
}

// noteBagItemCount 记录转移前背包当前页的物品格子数基线（手动存放验证用）。
func (s *stateStore) noteBagItemCount(item storedItem, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.bagBaselineItem = item
	s.session.bagBaselineCount = count
	s.session.bagBaselineValid = true
}

// bagItemMoved 以“格子数比转移前至少少一”判定手动存放成功，
// 背包中存在多个同物品堆叠时依然成立。基线缺失时按未移动处理（走失败路径）。
func (s *stateStore) bagItemMoved(item storedItem, count int) (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.session.bagBaselineValid ||
		s.session.bagBaselineItem.ItemID != item.ItemID ||
		s.session.bagBaselineItem.CategoryType != item.CategoryType {
		return false, 0
	}
	return count < s.session.bagBaselineCount, s.session.bagBaselineCount
}

// prepareStoredTargets 将存放记录整体转为取回目标队列，记录随即清空；
// 取回中止时由 abortRestore 把剩余目标写回记录。
func (s *stateStore) prepareStoredTargets() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	targets := make([]snapshotItem, 0, len(s.session.Stored))
	for _, item := range s.session.Stored {
		targets = append(targets, snapshotItem{ItemID: item.ItemID, CategoryType: item.CategoryType})
	}
	s.session.Stored = nil
	s.session.Targets = targets
	s.session.BagPage = bagPageState{}
	return len(targets), nil
}

// abortRestore 在取回无法继续（如背包已满）时保留剩余目标并结束本次取回。
func (s *stateStore) abortRestore() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := append([]storedItem(nil), s.session.Stored...)
	for _, item := range s.session.Targets {
		remaining = append(remaining, storedItem{ItemID: item.ItemID, CategoryType: item.CategoryType})
	}
	s.session.Stored = remaining
	s.session.Targets = nil
	s.session.BagPage = bagPageState{}
	return len(remaining)
}

func (s *stateStore) storedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.session.Stored)
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
	s.session.BagPage = bagPageState{}
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
	s.session.BagPage = bagPageState{}
	return append([]snapshotItem(nil), targets...), nil
}

func (s *stateStore) currentTarget() (snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session.BagPage.Selected != nil {
		return *s.session.BagPage.Selected, true
	}
	if len(s.session.Targets) == 0 {
		return snapshotItem{}, false
	}
	return s.session.Targets[0], true
}

func (s *stateStore) consumeTarget() (snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumeTargetLocked()
}

func (s *stateStore) consumeTargetLocked() (snapshotItem, bool) {
	if s.session.BagPage.Selected != nil {
		selected := *s.session.BagPage.Selected
		s.session.BagPage.Selected = nil
		for index, item := range s.session.Targets {
			if item.ItemID != selected.ItemID || item.CategoryType != selected.CategoryType {
				continue
			}
			s.session.Targets = append(s.session.Targets[:index], s.session.Targets[index+1:]...)
			return selected, true
		}
		return snapshotItem{}, false
	}
	if len(s.session.Targets) == 0 {
		return snapshotItem{}, false
	}
	item := s.session.Targets[0]
	s.session.Targets = s.session.Targets[1:]
	return item, true
}

// consumeStoredTarget 消费当前手动存放目标，并原子地写入后续取回记录。
func (s *stateStore) consumeStoredTarget() (snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.consumeTargetLocked()
	if !ok {
		return snapshotItem{}, false
	}
	s.recordStoredItem(storedItem{ItemID: item.ItemID, CategoryType: item.CategoryType})
	return item, true
}

func (s *stateStore) bagRecognitionItemIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.session.Targets)+len(s.session.BagPage.Clicked))
	seen := make(map[string]struct{}, cap(ids))
	for _, item := range s.session.Targets {
		if item.ItemID == "" {
			continue
		}
		if _, ok := seen[item.ItemID]; ok {
			continue
		}
		seen[item.ItemID] = struct{}{}
		ids = append(ids, item.ItemID)
	}
	for _, clicked := range s.session.BagPage.Clicked {
		if clicked.Item.ItemID == "" {
			continue
		}
		if _, ok := seen[clicked.Item.ItemID]; ok {
			continue
		}
		seen[clicked.Item.ItemID] = struct{}{}
		ids = append(ids, clicked.Item.ItemID)
	}
	return ids
}

func (s *stateStore) nextBagPageMatch() (bagPageMatch, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.session.BagPage.Matches) > 0 {
		match := s.session.BagPage.Matches[0]
		s.session.BagPage.Matches = s.session.BagPage.Matches[1:]
		for _, target := range s.session.Targets {
			if target.ItemID != match.ItemID {
				continue
			}
			targetCopy := target
			s.session.BagPage.Selected = &targetCopy
			return match, true
		}
	}
	return bagPageMatch{}, false
}

// updateBagPageMatches 用本页复扫结果核销已点击目标，再缓存尚未处理的格子。
func (s *stateStore) updateBagPageMatches(matches []bagPageMatch) (confirmed, failed, skipped []bagClickedTarget) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.session.BagPage.RecognitionFailed = false
	currentCounts := make(map[string]int)
	for _, match := range matches {
		currentCounts[match.ItemID]++
	}
	if len(s.session.BagPage.Clicked) > 0 {
		movedCounts := make(map[string]int)
		for itemID, baseline := range s.session.BagPage.BaselineCounts {
			if moved := baseline - currentCounts[itemID]; moved > 0 {
				movedCounts[itemID] = moved
			}
		}
		for _, clicked := range s.session.BagPage.Clicked {
			if movedCounts[clicked.Item.ItemID] > 0 {
				movedCounts[clicked.Item.ItemID]--
				confirmed = append(confirmed, clicked)
				delete(s.session.BagPage.ClickAttempts, clicked.Item)
				// 只有独立存放任务的手动存放计入取回记录；
				// 存放新物品（嵌入流程与取回任务选项）存入的物品按设计留在仓库，不参与取回。
				if clicked.Reason == storeReasonManualStored {
					s.recordStoredItem(storedItem{
						ItemID:       clicked.Item.ItemID,
						CategoryType: clicked.Item.CategoryType,
					})
				}
				continue
			}
			if clicked.Attempts >= bagStoreMaxAttempts {
				skipped = append(skipped, clicked)
				delete(s.session.BagPage.ClickAttempts, clicked.Item)
				continue
			}
			failed = append(failed, clicked)
			s.session.Targets = append(s.session.Targets, clicked.Item)
		}
		s.session.BagPage.Clicked = nil
		sort.SliceStable(s.session.Targets, func(i, j int) bool {
			if s.session.Targets[i].Row != s.session.Targets[j].Row {
				return s.session.Targets[i].Row < s.session.Targets[j].Row
			}
			return s.session.Targets[i].Column < s.session.Targets[j].Column
		})
	}

	s.session.BagPage.BaselineCounts = currentCounts
	s.session.BagPage.Matches = nil
	s.session.BagPage.Selected = nil
	remainingCounts := make(map[string]int)
	for _, target := range s.session.Targets {
		remainingCounts[target.ItemID]++
	}
	for _, match := range matches {
		if remainingCounts[match.ItemID] == 0 {
			continue
		}
		remainingCounts[match.ItemID]--
		s.session.BagPage.Matches = append(s.session.BagPage.Matches, match)
	}
	return confirmed, failed, skipped
}

func (s *stateStore) markBagPageRecognitionFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session.BagPage.RecognitionFailed = true
}

func (s *stateStore) bagPageRecognitionFailed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.BagPage.RecognitionFailed
}

func (s *stateStore) markSelectedBagTargetClicked(reason string) (snapshotItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session.BagPage.Selected == nil {
		return snapshotItem{}, false
	}
	selected := *s.session.BagPage.Selected
	s.session.BagPage.Selected = nil
	for index, target := range s.session.Targets {
		if target.ItemID != selected.ItemID || target.CategoryType != selected.CategoryType {
			continue
		}
		s.session.Targets = append(s.session.Targets[:index], s.session.Targets[index+1:]...)
		if s.session.BagPage.ClickAttempts == nil {
			s.session.BagPage.ClickAttempts = make(map[snapshotItem]int)
		}
		s.session.BagPage.ClickAttempts[target]++
		s.session.BagPage.Clicked = append(s.session.BagPage.Clicked, bagClickedTarget{
			Item:     target,
			Reason:   reason,
			Attempts: s.session.BagPage.ClickAttempts[target],
		})
		return target, true
	}
	return snapshotItem{}, false
}

func (s *stateStore) advanceBagPage() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.session.BagPage.Clicked) > 0 {
		return fmt.Errorf("bag page contains unverified clicked targets")
	}
	s.session.BagPage.PageIndex++
	s.session.BagPage.Matches = nil
	s.session.BagPage.Selected = nil
	s.session.BagPage.BaselineCounts = nil
	s.session.BagPage.RecognitionFailed = false
	return nil
}

func (s *stateStore) bagTargetsExhausted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.session.Targets) == 0 && len(s.session.BagPage.Clicked) == 0 &&
		len(s.session.BagPage.Matches) == 0 && s.session.BagPage.Selected == nil
}

func (s *stateStore) discardRemainingBagTargets() []snapshotItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	discarded := append([]snapshotItem(nil), s.session.Targets...)
	s.session.Targets = nil
	s.session.BagPage = bagPageState{PageIndex: s.session.BagPage.PageIndex}
	return discarded
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
