package itemtransfer

import (
	"errors"
	"image"
	"testing"
	"time"
)

func TestItemCacheKeyUsesItemNameAndSide(t *testing.T) {
	got, err := itemCacheKey("精选柑实罐头", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Items/精选柑实罐头/repo.png"; got != want {
		t.Fatalf("itemCacheKey()=%q want=%q", got, want)
	}
	got, err = itemCacheKey(`A/B\C%`, "bag")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Items/A%2FB%5CC%25/bag.png"; got != want {
		t.Fatalf("itemCacheKey()=%q want=%q", got, want)
	}
	if _, err := itemCacheKey("A", "unknown"); err == nil {
		t.Fatal("itemCacheKey() error=nil for unknown side")
	}
}

func TestItemCacheSourceRect(t *testing.T) {
	got := itemCacheSourceRect(191, 246)
	want := image.Rect(165, 221, 217, 271)
	if got != want {
		t.Fatalf("itemCacheSourceRect()=%v want=%v", got, want)
	}
}

func TestItemCacheROIOffsetTrimsQuantityArea(t *testing.T) {
	if itemCacheROIOffset != ([4]int{0, 0, 0, -10}) {
		t.Fatalf("itemCacheROIOffset=%v", itemCacheROIOffset)
	}
}

func TestItemCacheNodePatchKeepsRepoAndBagTemplatesIsolated(t *testing.T) {
	const imageName = "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/repo.png"
	repoPatch, err := itemCacheNodePatch(imageName, "A", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(repoPatch) != 2 {
		t.Fatalf("repo patch node count=%d want=2", len(repoPatch))
	}
	for _, node := range []string{itemCacheRepoNode, itemCacheRepoLowConfidenceNode} {
		if _, ok := repoPatch[node]; !ok {
			t.Fatalf("repo patch missing node %q", node)
		}
	}
	for _, node := range []string{itemCacheBagNode, itemCacheBagReturnNode, itemCacheBagLowConfidenceNode, itemCacheBagReturnLowConfidenceNode} {
		if _, ok := repoPatch[node]; ok {
			t.Fatalf("repo patch unexpectedly contains bag node %q", node)
		}
	}

	bagPatch, err := itemCacheNodePatch("__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/bag.png", "A", "bag")
	if err != nil {
		t.Fatal(err)
	}
	if len(bagPatch) != 4 {
		t.Fatalf("bag patch node count=%d want=4", len(bagPatch))
	}
	for _, node := range []string{itemCacheBagNode, itemCacheBagReturnNode, itemCacheBagLowConfidenceNode, itemCacheBagReturnLowConfidenceNode} {
		if _, ok := bagPatch[node]; !ok {
			t.Fatalf("bag patch missing node %q", node)
		}
	}
	for _, node := range []string{itemCacheRepoNode, itemCacheRepoLowConfidenceNode} {
		if _, ok := bagPatch[node]; ok {
			t.Fatalf("bag patch unexpectedly contains repo node %q", node)
		}
	}
	if _, err := itemCacheNodePatch(imageName, "A", "unknown"); err == nil {
		t.Fatal("itemCacheNodePatch() error=nil for unknown side")
	}
}

func TestItemCacheNodePatchConfiguresLowConfidenceRecognition(t *testing.T) {
	const imageName = "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/bag.png"
	got, err := itemCacheNodePatch(imageName, "A", "bag")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		node      string
		cacheNode string
	}{
		{itemCacheBagLowConfidenceNode, itemCacheBagNode},
		{itemCacheBagReturnLowConfidenceNode, itemCacheBagReturnNode},
	}
	for _, tt := range tests {
		patch := got[tt.node]
		if patch["enabled"] != true {
			t.Fatalf("low-confidence node %q enabled=%v", tt.node, patch["enabled"])
		}
		if _, exists := patch["custom_recognition_param"]; exists {
			t.Fatalf("low-confidence node %q contains root custom_recognition_param", tt.node)
		}
		if _, exists := patch["custom_action_param"]; exists {
			t.Fatalf("low-confidence node %q contains root custom_action_param", tt.node)
		}
		recognition, ok := patch["recognition"].(map[string]any)
		if !ok {
			t.Fatalf("low-confidence node %q recognition=%T", tt.node, patch["recognition"])
		}
		recognitionParam, ok := recognition["param"].(map[string]any)
		if !ok {
			t.Fatalf("low-confidence node %q recognition.param=%T", tt.node, recognition["param"])
		}
		param, ok := recognitionParam["custom_recognition_param"].(itemCacheLowConfidenceParam)
		if !ok {
			t.Fatalf("low-confidence node %q recognition param=%T", tt.node, recognitionParam["custom_recognition_param"])
		}
		if param.ItemName != "A" || param.Side != "bag" || param.CacheNode != tt.cacheNode {
			t.Fatalf("low-confidence node %q param=%+v", tt.node, param)
		}
		action, ok := patch["action"].(map[string]any)
		if !ok {
			t.Fatalf("low-confidence node %q action=%T", tt.node, patch["action"])
		}
		actionParamMap, ok := action["param"].(map[string]any)
		if !ok {
			t.Fatalf("low-confidence node %q action.param=%T", tt.node, action["param"])
		}
		if actionParam, ok := actionParamMap["custom_action_param"].(itemCacheLowConfidenceParam); !ok || actionParam != param {
			t.Fatalf("low-confidence node %q action param=%+v", tt.node, actionParamMap["custom_action_param"])
		}
	}
}

func TestItemCacheNodePatchUsesSharedTemplateMatchSettings(t *testing.T) {
	got, err := itemCacheNodePatch("__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A/bag.png", "A", "bag")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []string{itemCacheBagNode, itemCacheBagReturnNode} {
		patch := got[node]
		if patch["method"] != itemCacheMatchMethod || patch["threshold"] != itemCacheDirectThreshold || patch["order_by"] != "Score" {
			t.Fatalf("cache node %q match settings=%v", node, patch)
		}
	}
}

func TestClassifyItemCacheScore(t *testing.T) {
	tests := []struct {
		score float64
		want  itemCacheScoreDecision
	}{
		{0.899999, itemCacheScoreMiss},
		{0.9, itemCacheScoreVerifyOCR},
		{0.969999, itemCacheScoreVerifyOCR},
		{0.97, itemCacheScoreDirect},
		{1, itemCacheScoreDirect},
	}
	for _, tt := range tests {
		if got := classifyItemCacheScore(tt.score); got != tt.want {
			t.Fatalf("classifyItemCacheScore(%v)=%v want=%v", tt.score, got, tt.want)
		}
	}
}

func TestParseItemCacheLowConfidenceParamValidatesSideAndCacheNode(t *testing.T) {
	got, err := parseItemCacheLowConfidenceParam(`{"item_name":"A","side":"repo","cache_node":"ItemTransferFindCachedItemInRepo"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != (itemCacheLowConfidenceParam{ItemName: "A", Side: "repo", CacheNode: itemCacheRepoNode}) {
		t.Fatalf("parseItemCacheLowConfidenceParam()=%+v", got)
	}
	invalid := []string{
		`{}`,
		`{"item_name":"A","side":"unknown","cache_node":"ItemTransferFindCachedItemInRepo"}`,
		`{"item_name":"A","side":"repo","cache_node":"ItemTransferFindCachedItemInBag"}`,
		`{"item_name":"A","side":"bag","cache_node":"ItemTransferFindCachedItemInRepo"}`,
	}
	for _, raw := range invalid {
		if _, err := parseItemCacheLowConfidenceParam(raw); err == nil {
			t.Fatalf("parseItemCacheLowConfidenceParam(%s) error=nil", raw)
		}
	}
}

func TestAcceptLowConfidenceCacheMatchRequiresOCRInVerificationRange(t *testing.T) {
	tests := []struct {
		score float64
		names []string
		want  bool
	}{
		{score: 0.9, names: []string{"A"}, want: true},
		{score: 0.969999, names: []string{"A"}, want: true},
		{score: 0.95, names: []string{"B"}, want: false},
		{score: 0.899999, names: []string{"A"}, want: false},
		{score: 0.97, names: []string{"A"}, want: false},
	}
	for _, tt := range tests {
		if got := acceptLowConfidenceCacheMatch(tt.score, tt.names, "A"); got != tt.want {
			t.Fatalf("acceptLowConfidenceCacheMatch(%v,%v)=%v want=%v", tt.score, tt.names, got, tt.want)
		}
	}
}

func TestRunCacheBeforeTransferContinuesWhenCacheFails(t *testing.T) {
	var calls []string
	got := runCacheBeforeTransfer(
		func() error {
			calls = append(calls, "cache")
			return errors.New("cache failed")
		},
		func() bool {
			calls = append(calls, "transfer")
			return true
		},
	)
	if !got {
		t.Fatal("runCacheBeforeTransfer()=false want=true")
	}
	if len(calls) != 2 || calls[0] != "cache" || calls[1] != "transfer" {
		t.Fatalf("calls=%v want=[cache transfer]", calls)
	}
}

func TestItemCacheWaitConfigUsesSideSpecificROI(t *testing.T) {
	tests := []struct {
		side    string
		wantROI [4]int
	}{
		{side: "repo", wantROI: [4]int{158, 203, 551, 292}},
		{side: "bag", wantROI: [4]int{768, 209, 349, 285}},
	}

	for _, tt := range tests {
		t.Run(tt.side, func(t *testing.T) {
			duration, param, err := itemCacheWaitConfig(tt.side)
			if err != nil {
				t.Fatal(err)
			}
			if duration != 400*time.Millisecond {
				t.Fatalf("duration=%v want=%v", duration, 400*time.Millisecond)
			}
			if param.Method != 5 || param.Threshold != 0.95 {
				t.Fatalf("method=%d threshold=%v", param.Method, param.Threshold)
			}
			if param.RateLimit != 100*time.Millisecond || param.Timeout != 5*time.Second {
				t.Fatalf("rate_limit=%v timeout=%v", param.RateLimit, param.Timeout)
			}
			roi, err := param.Target.AsRect()
			if err != nil {
				t.Fatal(err)
			}
			if roi != tt.wantROI {
				t.Fatalf("roi=%v want=%v", roi, tt.wantROI)
			}
		})
	}
}

func TestItemCacheWaitConfigRejectsUnknownSide(t *testing.T) {
	if _, _, err := itemCacheWaitConfig("unknown"); err == nil {
		t.Fatal("itemCacheWaitConfig() error=nil, want non-nil")
	}
}
