package autoalt

import (
	"errors"
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type modifierActionCall struct {
	node string
	box  maa.Rect
}

type fakePipelineActionRunner struct {
	calls      []modifierActionCall
	failNode   string
	failedNode string
}

func (r *fakePipelineActionRunner) RunAction(
	entry string,
	box maa.Rect,
	_ string,
	_ ...any,
) (*maa.ActionDetail, error) {
	r.calls = append(r.calls, modifierActionCall{node: entry, box: box})
	if entry == r.failNode {
		return nil, errors.New("action failed")
	}
	if entry == r.failedNode {
		return &maa.ActionDetail{Success: false}, nil
	}
	return &maa.ActionDetail{Success: true}, nil
}

func TestRunModifierClickActionsAlwaysReleasesModifier(t *testing.T) {
	t.Parallel()

	const (
		keyDownNode = "KeyDown"
		clickNode   = "Click"
		keyUpNode   = "KeyUp"
	)
	target := maa.Rect{10, 20, 30, 40}
	wantSuccessCalls := []modifierActionCall{
		{node: keyDownNode, box: maa.Rect{}},
		{node: clickNode, box: target},
		{node: keyUpNode, box: maa.Rect{}},
	}

	tests := []struct {
		name       string
		failNode   string
		failedNode string
		want       bool
		calls      []modifierActionCall
	}{
		{name: "success", want: true, calls: wantSuccessCalls},
		{
			name:     "key down error still releases modifier",
			failNode: keyDownNode,
			calls: []modifierActionCall{
				{node: keyDownNode, box: maa.Rect{}},
				{node: keyUpNode, box: maa.Rect{}},
			},
		},
		{
			name:       "unsuccessful key down still releases modifier",
			failedNode: keyDownNode,
			calls: []modifierActionCall{
				{node: keyDownNode, box: maa.Rect{}},
				{node: keyUpNode, box: maa.Rect{}},
			},
		},
		{name: "click error still releases modifier", failNode: clickNode, calls: wantSuccessCalls},
		{name: "unsuccessful click still releases modifier", failedNode: clickNode, calls: wantSuccessCalls},
		{name: "key up error fails action", failNode: keyUpNode, calls: wantSuccessCalls},
		{name: "unsuccessful key up fails action", failedNode: keyUpNode, calls: wantSuccessCalls},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakePipelineActionRunner{
				failNode:   test.failNode,
				failedNode: test.failedNode,
			}
			if got := runModifierClickActions(
				runner,
				"TestModifierClickAction",
				keyDownNode,
				clickNode,
				keyUpNode,
				target,
			); got != test.want {
				t.Fatalf("runModifierClickActions() = %v, want %v", got, test.want)
			}
			if !reflect.DeepEqual(runner.calls, test.calls) {
				t.Fatalf("calls = %#v, want %#v", runner.calls, test.calls)
			}
		})
	}
}
