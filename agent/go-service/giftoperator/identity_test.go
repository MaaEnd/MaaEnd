package giftoperator

import (
	"reflect"
	"testing"
)

func TestUnknownRecipientRequiresVerifiedIdentityBeforeCounting(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 2, Generic: true, Pending: "portrait-1", EncounteredName: "现场对话名",
		Portraits: []portraitEntry{{Operator: "portrait-1", Template: "runtime.png"}}}
	if err := bindGiftIdentity(&s, " 未登记新干员 ", false); err == nil {
		t.Fatal("verification accepted a recipient without an initial identity")
	}
	if err := bindGiftIdentity(&s, " 未登记新干员 ", true); err != nil {
		t.Fatal(err)
	}
	if s.Portraits[0].Name != "未登记新干员" || s.Portraits[0].DialogueName != "现场对话名" {
		t.Fatalf("live identity was not retained: %+v", s.Portraits[0])
	}
	if err := s.observeBefore(101, 0); err != nil {
		t.Fatal(err)
	}
	before := s
	if err := bindGiftIdentity(&s, "另一位干员", false); err == nil {
		t.Fatal("counted a different recipient")
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("failed identity check changed count or identity")
	}
	if err := bindGiftIdentity(&s, "未登记新干员", false); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.commitAfter(110, 3); err != nil || !ok || s.Remaining != 1 {
		t.Fatalf("unknown verified recipient was not counted: success=%v err=%v state=%+v", ok, err, s)
	}
	if !blockedDialogueName(s, "现场对话名") || !blockedDialogueName(s, "未登记新干员") {
		t.Fatal("completed recipient can still be chosen by its live name")
	}
	if err := s.reserve("portrait-1"); err == nil {
		t.Fatal("completed portrait was reserved again")
	}
}

func TestWrongDialogueDoesNotExcludeReservedPortrait(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 2, Generic: true, Pending: "portrait-2", AvoidNames: []string{"旧干员"},
		Portraits: []portraitEntry{{Operator: "portrait-1", Name: "已送礼干员", DialogueName: "已送礼对话名"},
			{Operator: "portrait-2", Template: "runtime.png"}}, Completed: []string{"portrait-1"}}
	for _, name := range []string{"旧干员", "已送礼干员", "已送礼对话名"} {
		if !blockedDialogueName(s, name) {
			t.Fatalf("previous dialogue %q was not blocked", name)
		}
	}
	if blockedDialogueName(s, "待召集新干员") {
		t.Fatal("reserved unknown recipient was blocked")
	}
	if s.Remaining != 2 || containsOperator(s.Excluded, "portrait-2") || containsOperator(s.Completed, "portrait-2") {
		t.Fatal("wrong dialogue excluded or counted the reserved recipient")
	}
	if err := s.reserve("portrait-3"); err != nil {
		t.Fatal(err)
	}
	if s.EncounteredName != "" || len(s.AvoidNames) != 0 || s.Generic {
		t.Fatal("per-recipient dialogue exclusions leaked into the next round")
	}
	if !blockedDialogueName(s, "已送礼干员") {
		t.Fatal("completed identity was forgotten")
	}
}

func TestUnknownAlreadyFullRecipientIsExcludedWithoutCounting(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 1, Generic: true, Pending: "portrait-1", EncounteredName: "新干员对话",
		Portraits: []portraitEntry{{Operator: "portrait-1", Template: "runtime.png"}}}
	if err := bindGiftIdentity(&s, "新干员", true); err != nil {
		t.Fatal(err)
	}
	if err := s.observeBefore(101, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.reject(); err != nil {
		t.Fatal(err)
	}
	if s.Remaining != 1 || len(s.Completed) != 0 || !containsOperator(s.Excluded, "portrait-1") {
		t.Fatalf("already-full unknown recipient changed success count: %+v", s)
	}
	if !blockedDialogueName(s, "新干员对话") {
		t.Fatal("excluded live dialogue name was forgotten")
	}
}

func TestLiveGiftNameDeduplicatesChangedPortraitIdentity(t *testing.T) {
	s := session{TargetRings: 3, Remaining: 1, Pending: "portrait-2", Completed: []string{"portrait-1"},
		Portraits: []portraitEntry{{Operator: "portrait-1", Name: "新干员"}, {Operator: "portrait-2", Template: "new-crop.png"}}}
	if !processedGiftName(s, " 新干员 ") {
		t.Fatal("already-completed UI name was treated as a new person")
	}
	if processedGiftName(s, "另一名新干员") {
		t.Fatal("a distinct live name was excluded")
	}
	s.Pending = "portrait-1"
	if processedGiftName(s, "新干员") {
		t.Fatal("the same reservation was rejected during verification")
	}
}
