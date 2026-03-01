package autofight

import (
	"testing"
	"time"
)

func TestScheduler_ProcessEvent_SwitchOperator(t *testing.T) {
	s := &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventSwitchOperator, OperatorIndex: 2},
		},
	}

	actions := s.processEvent(s.config.Events[0], 1, nil)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSwitchOperator || actions[0].Operator != 2 {
		t.Errorf("expected SwitchOperator 2, got %v", actions[0])
	}
}

func TestScheduler_ProcessEvent_Skill(t *testing.T) {
	s := &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventSkill, OperatorIndex: 1},
		},
	}

	// 技力充足
	actions := s.processEvent(s.config.Events[0], 1, nil)
	if len(actions) != 1 || actions[0].Type != ActionSkill {
		t.Errorf("expected ActionSkill, got %v", actions)
	}
	if s.isPaused {
		t.Errorf("should not pause if energy ok")
	}

	// 重置状态
	s = &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventSkill, OperatorIndex: 1},
		},
	}
	// 技力不足
	actions = s.processEvent(s.config.Events[0], 0, nil)
	if len(actions) != 1 || actions[0].Type != ActionAttack {
		t.Errorf("expected ActionAttack when waiting for energy, got %v", actions)
	}
	if !s.isPaused {
		t.Errorf("expected scheduler to pause when energy not enough")
	}
}

func TestScheduler_ProcessEvent_Link(t *testing.T) {
	s := &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventLink},
		},
	}

	actions := s.processEvent(s.config.Events[0], 1, nil)
	if len(actions) != 0 {
		t.Errorf("expected 0 immediate actions for link event, got %v", actions)
	}
	if s.linkPending != 1 {
		t.Errorf("expected linkPending to be 1")
	}
	if len(s.linkTimeouts) != 1 {
		t.Errorf("expected 1 link timeout")
	}
}

func TestScheduler_ProcessEvent_Ultimate(t *testing.T) {
	s := &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventUltimate, OperatorIndex: 3, SpAtMoment: 2},
		},
	}

	// Condition 1: Ready and SP ok
	actions := s.processEvent(s.config.Events[0], 2, []int{3})
	if len(actions) != 1 || actions[0].Type != ActionEndSkillKeyDown {
		t.Errorf("expected ActionEndSkillKeyDown, got %v", actions)
	}

	// Condition 2: Not ready -> Recovery mode
	s = &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventUltimate, OperatorIndex: 3, SpAtMoment: 2},
		},
	}
	actions = s.processEvent(s.config.Events[0], 2, []int{1}) // 3 is not ready
	if !s.ultimateRecovery {
		t.Errorf("expected ultimateRecovery to be true")
	}
	if s.ultimateWaitSp {
		t.Errorf("expected ultimateWaitSp to be false")
	}
	if s.isPaused != true {
		t.Errorf("expected scheduler to be paused")
	}
	if len(actions) != 1 || actions[0].Type != ActionSwitchOperator || actions[0].Operator != 3 {
		t.Errorf("expected SwitchOperator to 3 to recover uliti, got %v", actions)
	}

	// Condition 3: Ready but SP not enough -> WaitSp mode
	s = &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventUltimate, OperatorIndex: 3, SpAtMoment: 2},
		},
	}
	actions = s.processEvent(s.config.Events[0], 1, []int{3}) // Have 1, need 2
	if s.ultimateRecovery {
		t.Errorf("expected ultimateRecovery to be false")
	}
	if !s.ultimateWaitSp {
		t.Errorf("expected ultimateWaitSp to be true")
	}
	if s.isPaused != true {
		t.Errorf("expected scheduler to be paused")
	}
	if len(actions) != 1 || actions[0].Type != ActionAttack {
		t.Errorf("expected ActionAttack to wait for SP, got %v", actions)
	}
}

func TestScheduler_Tick_LinkExecution(t *testing.T) {
	s := &FightScheduler{}
	s.config = &FightConfig{
		Events: []SchedulerEvent{
			{Type: EventDodge, Time: 100}, // Future event so we don't process it immediately
		},
	}
	s.battleStartTime = time.Now()
	s.started = true

	// Manual setup link pending
	s.linkPending = 1
	s.linkTimeouts = []time.Time{time.Now().Add(10 * time.Second)}

	// Tick with combo available
	res := s.Tick(1, true, nil)

	// Should return Combo + Attack (since future event triggers attack fallback)
	if len(res.Actions) != 2 || res.Actions[0].Type != ActionCombo || res.Actions[1].Type != ActionAttack {
		t.Errorf("expected [ActionCombo, ActionAttack], got %v", res.Actions)
	}
	if s.linkPending != 0 {
		t.Errorf("expected linkPending to be 0")
	}
}
