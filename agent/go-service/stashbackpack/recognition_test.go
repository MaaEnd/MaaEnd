package stashbackpack

import (
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
			"custom_recognition_param": iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
				iconrecognition.WithItemIDs("item_test"),
				iconrecognition.WithItemRecheckFilters(filters.Normal.Any),
				iconrecognition.WithDeduplicate(true),
			),
		},
		"RepoFinder": map[string]any{
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
		{controllerType: "Adb", want: false},
		{controllerType: "", want: false},
	} {
		if got := isSupportedControllerType(testCase.controllerType); got != testCase.want {
			t.Errorf("isSupportedControllerType(%q) = %t, want %t", testCase.controllerType, got, testCase.want)
		}
	}
}
