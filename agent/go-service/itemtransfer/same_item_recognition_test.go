package itemtransfer

import (
	"errors"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type fakeSameItemNodeStore struct {
	nodes map[string]string
	err   error
}

func (s *fakeSameItemNodeStore) GetNodeJSON(nodeName string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.nodes[nodeName], nil
}

func TestRunSameItemRecognition(t *testing.T) {
	t.Parallel()

	validParam := `{"forward_item_node":"ForwardNode","return_item_node":"ReturnNode"}`
	tests := []struct {
		name  string
		store *fakeSameItemNodeStore
		arg   *maa.CustomRecognitionArg
		wants bool
	}{
		{
			name: "same selected item ids",
			store: &fakeSameItemNodeStore{nodes: map[string]string{
				"ForwardNode": itemNodeJSON("item_a"),
				"ReturnNode":  itemNodeJSON("item_a"),
			}},
			arg: &maa.CustomRecognitionArg{
				CustomRecognitionParam: validParam,
				Roi:                    maa.Rect{0, 0, 1, 1},
			},
			wants: true,
		},
		{
			name: "different selected item ids",
			store: &fakeSameItemNodeStore{nodes: map[string]string{
				"ForwardNode": itemNodeJSON("item_a"),
				"ReturnNode":  itemNodeJSON("item_b"),
			}},
			arg: &maa.CustomRecognitionArg{CustomRecognitionParam: validParam},
		},
		{
			name: "missing selected item id",
			store: &fakeSameItemNodeStore{nodes: map[string]string{
				"ForwardNode": itemNodeJSON(),
				"ReturnNode":  itemNodeJSON("item_a"),
			}},
			arg: &maa.CustomRecognitionArg{CustomRecognitionParam: validParam},
		},
		{
			name:  "node lookup failure",
			store: &fakeSameItemNodeStore{err: errors.New("lookup failed")},
			arg:   &maa.CustomRecognitionArg{CustomRecognitionParam: validParam},
		},
		{
			name: "invalid node json",
			store: &fakeSameItemNodeStore{nodes: map[string]string{
				"ForwardNode": `{`,
				"ReturnNode":  itemNodeJSON("item_a"),
			}},
			arg: &maa.CustomRecognitionArg{CustomRecognitionParam: validParam},
		},
		{
			name:  "missing node names",
			store: &fakeSameItemNodeStore{},
			arg:   &maa.CustomRecognitionArg{CustomRecognitionParam: `{}`},
		},
		{
			name:  "invalid recognition param",
			store: &fakeSameItemNodeStore{},
			arg:   &maa.CustomRecognitionArg{CustomRecognitionParam: `{`},
		},
		{
			name:  "nil arg",
			store: &fakeSameItemNodeStore{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, hit := runSameItemRecognition(test.store, test.arg)
			if hit != test.wants {
				t.Fatalf("hit = %v, want %v", hit, test.wants)
			}
			if test.wants && result == nil {
				t.Fatal("matching item ids should return a recognition result")
			}
			if !test.wants && result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
		})
	}
}

func itemNodeJSON(itemIDs ...string) string {
	if len(itemIDs) == 0 {
		return `{"recognition":{"param":{"custom_recognition_param":{"item_ids":[]}}}}`
	}
	return `{"recognition":{"param":{"custom_recognition_param":{"item_ids":["` + itemIDs[0] + `"]}}}}`
}
