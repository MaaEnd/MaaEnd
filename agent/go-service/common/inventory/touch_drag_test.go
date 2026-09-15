package inventory

import (
	"image"
	"reflect"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type fakeDragRunner struct {
	events []string
	waits  []time.Duration
	frame  image.Image
}

func (r *fakeDragRunner) swipe(source, destination maa.Rect, _ time.Duration) bool {
	r.record("swipe")
	sourceX, sourceY := rectCenter(source)
	destinationX, destinationY := rectCenter(destination)
	if sourceX != 415 || sourceY != 315 || destinationX != 847 || destinationY != 234 {
		r.events = append(r.events, "unexpected_destination")
	}
	return true
}

func (r *fakeDragRunner) record(event string) {
	r.events = append(r.events, event)
}

func (r *fakeDragRunner) RunAction(node string, _ maa.Rect, _ string, _ ...any) (*maa.ActionDetail, error) {
	r.record(node)
	return &maa.ActionDetail{Success: true}, nil
}

func (r *fakeDragRunner) RunRecognition(node string, img image.Image, _ ...any) (*maa.RecognitionDetail, error) {
	r.record(node)
	if img != r.frame {
		r.events = append(r.events, "unexpected_frame")
	}
	return &maa.RecognitionDetail{
		Hit: node == "__InventoryTransferStackButtonLeft",
		Box: maa.Rect{804, 191, 86, 86},
	}, nil
}

func (r *fakeDragRunner) screenshot() (image.Image, error) {
	r.record("screenshot")
	return r.frame, nil
}

func (r *fakeDragRunner) stopping() bool { return false }

func (r *fakeDragRunner) release(contact int32) bool {
	if contact == sourceContact {
		r.record("release_source")
	}
	return true
}

func (r *fakeDragRunner) wait(duration time.Duration) bool {
	r.record("wait_before_release_source")
	r.waits = append(r.waits, duration)
	return true
}

func (r *fakeDragRunner) move(_ int32, x, y int32) bool {
	r.record("move")
	if x != 847 || y != 234 {
		r.events = append(r.events, "unexpected_destination")
	}
	return true
}

func TestRepoToBagADBKeepsSourceContactUntilDestinationMove(t *testing.T) {
	runner := &fakeDragRunner{
		frame: image.NewRGBA(image.Rect(0, 0, 1280, 720)),
		// 同一无遮挡帧提供目标格，长按后的帧只用于确认操作菜单出现。
	}
	source := maa.Rect{400, 300, 30, 30}
	destination := maa.Rect{804, 191, 86, 86}

	if !runRepoToBagADB(runner, source, destination, time.Second) {
		t.Fatal("runRepoToBagADB returned false")
	}
	want := []string{
		sourceTouchDownNode,
		"screenshot",
		"__InventoryTransferStackButtonLeft",
		"move",
		"wait_before_release_source",
		"release_source",
	}
	if !reflect.DeepEqual(runner.events, want) {
		t.Fatalf("events = %v, want %v", runner.events, want)
	}
	if runner.waits[len(runner.waits)-1] != dragTargetHold {
		t.Fatalf("destination hold = %v, want %v", runner.waits[len(runner.waits)-1], dragTargetHold)
	}
}

func TestRepoToBagDesktopUsesBothRecognizedBoxes(t *testing.T) {
	runner := &fakeDragRunner{}
	if !runRepoToBagDesktop(runner, maa.Rect{400, 300, 30, 30}, maa.Rect{804, 191, 86, 86}) {
		t.Fatal("runRepoToBagDesktop returned false")
	}
	if !reflect.DeepEqual(runner.events, []string{"swipe"}) {
		t.Fatalf("events = %v, want [swipe]", runner.events)
	}
}

func TestRectCenter(t *testing.T) {
	x, y := rectCenter(maa.Rect{804, 191, 86, 86})
	if x != 847 || y != 234 {
		t.Fatalf("point = [%d %d], want [847 234]", x, y)
	}
}

func TestFindCombinedRecognitionBox(t *testing.T) {
	// 夹具与绑定层 parseCombinedResult 的真实输出一致：And 子项没有 Hit 字段。
	detail := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			{
				Name: "StashBackpackFindCurrentItemInRepo",
				Box:  maa.Rect{100, 200, 86, 86},
			},
			{
				Name: "StashBackpackFindCurrentItemInBag",
				Box:  maa.Rect{804, 191, 86, 86},
			},
		},
	}
	if got, ok := findCombinedRecognitionBox(detail, moveRepoToBagTarget); !ok || got != (maa.Rect{804, 191, 86, 86}) {
		t.Fatalf("target box = %v, ok = %v", got, ok)
	}
}
