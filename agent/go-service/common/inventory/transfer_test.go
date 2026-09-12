package inventory

import (
	"errors"
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type actionCall struct {
	node     string
	box      maa.Rect
	override []any
}

type fakeActionRunner struct {
	calls       []actionCall
	failureNode string
	failureMode string
}

func (r *fakeActionRunner) RunAction(node string, box maa.Rect, _ string, override ...any) (*maa.ActionDetail, error) {
	r.calls = append(r.calls, actionCall{node: node, box: box, override: override})
	if node == r.failureNode {
		switch r.failureMode {
		case "error":
			return nil, errors.New("input failed")
		case "nil_detail":
			return nil, nil
		case "unsuccessful":
			return &maa.ActionDetail{Success: false}, nil
		}
	}
	return &maa.ActionDetail{Success: true}, nil
}

func TestTransferActions(t *testing.T) {
	box := maa.Rect{300, 200, 40, 50}
	for mode, touchMode := range map[string]string{"All": "all", "Stack": "stack", "Half": "half"} {
		t.Run(mode, func(t *testing.T) {
			prefix := "__InventoryTransfer" + mode
			component := "InventoryTransfer" + mode + "Action"
			begin := actionCall{node: prefix + "BeginAction", box: box}
			execute := actionCall{node: "__InventoryTransferClickAction", box: box, override: []any{
				map[string]any{
					"__InventoryTransferClickAction": map[string]any{
						"custom_action_param": touchTransferParam{Mode: touchMode},
					},
				},
			}}
			end := actionCall{node: prefix + "EndAction", box: box}
			t.Run("success", func(t *testing.T) {
				runner := &fakeActionRunner{}
				if !runTransferActions(runner, component, prefix, touchMode, box) {
					t.Fatal("transfer should succeed")
				}
				if want := []actionCall{begin, execute, end}; !reflect.DeepEqual(runner.calls, want) {
					t.Fatalf("calls = %v, want %v", runner.calls, want)
				}
			})
			for _, failedStage := range []string{begin.node, execute.node, end.node} {
				for _, failureMode := range []string{"error", "nil_detail", "unsuccessful"} {
					t.Run(failedStage+"/"+failureMode, func(t *testing.T) {
						runner := &fakeActionRunner{failureNode: failedStage, failureMode: failureMode}
						if runTransferActions(runner, component, prefix, touchMode, box) {
							t.Fatal("failed stage must fail the transfer")
						}
						want := []actionCall{begin, execute, end}
						if failedStage == begin.node {
							want = []actionCall{begin, end}
						}
						if !reflect.DeepEqual(runner.calls, want) {
							t.Fatalf("calls = %v, want %v", runner.calls, want)
						}
					})
				}
			}
		})
	}
}

func TestTransferRejectsNilArguments(t *testing.T) {
	for name, action := range map[string]maa.CustomActionRunner{
		"All":   &TransferAllAction{},
		"Stack": &TransferStackAction{},
		"Half":  &TransferHalfAction{},
	} {
		t.Run(name, func(t *testing.T) {
			if action.Run(nil, &maa.CustomActionArg{}) {
				t.Fatal("nil context must fail")
			}
			if action.Run(&maa.Context{}, nil) {
				t.Fatal("nil arg must fail")
			}
		})
	}
}
