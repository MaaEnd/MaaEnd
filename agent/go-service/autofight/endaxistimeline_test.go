package autofight

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// encodeTimelineCode 生成与 end-axis "复制数据码" 相同的分享字符串：gzip 压缩 + base64url。
func encodeTimelineCode(t *testing.T, root map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatalf("marshal timeline: %v", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf.Bytes())
}

func scenarioWithActions(actions []map[string]any) map[string]any {
	return map[string]any{
		"scenarioList": []any{
			map[string]any{
				"id":   "sc1",
				"name": "方案",
				"data": map[string]any{
					"tracks": []any{
						map[string]any{"id": "track_1", "actions": actions},
					},
				},
			},
		},
	}
}

// liinoToggleScenario 构造与 issue #5126 一致的开关型战技场景：
// battleSkill 位于每轮循环开头（startTime=0），带
// "not(liino-vocalist-stance OR liino-cosmovoice-stance)" 条件，
// 动作 hits 会在执行后置位 liino-vocalist-stance。
func liinoToggleScenario() map[string]any {
	return scenarioWithActions([]map[string]any{
		{
			"id":        "liino_battleSkill",
			"type":      "battleSkill",
			"startTime": 0,
			"duration":  42,
			"name":      "战技",
			"requisites": []any{
				map[string]any{
					"id": "liino-battle-skill-no-stance",
					"condition": map[string]any{
						"kind": "not",
						"condition": map[string]any{
							"kind": "or",
							"conditions": []any{
								map[string]any{"kind": "operatorStatus", "status": "liino-vocalist-stance"},
								map[string]any{"kind": "operatorStatus", "status": "liino-cosmovoice-stance"},
							},
						},
					},
					"messageKey": "actionItem.requisiteTitle.liinoUseStanceTermination",
				},
			},
			"hits": []any{
				map[string]any{
					"effects": []any{
						map[string]any{
							"id":       "liino-vocalist-stance",
							"kind":     "status",
							"target":   "self",
							"duration": 3600,
						},
					},
				},
			},
		},
	})
}

func newLiinoTimeline(t *testing.T) *EndAxisTimeline {
	t.Helper()
	tl := NewEndAxisTimeline()
	if !tl.SetTimelineCode(encodeTimelineCode(t, liinoToggleScenario())) {
		t.Fatal("SetTimelineCode failed")
	}
	return tl
}

// 核心回归：开关型战技第 1 轮触发并置位姿态，第 2 轮因 requisites
// 条件不满足被跳过（不再反复开/关姿态），与 end-axis 数据语义一致。
func TestToggleSkillSkippedOnSecondRound(t *testing.T) {
	tl := newLiinoTimeline(t)

	if !tl.SelectScenario(nil, 1, []int{1}, nil, 1, false) {
		t.Fatal("SelectScenario round 1 failed")
	}

	// 第 1 轮：姿态未生效，战技应派发并置位姿态。
	a := tl.FrontAction()
	if a == nil {
		t.Fatal("round 1: expected battleSkill action")
	}
	if a.Type != "skill" || a.ID != "liino_battleSkill" {
		t.Fatalf("round 1: unexpected action %+v", a)
	}
	tl.PopFrontAction()
	if st := tl.statusState(0, "liino-vocalist-stance"); st == nil || st.stacks < 1 {
		t.Fatal("round 1: stance should be active after cast")
	}

	// 第 2 轮：重新选择方案，姿态仍然生效，战技应被跳过且队列为空。
	if !tl.SelectScenario(nil, 1, []int{1}, nil, 1, false) {
		t.Fatal("SelectScenario round 2 failed")
	}
	if a := tl.FrontAction(); a != nil {
		t.Fatalf("round 2: expected skill to be skipped, got %+v", a)
	}
	if name := tl.TakeSkippedActionName(); name != "战技" {
		t.Fatalf("round 2: skipped name = %q, want 战技", name)
	}
	if tl.TakeSkippedActionName() != "" {
		t.Fatal("skipped name should be cleared after read")
	}
	if len(tl.queue) != 0 {
		t.Fatalf("round 2: queue should be empty after skip, got %d", len(tl.queue))
	}

	// 回归：姿态在持续时间内（60 秒）连续多轮都保持，不会在每轮循环开头
	// 被反复触发导致姿态开/关振荡；到期后（>60 秒）才允许再次施法，由
	// TestStatusExpiresAfterDuration 覆盖。
	for round := 3; round <= 5; round++ {
		if !tl.SelectScenario(nil, 1, []int{1}, nil, 1, false) {
			t.Fatalf("SelectScenario round %d failed", round)
		}
		if a := tl.FrontAction(); a != nil {
			t.Fatalf("round %d: expected skill to be skipped, got %+v", round, a)
		}
		if name := tl.TakeSkippedActionName(); name != "战技" {
			t.Fatalf("round %d: skipped name = %q, want 战技", round, name)
		}
		if st := tl.statusState(0, "liino-vocalist-stance"); st == nil || st.stacks < 1 {
			t.Fatalf("round %d: stance should still be active (persist until consumed)", round)
		}
	}
}

// 无 requisites 的动作保持原有行为：每轮都照常派发。
func TestNoRequisitesActionAlwaysDispatched(t *testing.T) {
	root := scenarioWithActions([]map[string]any{
		{"id": "op1_skill", "type": "skill", "startTime": 0, "duration": 10, "name": "战技"},
	})
	tl := NewEndAxisTimeline()
	if !tl.SetTimelineCode(encodeTimelineCode(t, root)) {
		t.Fatal("SetTimelineCode failed")
	}
	if !tl.SelectScenario(nil, 1, []int{1}, nil, 1, false) {
		t.Fatal("SelectScenario failed")
	}
	a := tl.FrontAction()
	if a == nil || a.ID != "op1_skill" {
		t.Fatalf("expected skill to be dispatched, got %+v", a)
	}
	tl.PopFrontAction()
	if tl.TakeSkippedActionName() != "" {
		t.Fatal("no requisites: nothing should be skipped")
	}
}

// consume 效果会清除对应状态，供后续轮次重新满足条件。
func TestConsumeEffectClearsStatus(t *testing.T) {
	root := scenarioWithActions([]map[string]any{
		{
			"id":        "op1_battleSkill",
			"type":      "battleSkill",
			"startTime": 0,
			"duration":  42,
			"name":      "战技",
			"requisites": []any{
				map[string]any{
					"id": "no-stance",
					"condition": map[string]any{
						"kind": "not",
						"condition": map[string]any{
							"kind": "or",
							"conditions": []any{
								map[string]any{"kind": "operatorStatus", "status": "op1-stance"},
								map[string]any{"kind": "operatorStatus", "status": "op1-stance-2"},
							},
						},
					},
				},
			},
			"hits": []any{
				map[string]any{
					"effects": []any{
						map[string]any{"id": "op1-stance", "kind": "status", "target": "self", "duration": 3600},
					},
				},
			},
		},
		{
			"id":        "op1_skill2",
			"type":      "skill",
			"startTime": 100,
			"duration":  10,
			"name":      "战技2",
			"hits": []any{
				map[string]any{
					"effects": []any{
						map[string]any{
							"kind":           "consume",
							"operatorStatus": []any{"op1-stance"},
							"consumeTarget":  "team",
						},
					},
				},
			},
		},
	})
	tl := NewEndAxisTimeline()
	if !tl.SetTimelineCode(encodeTimelineCode(t, root)) {
		t.Fatal("SetTimelineCode failed")
	}
	if !tl.SelectScenario(nil, 1, []int{1}, nil, 1, false) {
		t.Fatal("SelectScenario failed")
	}

	a := tl.FrontAction()
	if a == nil || a.ID != "op1_battleSkill" {
		t.Fatalf("expected battleSkill first, got %+v", a)
	}
	tl.PopFrontAction()
	if st := tl.statusState(0, "op1-stance"); st == nil || st.stacks < 1 {
		t.Fatal("stance should be active after battleSkill")
	}

	a = tl.FrontAction()
	if a == nil || a.ID != "op1_skill2" {
		t.Fatalf("expected second skill, got %+v", a)
	}
	tl.PopFrontAction()
	if st := tl.statusState(0, "op1-stance"); st == nil || st.stacks != 0 {
		t.Fatal("stance should be consumed after second skill")
	}
}

// 旧数据只有 payload.hits 时，状态效果仍应被解析并应用。
func TestStatusEffectsParsedFromPayloadHits(t *testing.T) {
	root := scenarioWithActions([]map[string]any{
		{
			"id":        "op1_battleSkill",
			"type":      "battleSkill",
			"startTime": 0,
			"duration":  42,
			"name":      "战技",
			"requisites": []any{
				map[string]any{
					"id": "no-stance",
					"condition": map[string]any{
						"kind": "not",
						"condition": map[string]any{
							"kind": "or",
							"conditions": []any{
								map[string]any{"kind": "operatorStatus", "status": "op1-stance"},
								map[string]any{"kind": "operatorStatus", "status": "op1-stance-2"},
							},
						},
					},
				},
			},
			// 只有 payload.hits，没有顶层 hits。
			"payload": map[string]any{
				"hits": []any{
					map[string]any{
						"effects": []any{
							map[string]any{"id": "op1-stance", "kind": "status", "target": "self", "duration": 3600},
						},
					},
				},
			},
		},
	})
	tl := NewEndAxisTimeline()
	if !tl.SetTimelineCode(encodeTimelineCode(t, root)) {
		t.Fatal("SetTimelineCode failed")
	}
	if !tl.SelectScenario(nil, 1, []int{1}, nil, 1, false) {
		t.Fatal("SelectScenario failed")
	}
	a := tl.FrontAction()
	if a == nil {
		t.Fatal("expected battleSkill")
	}
	tl.PopFrontAction()
	if st := tl.statusState(0, "op1-stance"); st == nil || st.stacks < 1 {
		t.Fatal("stance should be active after cast (parsed from payload.hits)")
	}
}

// 条件树三值求值：operatorStatus / not / or / and / 数组 / 未知种类。
func TestConditionTreeEvaluation(t *testing.T) {
	tl := &EndAxisTimeline{
		trackedStatuses: map[string]struct{}{"stance-a": {}, "stance-b": {}},
		statuses: map[int]map[string]*timelineStatusState{
			0: {},
		},
	}

	notStance := json.RawMessage(`{"kind":"not","condition":{"kind":"or","conditions":[{"kind":"operatorStatus","status":"stance-a"},{"kind":"operatorStatus","status":"stance-b"}]}}`)
	if !tl.conditionMet(notStance, 0) {
		t.Fatal("no stance active: not(or) should be satisfied")
	}
	tl.setStatus(0, "stance-a", 0, 0)
	if tl.conditionMet(notStance, 0) {
		t.Fatal("stance-a active: not(or) should be unsatisfied")
	}

	// or：任一满足即满足。
	either := json.RawMessage(`{"kind":"or","conditions":[{"kind":"operatorStatus","status":"stance-a"},{"kind":"operatorStatus","status":"stance-b"}]}`)
	if !tl.conditionMet(either, 0) {
		t.Fatal("stance-a active: or should be satisfied")
	}
	tl.clearStatus(0, "stance-a")
	if tl.conditionMet(either, 0) {
		t.Fatal("no stance active: or should be unsatisfied")
	}

	// and / 数组：全部满足才满足。
	andCond := json.RawMessage(`{"kind":"and","conditions":[{"kind":"operatorStatus","status":"stance-a"},{"kind":"operatorStatus","status":"stance-b"}]}`)
	tl.setStatus(0, "stance-a", 0, 0)
	if tl.conditionMet(andCond, 0) {
		t.Fatal("stance-b absent: and should be unsatisfied")
	}
	tl.setStatus(0, "stance-b", 0, 0)
	if !tl.conditionMet(andCond, 0) {
		t.Fatal("both present: and should be satisfied")
	}
	arrCond := json.RawMessage(`[{"kind":"operatorStatus","status":"stance-a"},{"kind":"operatorStatus","status":"stance-b"}]`)
	if !tl.conditionMet(arrCond, 0) {
		t.Fatal("array condition with both present should be satisfied")
	}
}

// 无法求值的条件种类（cooldown / enemy / enhancement 等）不应导致动作被误跳过，
// 包括出现在 not 内部时（如 arcane 的 or(not(ultimateEnhancement), ...)）。
func TestUnknownConditionKindsFailOpen(t *testing.T) {
	tl := &EndAxisTimeline{
		trackedStatuses: map[string]struct{}{"arcana-ready": {}},
		statuses: map[int]map[string]*timelineStatusState{
			0: {},
		},
	}

	// 纯未知条件：放行。
	if !tl.conditionMet(json.RawMessage(`{"kind":"skillCooldownReady","cooldownKey":"liino-battle-skill"}`), 0) {
		t.Fatal("skillCooldownReady should not block")
	}
	if !tl.conditionMet(json.RawMessage(`{"kind":"ultimateCooldownReady"}`), 0) {
		t.Fatal("ultimateCooldownReady should not block")
	}

	// 未知条件经 not 取反后仍为未知，顶层未知放行（对应 arcane 的
	// or(not(ultimateEnhancement), operatorStatus arcana-ready)）。
	arcaneLike := json.RawMessage(`{"kind":"or","conditions":[{"kind":"not","condition":{"kind":"ultimateEnhancement"}},{"kind":"operatorStatus","status":"arcana-ready"}]}`)
	if !tl.conditionMet(arcaneLike, 0) {
		t.Fatal("unknown-enhancement branch should not block the ultimate")
	}

	// 已知不满足的分支仍会阻断：or(false, not-unknown) 整体未知 → 放行；
	// 但如果要求必须有 arcana-ready（单独条件），不满足时应跳过。
	if tl.conditionMet(json.RawMessage(`{"kind":"operatorStatus","status":"arcana-ready"}`), 0) {
		t.Fatal("arcana-ready absent: operatorStatus condition should be unsatisfied")
	}
}

// 层数约束。
func TestConditionStacks(t *testing.T) {
	tl := &EndAxisTimeline{
		trackedStatuses: map[string]struct{}{"stance-a": {}},
		statuses: map[int]map[string]*timelineStatusState{
			0: {"stance-a": {stacks: 2}},
		},
	}

	atLeast2 := json.RawMessage(`{"kind":"operatorStatus","status":"stance-a","stacks":{"compare":"atLeast","count":2}}`)
	if !tl.conditionMet(atLeast2, 0) {
		t.Fatal("stacks=2 should satisfy atLeast 2")
	}
	exact3 := json.RawMessage(`{"kind":"operatorStatus","status":"stance-a","stacks":{"compare":"exact","count":3}}`)
	if tl.conditionMet(exact3, 0) {
		t.Fatal("stacks=2 should not satisfy exact 3")
	}
	atMost1 := json.RawMessage(`{"kind":"operatorStatus","status":"stance-a","stacks":{"compare":"atMost","count":1}}`)
	if tl.conditionMet(atMost1, 0) {
		t.Fatal("stacks=2 should not satisfy atMost 1")
	}
}

// 状态按 duration 过期：到期后视为不存在，requisites 重新允许施法。
// （莉诺演唱姿态持续 60 秒，到期后战技应可再次施放以重新开启姿态。）
func TestStatusExpiresAfterDuration(t *testing.T) {
	tl := &EndAxisTimeline{
		trackedStatuses: map[string]struct{}{"stance-a": {}},
		statuses: map[int]map[string]*timelineStatusState{
			0: {},
		},
	}

	// 置位时带 duration（帧）：3600 帧 = 60 秒。
	tl.setStatus(0, "stance-a", 3600, 0)
	cond := json.RawMessage(`{"kind":"operatorStatus","status":"stance-a"}`)
	if !tl.conditionMet(cond, 0) {
		t.Fatal("status should be active within its duration")
	}

	// 直接把过期时刻拨到过去，模拟 60 秒已过。
	tl.statuses[0]["stance-a"].expiry = time.Now().Add(-time.Second)
	if tl.conditionMet(cond, 0) {
		t.Fatal("expired status should be treated as absent")
	}

	// 过期后重新置位会刷新过期时刻。
	tl.setStatus(0, "stance-a", 3600, 0)
	if !tl.conditionMet(cond, 0) {
		t.Fatal("re-applied status should be active again")
	}

	// consume 可提前清除。
	tl.clearStatus(0, "stance-a")
	if tl.conditionMet(cond, 0) {
		t.Fatal("consumed status should be absent")
	}
}

// 仅被 requisites 引用、但没有任何动作效果管理的状态视为不存在，
// 保证 not(or(...)) 守卫在部分成员未受动作管理时仍能正确求值。
func TestUntrackedStatusTreatedAsAbsent(t *testing.T) {
	tl := &EndAxisTimeline{
		trackedStatuses: map[string]struct{}{"stance-a": {}},
		statuses: map[int]map[string]*timelineStatusState{
			0: {},
		},
	}

	// stance-b 未被跟踪（没有动作管理它），视为不存在。
	guard := json.RawMessage(`{"kind":"not","condition":{"kind":"or","conditions":[{"kind":"operatorStatus","status":"stance-a"},{"kind":"operatorStatus","status":"stance-b"}]}}`)
	if !tl.conditionMet(guard, 0) {
		t.Fatal("all members absent (including untracked): guard should be satisfied")
	}
	tl.setStatus(0, "stance-a", 0, 0)
	if tl.conditionMet(guard, 0) {
		t.Fatal("tracked member present: guard should be unsatisfied")
	}
}
