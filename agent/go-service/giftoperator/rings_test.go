package giftoperator

import (
	"reflect"
	"testing"
)

func TestDailyRingTargetCountsWithoutIntegerTrustIncrease(t *testing.T) {
	for _, test := range []struct{ before, target, after int }{{0, 1, 1}, {1, 2, 2}, {2, 3, 3}} {
		s := session{TargetRings: test.target, Remaining: 2}
		if err := s.reserve("Perlica"); err != nil {
			t.Fatal(err)
		}
		if err := s.observeBefore(101, test.before); err != nil {
			t.Fatal(err)
		}
		if committed, err := s.commitAfter(101, test.after); err != nil || !committed || s.Remaining != 1 {
			t.Fatalf("ring target not counted without integer trust gain: test=%+v state=%+v error=%v", test, s, err)
		}
		if committed, err := s.commitAfter(101, test.after); err != nil || committed || s.Remaining != 1 {
			t.Fatalf("duplicate ring result consumed count: state=%+v error=%v", s, err)
		}
	}
}

func TestUnmetDailyTargetDoesNotCountTrustGain(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 1, Pending: "Perlica"}
	if err := s.observeBefore(101, 0); err != nil {
		t.Fatal(err)
	}
	for _, rings := range []int{0, 1, 2} {
		if committed, err := s.commitAfter(110, rings); err == nil || committed || s.Remaining != 1 || len(s.Completed) != 0 {
			t.Fatalf("below-target trust gain consumed count: rings=%d state=%+v error=%v", rings, s, err)
		}
	}
}

func TestAlreadyReachedDailyTargetIsExcludedWithoutCounting(t *testing.T) {
	for _, rings := range []int{1, 2, 3} {
		s := session{TargetRings: rings, Remaining: 1, Pending: "Perlica"}
		if err := s.observeBefore(101, rings); err != nil || s.Prepared {
			t.Fatalf("already-met target was prepared: state=%+v error=%v", s, err)
		}
		if err := s.reject(); err != nil || !containsOperator(s.Excluded, "Perlica") || s.Remaining != 1 {
			t.Fatalf("already-met target was not excluded: state=%+v error=%v", s, err)
		}
	}
}

func TestInitReadsIndependentRingTargetAndResetsSelection(t *testing.T) {
	for _, target := range []int{1, 2, 3} {
		store := newSessionStore()
		store.nodes[sessionNode]["attach"].(map[string]any)["target_rings"] = target
		count := 4
		if err := updateSession(store, sessionActionParam{Operation: "init", Count: &count}, nil); err != nil {
			t.Fatal(err)
		}
		s, err := loadSession(store)
		if err != nil || s.TargetRings != target || s.Remaining != count || s.SelectedCount != 0 || s.BeforeRings != 0 {
			t.Fatalf("ring option overwrote recipient count: state=%+v error=%v", s, err)
		}
	}
	store := newSessionStore()
	if err := initSession(store, 1); err != nil {
		t.Fatal(err)
	}
	s, err := loadSession(store)
	if err != nil || s.TargetRings != 3 {
		t.Fatalf("default daily target is not three: state=%+v error=%v", s, err)
	}
	for _, invalid := range []any{0, 4, -1, "2", 1.5} {
		store := newSessionStore()
		store.nodes[sessionNode]["attach"].(map[string]any)["target_rings"] = invalid
		if err := initSession(store, 1); err == nil {
			t.Fatalf("invalid daily target accepted: %v", invalid)
		}
	}
}

func TestPreviewSelectionCannotCommitRecipient(t *testing.T) {
	store := newSessionStore()
	s := session{TargetRings: 1, Remaining: 1, Pending: "Perlica"}
	if err := s.observeBefore(101, 0); err != nil {
		t.Fatal(err)
	}
	status := observation(101, 1)
	status.Preview = true
	count := 28
	status.SelectedCount = &count
	if err := applyGiftObservation(store, &s, "observe_selection", status); err != nil || s.Remaining != 1 || len(s.Completed) != 0 {
		t.Fatalf("preview counted recipient: state=%+v error=%v", s, err)
	}
	if store.nodes["GiftOperatorSendSelectionReady"]["enabled"] != true || store.nodes["GiftOperatorSendSelectionMore"]["enabled"] != false {
		t.Fatal("target preview did not enable confirmation")
	}
	if err := applyGiftObservation(store, &s, "observe_after", status); err == nil || s.Remaining != 1 {
		t.Fatal("preview was accepted as real gift confirmation")
	}
	status.Preview = false
	if err := applyGiftObservation(store, &s, "observe_after", status); err != nil || s.Remaining != 0 {
		t.Fatalf("real target observation did not count: state=%+v error=%v", s, err)
	}
}

func TestSelectionMustGrowAndDoesNotMutateBaseline(t *testing.T) {
	s := session{TargetRings: 2, Remaining: 1, Pending: "Perlica"}
	if err := s.observeBefore(101, 0); err != nil {
		t.Fatal(err)
	}
	if ready, err := s.observeSelection(101, 1, 28); err != nil || ready {
		t.Fatalf("one ring prematurely ready: ready=%v error=%v", ready, err)
	}
	before := s
	for _, count := range []int{0, 1, 28} {
		if ready, err := s.observeSelection(101, 2, count); err == nil || ready || !reflect.DeepEqual(s, before) {
			t.Fatalf("unchanged or decreased quantity was accepted: count=%d state=%+v error=%v", count, s, err)
		}
	}
	if ready, err := s.observeSelection(101, 2, 29); err != nil || !ready || s.BeforeRings != 0 || s.BeforeTrust != 101 || s.Remaining != 1 {
		t.Fatalf("grown cumulative preview corrupted baseline: state=%+v error=%v", s, err)
	}
}

func TestMaxTrustCanCompleteBeforeDailyRingTarget(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 1, Pending: "Perlica"}
	if err := s.observeBefore(199, 0); err != nil {
		t.Fatal(err)
	}
	status := observation(200, 0)
	status.CompletedRings = nil // Max-trust UI may omit its remaining-gain label.
	if err := applyGiftObservation(newSessionStore(), &s, "observe_after", status); err != nil || s.Remaining != 0 {
		t.Fatalf("real max trust did not complete: state=%+v error=%v", s, err)
	}
}

func TestRingObservationKeepsIdentityGuard(t *testing.T) {
	s := session{TargetRings: 1, Remaining: 1, Pending: "portrait-1", Generic: true, Prepared: true,
		BeforeTrust: 101, Portraits: []portraitEntry{{Operator: "portrait-1", Name: "新干员"}}}
	status := observation(101, 1)
	status.Name = "新干员"
	status.IdentityMatch = false
	before := s
	if err := applyGiftObservation(newSessionStore(), &s, "observe_after", status); err == nil || !reflect.DeepEqual(s, before) {
		t.Fatal("different portrait counted ring progress")
	}
	status.IdentityMatch = true
	status.Name = "另一人"
	if err := applyGiftObservation(newSessionStore(), &s, "observe_after", status); err == nil || !reflect.DeepEqual(s, before) {
		t.Fatal("changed name counted ring progress")
	}
}

func observation(trust, rings int) selectionStatus {
	limited := rings == 3
	return selectionStatus{giftStatus: giftStatus{Trust: &trust, Limited: &limited, CompletedRings: &rings, IdentityMatch: true}}
}
