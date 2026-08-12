package itemtransfer

import (
	"errors"
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type fakeDirectActionRunner struct {
	calls    []maa.ActionType
	failType maa.ActionType
}

func (r *fakeDirectActionRunner) RunActionDirect(
	actionType maa.ActionType,
	_ maa.ActionParam,
	_ maa.Rect,
	_ *maa.RecognitionDetail,
) (*maa.ActionDetail, error) {
	r.calls = append(r.calls, actionType)
	if actionType == r.failType {
		return nil, errors.New("action failed")
	}
	return &maa.ActionDetail{Success: true}, nil
}

func TestRunCtrlClickActionsAlwaysReleasesCtrl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		failType maa.ActionType
		want     bool
		calls    []maa.ActionType
	}{
		{
			name:  "success",
			want:  true,
			calls: []maa.ActionType{maa.ActionTypeKeyDown, maa.ActionTypeClick, maa.ActionTypeKeyUp},
		},
		{
			name:     "key down failure still releases ctrl",
			failType: maa.ActionTypeKeyDown,
			calls:    []maa.ActionType{maa.ActionTypeKeyDown, maa.ActionTypeKeyUp},
		},
		{
			name:     "click failure still releases ctrl",
			failType: maa.ActionTypeClick,
			calls:    []maa.ActionType{maa.ActionTypeKeyDown, maa.ActionTypeClick, maa.ActionTypeKeyUp},
		},
		{
			name:     "key up failure fails the action",
			failType: maa.ActionTypeKeyUp,
			calls:    []maa.ActionType{maa.ActionTypeKeyDown, maa.ActionTypeClick, maa.ActionTypeKeyUp},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeDirectActionRunner{failType: test.failType}
			if got := runCtrlClickActions(runner, maa.Rect{10, 20, 30, 40}); got != test.want {
				t.Fatalf("runCtrlClickActions() = %v, want %v", got, test.want)
			}
			if !reflect.DeepEqual(runner.calls, test.calls) {
				t.Fatalf("calls = %v, want %v", runner.calls, test.calls)
			}
		})
	}
}
