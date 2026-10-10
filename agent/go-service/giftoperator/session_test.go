package giftoperator

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestSessionCountsDistinctConfirmedRecipients(t *testing.T) {
	s := session{TargetRings: 1, Remaining: 2}
	if err := s.reserve("Perlica"); err != nil {
		t.Fatal(err)
	}
	if err := s.observeBefore(101, 0); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.commitAfter(101, 0); err == nil || changed || s.Remaining != 2 {
		t.Fatalf("unchanged result consumed recipient: changed=%v error=%v session=%+v", changed, err, s)
	}
	if changed, err := s.commitAfter(102, 1); err != nil || !changed || s.Remaining != 1 {
		t.Fatalf("target ring completion was not counted: changed=%v error=%v session=%+v", changed, err, s)
	}
	if changed, err := s.commitAfter(102, 1); err != nil || changed || s.Remaining != 1 {
		t.Fatalf("duplicate confirmation consumed recipient: changed=%v error=%v session=%+v", changed, err, s)
	}
	if err := s.reserve("Perlica"); err == nil {
		t.Fatal("completed operator was selected again")
	}
	if err := s.reserve("Yvonne"); err != nil {
		t.Fatal(err)
	}
	if err := s.observeBefore(199, 0); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.commitAfter(199, 3); err != nil || !changed || s.Remaining != 0 {
		t.Fatalf("new daily limit was not counted: changed=%v error=%v session=%+v", changed, err, s)
	}
	if err := s.reserve("ChenQianyu"); err == nil {
		t.Fatal("recipient selected after requested count reached zero")
	}
}

func TestAlreadyFullRecipientDoesNotConsumeCount(t *testing.T) {
	for _, initial := range []struct {
		trust int
		rings int
	}{{200, 0}, {101, 3}} {
		s := session{TargetRings: 3, Remaining: 1}
		if err := s.reserve("Perlica"); err != nil {
			t.Fatal(err)
		}
		if err := s.observeBefore(initial.trust, initial.rings); err != nil {
			t.Fatal(err)
		}
		if changed, err := s.commitAfter(200, 3); err == nil || changed || s.Remaining != 1 {
			t.Fatalf("already-full recipient counted as success: changed=%v error=%v session=%+v", changed, err, s)
		}
		if err := s.reject(); err != nil {
			t.Fatal(err)
		}
		if err := s.reject(); err != nil || !reflect.DeepEqual(s.Excluded, []string{"Perlica"}) {
			t.Fatalf("reject must be idempotent: error=%v excluded=%v", err, s.Excluded)
		}
		if err := s.reserve("Perlica"); err == nil {
			t.Fatal("excluded recipient was selected again")
		}
	}
}

func TestTrustDecreaseDoesNotConfirmGiftDespiteDailyLimit(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 1}
	if err := s.reserve("Perlica"); err != nil {
		t.Fatal(err)
	}
	if err := s.observeBefore(101, 0); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.commitAfter(100, 3); err == nil || changed || s.Remaining != 1 || len(s.Completed) != 0 {
		t.Fatalf("decreased trust counted as success: changed=%v error=%v session=%+v", changed, err, s)
	}
}

func TestSessionFinishRequiresRequestedRecipientCount(t *testing.T) {
	store := newSessionStore()
	s := session{TargetRings: 3, Remaining: 2, Completed: []string{"Perlica"}, Excluded: []string{"Yvonne"}}
	if err := saveSession(store, s); err != nil {
		t.Fatal(err)
	}
	if err := updateSession(store, sessionActionParam{Operation: "finish"}, nil); err == nil {
		t.Fatal("exhausted candidates reported success while requested recipients remained")
	}
	unchanged, err := loadSession(store)
	if err != nil || !reflect.DeepEqual(unchanged, s) {
		t.Fatalf("finish mutated incomplete results: session=%+v error=%v", unchanged, err)
	}
	s.Remaining = 0
	if err := saveSession(store, s); err != nil {
		t.Fatal(err)
	}
	if err := updateSession(store, sessionActionParam{Operation: "finish"}, nil); err != nil {
		t.Fatalf("completed session rejected: %v", err)
	}
	if store.nodes["GiftOperatorSendContinue"]["enabled"] != false {
		t.Fatal("completed session still enabled another recipient")
	}
}

func TestSessionInitResetsOnlyCurrentContext(t *testing.T) {
	first, second := newSessionStore(), newSessionStore()
	for _, store := range []*sessionStore{first, second} {
		if err := initSession(store, 1); err != nil {
			t.Fatal(err)
		}
	}
	finished := session{TargetRings: 3, Completed: []string{"Perlica"}, Excluded: []string{"Yvonne"}, Pending: "Perlica"}
	if err := saveSession(first, finished); err != nil {
		t.Fatal(err)
	}
	if first.nodes["GiftOperatorSendContinue"]["enabled"] != false {
		t.Fatal("continue must be disabled at zero remaining")
	}
	other, err := loadSession(second)
	if err != nil || other.Remaining != 1 || len(other.Completed) != 0 {
		t.Fatalf("contexts shared state: session=%+v error=%v", other, err)
	}
	count := 3
	if err := initSession(first, count); err != nil {
		t.Fatal(err)
	}
	reset, err := loadSession(first)
	if err != nil || reset.Remaining != 3 || len(reset.Completed) != 0 || len(reset.Excluded) != 0 || reset.Pending != "" || reset.Prepared {
		t.Fatalf("init retained previous task state: session=%+v error=%v", reset, err)
	}
	if first.nodes["GiftOperatorSendContinue"]["enabled"] != true {
		t.Fatal("continue must be enabled when recipients remain")
	}
	attach := first.nodes[sessionNode]["attach"].(map[string]any)
	if attach["unrelated"] != "preserved" {
		t.Fatal("session update overwrote unrelated attach data")
	}
	for _, invalid := range []int{0, -1} {
		if err := initSession(first, invalid); err == nil {
			t.Fatalf("accepted invalid recipient count %d", invalid)
		}
	}
}

type sessionStore struct {
	nodes map[string]map[string]any
}

func newSessionStore() *sessionStore {
	return &sessionStore{nodes: map[string]map[string]any{
		sessionNode: {"attach": map[string]any{"unrelated": "preserved"}},
	}}
}

func (s *sessionStore) GetNodeJSON(name string) (string, error) {
	node, ok := s.nodes[name]
	if !ok {
		return "", fmt.Errorf("unknown node %q", name)
	}
	raw, err := json.Marshal(node)
	return string(raw), err
}

func (s *sessionStore) OverridePipeline(value any) error {
	overrides, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid override type %T", value)
	}
	for name, value := range overrides {
		fields := value.(map[string]any)
		if s.nodes[name] == nil {
			s.nodes[name] = map[string]any{}
		}
		for key, value := range fields {
			s.nodes[name][key] = value
		}
	}
	return nil
}
