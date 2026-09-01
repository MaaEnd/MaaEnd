package stashbackpack

import (
	"reflect"
	"testing"
)

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
