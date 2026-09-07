package stashbackpack

import (
	"fmt"
	"reflect"
	"testing"
)

func TestBagStoreAttemptsStopAfterThreeClicks(t *testing.T) {
	for successAttempt := 0; successAttempt <= 3; successAttempt++ {
		t.Run(fmt.Sprintf("success_attempt_%d", successAttempt), func(t *testing.T) {
			t.Parallel()
			store := newStateStore()
			store.session.Targets = []snapshotItem{testItem("tool", "Producer")}
			page := []bagPageMatch{{ItemID: "tool", CategoryType: "Producer"}}
			store.updateBagPageMatches(page)
			for attempt := 1; attempt <= 3; attempt++ {
				if _, ok := store.nextBagPageMatch(); !ok {
					t.Fatalf("attempt %d has no target", attempt)
				}
				if _, ok := store.markSelectedBagTargetClicked("test", true); !ok {
					t.Fatalf("attempt %d did not record click", attempt)
				}
				observed := page
				if attempt == successAttempt {
					observed = nil
				}
				confirmed, retry, skipped := store.updateBagPageMatches(observed)
				switch {
				case attempt == successAttempt:
					if len(confirmed) != 1 || len(retry) != 0 || len(skipped) != 0 ||
						confirmed[0].Attempts != attempt || !store.snapshotChanged() {
						t.Fatalf("successful attempt %d was not confirmed: %v / %v / %v", attempt, confirmed, retry, skipped)
					}
				case attempt < 3:
					if len(confirmed) != 0 || len(retry) != 1 || len(skipped) != 0 ||
						retry[0].Attempts != attempt || store.bagTargetsExhausted() || store.snapshotChanged() {
						t.Fatalf("failed attempt %d was not queued for retry: %v / %v / %v", attempt, confirmed, retry, skipped)
					}
					continue
				default:
					if len(confirmed) != 0 || len(retry) != 0 || len(skipped) != 1 ||
						skipped[0].Attempts != 3 || store.snapshotChanged() {
						t.Fatalf("third failed attempt was not skipped: %v / %v / %v", confirmed, retry, skipped)
					}
				}
				if !store.bagTargetsExhausted() || len(store.bagRecognitionItemIDs()) != 0 ||
					len(store.session.BagPage.ClickAttempts) != 0 {
					t.Fatal("completed target retained pending work")
				}
				if _, ok := store.nextBagPageMatch(); ok {
					t.Fatal("completed target can still be clicked")
				}
				return
			}
		})
	}
}

func TestBagStoreAttemptsArePerTargetAndContinueAfterSkipping(t *testing.T) {
	t.Parallel()
	store := newStateStore()
	store.session.Targets = []snapshotItem{
		{ItemID: "tool", CategoryType: "Producer", Row: 0, Column: 0},
		{ItemID: "tool", CategoryType: "Producer", Row: 0, Column: 1},
		{ItemID: "ore", CategoryType: "Ore", Row: 1, Column: 0},
	}
	page := []bagPageMatch{
		{ItemID: "tool", CategoryType: "Producer", Row: 0, Column: 0},
		{ItemID: "tool", CategoryType: "Producer", Row: 0, Column: 1},
	}
	store.updateBagPageMatches(page)
	for attempt := 1; attempt <= 3; attempt++ {
		for targetIndex := 0; targetIndex < 2; targetIndex++ {
			if _, ok := store.nextBagPageMatch(); !ok {
				t.Fatalf("attempt %d, target %d is missing", attempt, targetIndex)
			}
			if _, ok := store.markSelectedBagTargetClicked("test", true); !ok {
				t.Fatal("failed to record click")
			}
		}
		confirmed, retry, skipped := store.updateBagPageMatches(page)
		if len(confirmed) != 0 {
			t.Fatal("unchanged items were confirmed")
		}
		if attempt < 3 {
			if len(retry) != 2 || len(skipped) != 0 || retry[0].Attempts != attempt || retry[1].Attempts != attempt {
				t.Fatalf("same-ID targets shared attempt counts: %v / %v", retry, skipped)
			}
		} else if len(retry) != 0 || len(skipped) != 2 || skipped[0].Attempts != 3 || skipped[1].Attempts != 3 {
			t.Fatalf("targets were not skipped after three clicks each: %v / %v", retry, skipped)
		}
	}
	if store.snapshotChanged() || store.bagTargetsExhausted() || store.bagPageRecognitionFailed() {
		t.Fatal("skipped items changed the snapshot, exhausted later targets, or failed the page")
	}
	if err := store.advanceBagPage(); err != nil {
		t.Fatal(err)
	}
	store.updateBagPageMatches([]bagPageMatch{{ItemID: "ore", CategoryType: "Ore"}})
	if match, ok := store.nextBagPageMatch(); !ok || match.ItemID != "ore" {
		t.Fatal("later target cannot continue after skipping")
	}
	if _, ok := store.markSelectedBagTargetClicked("test", true); !ok {
		t.Fatal("failed to record later target click")
	}
	confirmed, retry, skipped := store.updateBagPageMatches(nil)
	if len(confirmed) != 1 || confirmed[0].Attempts != 1 || len(retry) != 0 || len(skipped) != 0 ||
		!store.snapshotChanged() || !store.bagTargetsExhausted() {
		t.Fatal("later target did not complete independently")
	}
}

func TestBagStoreAttemptsResetWhenPreparingTargets(t *testing.T) {
	for _, difference := range []bool{false, true} {
		t.Run(fmt.Sprintf("difference_%t", difference), func(t *testing.T) {
			t.Parallel()
			store := newStateStore()
			item := testItem("tool", "Producer")
			store.session.Snapshots["items"] = snapshotData{Items: []snapshotItem{item}}
			store.session.Snapshots["empty"] = snapshotData{}
			for batch := 0; batch < 2; batch++ {
				var err error
				if difference {
					_, err = store.prepareDifferenceTargets("items", "empty", nil)
				} else {
					_, err = store.prepareSnapshotTargets("items", nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				page := []bagPageMatch{{ItemID: "tool", CategoryType: "Producer"}}
				store.updateBagPageMatches(page)
				if _, ok := store.nextBagPageMatch(); !ok {
					t.Fatal("new batch has no target")
				}
				if _, ok := store.markSelectedBagTargetClicked("test", true); !ok {
					t.Fatal("failed to record click")
				}
				confirmed, retry, skipped := store.updateBagPageMatches(page)
				if len(confirmed) != 0 || len(retry) != 1 || len(skipped) != 0 || retry[0].Attempts != 1 {
					t.Fatalf("batch %d inherited previous attempt counts", batch)
				}
			}
		})
	}
}

func testItem(id, category string) snapshotItem {
	return snapshotItem{ItemID: id, CategoryType: category}
}

func testPositionedItem(id, category string, row, column int) snapshotItemWithPosition {
	return snapshotItemWithPosition{ItemID: id, CategoryType: category, Row: row, Column: column}
}

func TestMergeOrderedPagesUsesLargestOverlap(t *testing.T) {
	t.Parallel()
	existing := []snapshotItem{testItem("a", "Ore"), testItem("b", "Plant"), testItem("b", "Plant"), testItem("c", "Product")}
	page := []snapshotItem{testItem("b", "Plant"), testItem("b", "Plant"), testItem("c", "Product"), testItem("d", "Doodad")}
	want := []snapshotItem{testItem("a", "Ore"), testItem("b", "Plant"), testItem("b", "Plant"), testItem("c", "Product"), testItem("d", "Doodad")}
	if got := mergeOrderedPages(existing, page); !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeOrderedPages() = %#v, want %#v", got, want)
	}
}

func TestOrderedDifferencePreservesOrderAndMultiplicity(t *testing.T) {
	t.Parallel()
	minuend := []snapshotItem{testItem("a", "Ore"), testItem("b", "Plant"), testItem("a", "Ore"), testItem("c", "Usable")}
	subtrahend := []snapshotItem{testItem("a", "Ore"), testItem("c", "Usable")}
	want := []snapshotItem{testItem("b", "Plant"), testItem("a", "Ore")}
	if got := orderedDifference(minuend, subtrahend); !reflect.DeepEqual(got, want) {
		t.Fatalf("orderedDifference() = %#v, want %#v", got, want)
	}
}

func TestStateStoreMarksOnlyCompleteFullSnapshotPairs(t *testing.T) {
	t.Parallel()
	store := newStateStore()
	if err := store.beginSnapshot(snapshotS0); err != nil {
		t.Fatal(err)
	}
	if err := store.completeFull(); err == nil {
		t.Fatal("completeFull() accepted a missing S1 snapshot")
	}
	if store.fullComplete() {
		t.Fatal("partial snapshot pair was marked complete")
	}
	if err := store.beginSnapshot(snapshotS1); err != nil {
		t.Fatal(err)
	}
	if store.fullComplete() {
		t.Fatal("snapshot pair was marked complete before completeFull()")
	}
	if err := store.completeFull(); err != nil {
		t.Fatal(err)
	}
	if !store.fullComplete() {
		t.Fatal("complete snapshot pair was not marked complete")
	}
	store.reset()
	if store.fullComplete() {
		t.Fatal("reset() retained the completion marker")
	}
}

func TestAppendSnapshotPagesReindexesLogicalGrid(t *testing.T) {
	t.Parallel()
	store := newStateStore()
	if err := store.beginSnapshot(snapshotT); err != nil {
		t.Fatal(err)
	}
	first := []snapshotItemWithPosition{
		testPositionedItem("a", "Ore", 0, 0),
		testPositionedItem("b", "Plant", 0, 1),
		testPositionedItem("c", "Product", 1, 0),
		testPositionedItem("d", "Doodad", 1, 1),
	}
	second := []snapshotItemWithPosition{
		testPositionedItem("c", "Product", 0, 0),
		testPositionedItem("d", "Doodad", 0, 1),
		testPositionedItem("e", "Usable", 1, 0),
		testPositionedItem("f", "Producer", 1, 1),
	}
	if _, err := store.appendSnapshotPage(snapshotT, first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendSnapshotPage(snapshotT, second); err != nil {
		t.Fatal(err)
	}
	got, ok := store.snapshot(snapshotT)
	if !ok {
		t.Fatal("temporary snapshot does not exist")
	}
	want := []snapshotItem{
		{ItemID: "a", CategoryType: "Ore", Row: 0, Column: 0},
		{ItemID: "b", CategoryType: "Plant", Row: 0, Column: 1},
		{ItemID: "c", CategoryType: "Product", Row: 1, Column: 0},
		{ItemID: "d", CategoryType: "Doodad", Row: 1, Column: 1},
		{ItemID: "e", CategoryType: "Usable", Row: 2, Column: 0},
		{ItemID: "f", CategoryType: "Producer", Row: 2, Column: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %#v, want %#v", got, want)
	}
}

func TestPrepareDifferenceTargetsFiltersCategories(t *testing.T) {
	t.Parallel()
	store := newStateStore()
	if err := store.beginSnapshot("before"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendSnapshotPage("before", []snapshotItemWithPosition{
		testPositionedItem("ore", "Ore", 0, 0),
		testPositionedItem("tool", "Producer", 0, 1),
		testPositionedItem("device", "PortableDevice", 0, 2),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.beginSnapshot("after"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendSnapshotPage("after", []snapshotItemWithPosition{
		testPositionedItem("ore", "Ore", 0, 0),
	}); err != nil {
		t.Fatal(err)
	}
	targets, err := store.prepareDifferenceTargets("before", "after", []string{"Producer"})
	if err != nil {
		t.Fatal(err)
	}
	want := []snapshotItem{{ItemID: "tool", CategoryType: "Producer", Row: 0, Column: 1}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}
}

func TestCopySnapshotIsIndependentAndBeginResetsChangedMarker(t *testing.T) {
	t.Parallel()
	store := newStateStore()
	if err := store.beginSnapshot("source"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendSnapshotPage("source", []snapshotItemWithPosition{
		testPositionedItem("ore", "Ore", 0, 0),
	}); err != nil {
		t.Fatal(err)
	}
	store.markSnapshotChanged()
	if !store.snapshotChanged() {
		t.Fatal("snapshot change marker was not recorded")
	}
	if err := store.copySnapshot("source", "copied"); err != nil {
		t.Fatal(err)
	}
	if err := store.beginSnapshot("source"); err != nil {
		t.Fatal(err)
	}
	if store.snapshotChanged() {
		t.Fatal("beginSnapshot() retained the snapshot change marker")
	}
	got, ok := store.snapshot("copied")
	if !ok {
		t.Fatal("copied snapshot does not exist")
	}
	want := []snapshotItem{{ItemID: "ore", CategoryType: "Ore", Row: 0, Column: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("copied snapshot = %#v, want %#v", got, want)
	}
}
