package stashbackpack

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestBuildFinderOverrideUsesTypedItemAndCategoryFilters(t *testing.T) {
	item := snapshotItem{ItemID: "item_test", CategoryType: "Producer"}
	param := nextItemParam{
		BagNodes:  []string{"BagFinder"},
		RepoNodes: []string{"RepoFinder"},
	}
	filters := iconrecognition.StorageFilter()
	want := map[string]any{
		"BagFinder": map[string]any{
			"custom_recognition": iconrecognition.CustomRecognitionName,
			"custom_recognition_param": iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs("item_test"),
				iconrecognition.WithItemRecheckFilters(filters.Normal.Any),
				iconrecognition.WithDeduplicate(true),
			),
		},
		"RepoFinder": map[string]any{
			"custom_recognition": iconrecognition.CustomRecognitionName,
			"custom_recognition_param": iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs("item_test"),
				iconrecognition.WithItemRecheckFilters(filters.Normal.Producer),
				iconrecognition.WithDeduplicate(true),
			),
		},
	}

	if got := buildFinderOverride(item, param); !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFinderOverride() = %#v, want %#v", got, want)
	}
}

func TestFirstReplenishableMatchSkipsFullStacksInGridOrder(t *testing.T) {
	matches := []iconrecognition.Match{
		{ItemID: "item_test", CellBox: maa.Rect{100, 200, 64, 64}},
		{ItemID: "item_test", CellBox: maa.Rect{20, 100, 64, 64}},
		{ItemID: "item_test", CellBox: maa.Rect{100, 100, 64, 64}},
	}
	quantities := map[int]int{20: 50, 100: 25}
	got, ok := firstReplenishableMatch(matches, func(match iconrecognition.Match) (int, bool, error) {
		return quantities[match.CellBox.X()], true, nil
	})
	if !ok || got.CellBox != (maa.Rect{100, 100, 64, 64}) {
		t.Fatalf("firstReplenishableMatch() = (%v, %t), want second cell in the first row", got.CellBox, ok)
	}
}

func TestFirstReplenishableMatchRejectsAllFullStacks(t *testing.T) {
	matches := []iconrecognition.Match{{ItemID: "item_test", CellBox: maa.Rect{20, 100, 64, 64}}}
	if _, ok := firstReplenishableMatch(matches, func(iconrecognition.Match) (int, bool, error) {
		return usableItemStackLimit, true, nil
	}); ok {
		t.Fatal("firstReplenishableMatch() selected a full stack")
	}
}

func TestFirstReplenishableMatchKeepsOCRMissEligible(t *testing.T) {
	want := iconrecognition.Match{ItemID: "item_test", CellBox: maa.Rect{20, 100, 64, 64}}
	got, ok := firstReplenishableMatch([]iconrecognition.Match{want}, func(iconrecognition.Match) (int, bool, error) {
		return 0, false, nil
	})
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("firstReplenishableMatch() = (%#v, %t), want OCR-missed cell", got, ok)
	}
}

func TestReplenishMergedTargetProcessesBothStacks(t *testing.T) {
	globalState.reset()
	t.Cleanup(globalState.reset)
	globalState.session.Snapshots[snapshotWorking] = snapshotData{Items: []snapshotItem{
		{ItemID: "medicine", CategoryType: "Usable", Row: 0, Column: 0},
		{ItemID: "medicine", CategoryType: "Usable", Row: 0, Column: 1},
	}}
	action := &StateAction{}
	if !action.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"operation":"prepare_snapshot","snapshot":"working","categories":["Usable"],"merge_same_items":true}`}) {
		t.Fatal("failed to prepare replenishment targets")
	}
	matches := []iconrecognition.Match{
		{ItemID: "medicine", CellBox: maa.Rect{20, 100, 64, 64}},
		{ItemID: "medicine", CellBox: maa.Rect{100, 100, 64, 64}},
	}
	quantities := map[int]int{20: 20, 100: 30}
	for _, wantX := range []int{20, 100} {
		if _, ok := globalState.currentTarget(); !ok {
			t.Fatal("same-item target was consumed before both stacks were processed")
		}
		target := storedItem{ItemID: "medicine", CategoryType: "Usable"}
		match, ok := firstReplenishableMatch(globalState.unprocessedReplenishMatches(target, matches), func(match iconrecognition.Match) (int, bool, error) {
			return quantities[match.CellBox.X()], true, nil
		})
		if !ok || match.CellBox.X() != wantX {
			t.Fatalf("selected stack = %v, want x=%d", match.CellBox, wantX)
		}
		// 模拟该格补满，再走 Pipeline 使用的消费动作；下一次必须选中另一格。
		globalState.setReplenishPending(target, match.CellBox)
		quantities[wantX] = usableItemStackLimit
		if !action.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"operation":"consume_target","reason":"replenished"}`}) {
			t.Fatal("failed to consume replenished stack")
		}
	}
	if _, ok := globalState.currentTarget(); ok {
		t.Fatal("replenishment target remained after both stacks were processed")
	}
}

func TestReplenishMissingConsumesWholeGroup(t *testing.T) {
	for _, reason := range []string{"replenish_repo_not_found", "replenish_bag_not_found"} {
		t.Run(reason, func(t *testing.T) {
			globalState.reset()
			t.Cleanup(globalState.reset)
			globalState.session.Targets = mergeSameSnapshotTargets([]snapshotItem{
				{ItemID: "medicine", CategoryType: "Usable"},
				{ItemID: "medicine", CategoryType: "Usable", Column: 1},
				{ItemID: "other", CategoryType: "Usable", Column: 2},
			})
			action := &StateAction{}
			if !action.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"operation":"consume_target","reason":"` + reason + `"}`}) {
				t.Fatal("failed to skip replenishment group")
			}
			if target, ok := globalState.currentTarget(); !ok || target.ItemID != "other" {
				t.Fatalf("next target = %v, want other", target)
			}
		})
	}
}

func TestReplenishmentAttemptsEachCellOnceUntilBagScroll(t *testing.T) {
	for _, size := range []int{64, 80} {
		t.Run(fmt.Sprintf("cell_%d", size), func(t *testing.T) {
			globalState.reset()
			t.Cleanup(globalState.reset)
			globalState.session.Targets = mergeSameSnapshotTargets([]snapshotItem{
				{ItemID: "medicine", CategoryType: "Usable"},
				{ItemID: "medicine", CategoryType: "Usable", Column: 1},
			})
			matches := []iconrecognition.Match{
				{ItemID: "medicine", CellBox: maa.Rect{20, 100, size, size}},
				{ItemID: "medicine", CellBox: maa.Rect{20 + size + 5, 100, size, size}},
			}
			action := &StateAction{}
			for index, want := range matches {
				// 即使数量持续识别失败、拖动后画面没有变化，也必须转向下一格。
				target := storedItem{ItemID: "medicine", CategoryType: "Usable"}
				candidates := globalState.unprocessedReplenishMatches(target, matches)
				match, ok := firstReplenishableMatch(candidates, func(iconrecognition.Match) (int, bool, error) {
					return 0, false, nil
				})
				if !ok || match.CellBox != want.CellBox {
					t.Fatalf("attempt %d selected %v, want %v", index, match.CellBox, want.CellBox)
				}
				globalState.setReplenishPending(target, match.CellBox)
				// 拖动前 And 再次识别时，候选仍须可用，不能提前排除。
				if got := globalState.unprocessedReplenishMatches(target, matches); len(got) != len(candidates) {
					t.Fatal("candidate was excluded before the drag")
				}
				globalState.setReplenishPending(target, match.CellBox)
				if !action.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"operation":"consume_target","reason":"replenished"}`}) {
					t.Fatal("failed to record drag attempt")
				}
			}
			target := storedItem{ItemID: "medicine", CategoryType: "Usable"}
			if got := globalState.unprocessedReplenishMatches(target, matches); len(got) != 0 {
				t.Fatalf("attempted cells remain eligible: %v", got)
			}
			jittered := matches[0]
			jittered.CellBox[0]++
			jittered.CellBox[1]--
			if got := globalState.unprocessedReplenishMatches(target, []iconrecognition.Match{jittered}); len(got) != 0 {
				t.Fatal("minor box jitter made an attempted cell eligible again")
			}
			if !action.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"operation":"reset_replenish_page"}`}) {
				t.Fatal("failed to reset page after bag scroll")
			}
			if got := globalState.unprocessedReplenishMatches(target, matches); len(got) != len(matches) {
				t.Fatal("new page inherited the previous page's attempted cells")
			}
		})
	}
}

func TestReplenishmentMarksFinalRecognitionBox(t *testing.T) {
	store := newStateStore()
	store.session.Targets = []snapshotItem{{ItemID: "medicine", CategoryType: "Usable"}}
	first := iconrecognition.Match{CellBox: maa.Rect{20, 100, 64, 64}}
	final := iconrecognition.Match{CellBox: maa.Rect{100, 100, 64, 64}}
	target := storedItem{ItemID: "medicine", CategoryType: "Usable"}
	store.setReplenishPending(target, first.CellBox)
	store.setReplenishPending(target, final.CellBox)
	if _, ok := store.consumeReplenishTarget(); !ok {
		t.Fatal("failed to consume drag attempt")
	}
	if got := store.unprocessedReplenishMatches(target, []iconrecognition.Match{first, final}); !reflect.DeepEqual(got, []iconrecognition.Match{first}) {
		t.Fatalf("remaining cells = %v, want only the initial search candidate", got)
	}
}

func TestTargetCategoryRecognitionMatchesCurrentTarget(t *testing.T) {
	globalState.reset()
	t.Cleanup(globalState.reset)
	if err := globalState.beginSnapshot("targets"); err != nil {
		t.Fatal(err)
	}
	if _, err := globalState.appendSnapshotPage("targets", []snapshotItemWithPosition{
		{ItemID: "item_test", CategoryType: "Producer", Row: 0, Column: 0},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := globalState.prepareSnapshotTargets("targets", nil); err != nil {
		t.Fatal(err)
	}

	recognition := &TargetCategoryRecognition{}
	if _, matched := recognition.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"category":"Producer"}`,
	}); !matched {
		t.Fatal("TargetCategoryRecognition.Run() did not match the current target category")
	}
	if _, matched := recognition.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"category":"Usable"}`,
	}); matched {
		t.Fatal("TargetCategoryRecognition.Run() matched a different category")
	}
}

func TestFullCompleteRecognitionRejectsPartialSnapshots(t *testing.T) {
	globalState.reset()
	t.Cleanup(globalState.reset)
	recognition := &FullCompleteRecognition{}
	arg := &maa.CustomRecognitionArg{}

	if err := globalState.beginSnapshot(snapshotS0); err != nil {
		t.Fatal(err)
	}
	if err := globalState.beginSnapshot(snapshotS1); err != nil {
		t.Fatal(err)
	}
	if _, matched := recognition.Run(nil, arg); matched {
		t.Fatal("FullCompleteRecognition.Run() matched before complete_full")
	}
	if err := globalState.completeFull(); err != nil {
		t.Fatal(err)
	}
	if _, matched := recognition.Run(nil, arg); !matched {
		t.Fatal("FullCompleteRecognition.Run() did not match a completed full snapshot pair")
	}
}

func TestSupportedControllerType(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		controllerType string
		want           bool
	}{
		{controllerType: "Win32", want: true},
		{controllerType: " win32 ", want: true},
		{controllerType: "Adb", want: true},
		{controllerType: " ADB ", want: true},
		{controllerType: "MacOS", want: false},
		{controllerType: "PlayCover", want: false},
		{controllerType: "", want: false},
	} {
		if got := isSupportedControllerType(testCase.controllerType); got != testCase.want {
			t.Errorf("isSupportedControllerType(%q) = %t, want %t", testCase.controllerType, got, testCase.want)
		}
	}
}
