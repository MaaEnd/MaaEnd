package giftoperator

import (
	"encoding/json"
	"fmt"
	"strings"
)

const sessionNode = "GiftOperatorSendMain"

// nodeStore keeps bookkeeping in the current Pipeline context, not process state.
type nodeStore interface {
	GetNodeJSON(string) (string, error)
	OverridePipeline(any) error
}

// portraitEntry links a runtime avatar to names read from the current UI.
// Neither name nor template comes from the maintained operator catalog.
type portraitEntry struct {
	Operator     string `json:"operator"`
	Template     string `json:"template"`
	Image        string `json:"image,omitempty"`
	Name         string `json:"name,omitempty"`
	DialogueName string `json:"dialogue_name,omitempty"`
}

type session struct {
	Generic         bool            `json:"generic"`
	Portraits       []portraitEntry `json:"portraits,omitempty"`
	EncounteredName string          `json:"encountered_name,omitempty"`
	AvoidNames      []string        `json:"avoid_names,omitempty"`
	TargetRings     int             `json:"target_rings"`
	BeforeRings     int             `json:"before_rings"`
	SelectedCount   int             `json:"selected_count"`
	Remaining       int             `json:"remaining"`
	Completed       []string        `json:"completed"`
	Excluded        []string        `json:"excluded"`
	Pending         string          `json:"pending"`
	BeforeTrust     int             `json:"before_trust"`
	BeforeLimited   bool            `json:"before_limited"`
	Prepared        bool            `json:"prepared"`
}

func initSession(store nodeStore, count int) error {
	if count <= 0 {
		return fmt.Errorf("count must be a positive integer")
	}
	raw, err := store.GetNodeJSON(sessionNode)
	if err != nil {
		return fmt.Errorf("read gift ring target: %w", err)
	}
	var config struct {
		Attach struct {
			TargetRings *int `json:"target_rings"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("parse gift ring target: %w", err)
	}
	target := 3
	if config.Attach.TargetRings != nil {
		target = *config.Attach.TargetRings
	}
	if target < 1 || target > 3 {
		return fmt.Errorf("target_rings must be between 1 and 3")
	}
	if err := setSelectionReadiness(store, false, false); err != nil {
		return err
	}
	return saveSession(store, session{TargetRings: target, Remaining: count, Completed: []string{}, Excluded: []string{}})
}

func loadSession(store nodeStore) (session, error) {
	raw, err := store.GetNodeJSON(sessionNode)
	if err != nil {
		return session{}, fmt.Errorf("read gift session: %w", err)
	}
	var node struct {
		Attach struct {
			Session *session `json:"gift_session"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return session{}, fmt.Errorf("parse gift session: %w", err)
	}
	if node.Attach.Session == nil {
		return session{}, fmt.Errorf("gift session has not been initialized")
	}
	s := *node.Attach.Session
	if s.Remaining < 0 || s.BeforeTrust < 0 || s.BeforeTrust > 200 ||
		s.TargetRings < 1 || s.TargetRings > 3 || s.BeforeRings < 0 || s.BeforeRings > 3 || s.SelectedCount < 0 {
		return session{}, fmt.Errorf("invalid gift session values")
	}
	return s, nil
}

func saveSession(store nodeStore, s session) error {
	raw, err := store.GetNodeJSON(sessionNode)
	if err != nil {
		return fmt.Errorf("read session node attach: %w", err)
	}
	var node struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return fmt.Errorf("parse session node attach: %w", err)
	}
	if node.Attach == nil {
		node.Attach = map[string]any{}
	}
	node.Attach["gift_session"] = s
	return store.OverridePipeline(map[string]any{
		sessionNode:                map[string]any{"attach": node.Attach},
		"GiftOperatorSendContinue": map[string]any{"enabled": s.Remaining > 0},
	})
}

func (s *session) reserve(operator string) error {
	operator = strings.TrimSpace(operator)
	if operator == "" {
		return fmt.Errorf("operator is required")
	}
	if s.Remaining == 0 {
		return fmt.Errorf("requested gift recipients have been completed")
	}
	if containsOperator(s.Completed, operator) || containsOperator(s.Excluded, operator) {
		return fmt.Errorf("operator %q has already been completed or excluded", operator)
	}
	s.Generic = false
	s.EncounteredName = ""
	s.AvoidNames = nil
	s.Pending = operator
	s.BeforeTrust = 0
	s.BeforeLimited = false
	s.BeforeRings = 0
	s.SelectedCount = 0
	s.Prepared = false
	return nil
}

func (s *session) observeBefore(trust, rings int) error {
	if s.Pending == "" {
		return fmt.Errorf("no operator is reserved")
	}
	if err := validObservation(trust, rings); err != nil {
		return err
	}
	if s.TargetRings < 1 || s.TargetRings > 3 {
		return fmt.Errorf("invalid gift ring target")
	}
	s.BeforeTrust = trust
	s.BeforeRings = rings
	s.BeforeLimited = rings == 3
	s.SelectedCount = 0
	s.Prepared = trust < 200 && rings < s.TargetRings
	return nil
}

// commitAfter counts only a real post-confirmation observation. Daily rings are
// cumulative; a trust increase below the configured daily target is insufficient.
func (s *session) commitAfter(trust, rings int) (bool, error) {
	if err := validObservation(trust, rings); err != nil {
		return false, err
	}
	if s.Pending == "" {
		return false, fmt.Errorf("no operator is reserved")
	}
	if containsOperator(s.Completed, s.Pending) {
		return false, nil
	}
	if !s.Prepared || s.Remaining == 0 {
		return false, fmt.Errorf("operator was not prepared for gifting")
	}
	if trust < s.BeforeTrust || rings < s.BeforeRings {
		return false, fmt.Errorf("gift progress decreased between observations")
	}
	// A recipient reaching total max trust cannot gain further daily progress.
	if trust != 200 && (rings < s.TargetRings || rings <= s.BeforeRings) {
		return false, fmt.Errorf("gift daily ring target was not confirmed")
	}
	s.Completed = append(s.Completed, s.Pending)
	s.Remaining--
	s.Prepared = false
	return true, nil
}

// observeSelection records a strictly growing total across all gift categories.
// Preview progress controls selection only and never consumes a recipient.
func (s *session) observeSelection(trust, rings, count int) (bool, error) {
	if err := validObservation(trust, rings); err != nil {
		return false, err
	}
	if !s.Prepared || s.Pending == "" {
		return false, fmt.Errorf("operator was not prepared for selecting gifts")
	}
	if count <= s.SelectedCount {
		return false, fmt.Errorf("selected gift count has not increased")
	}
	if trust < s.BeforeTrust || rings < s.BeforeRings {
		return false, fmt.Errorf("gift preview progress decreased")
	}
	s.SelectedCount = count
	return trust == 200 || rings >= s.TargetRings, nil
}

func validObservation(trust, rings int) error {
	if trust < 0 || trust > 200 {
		return fmt.Errorf("trust must be between 0 and 200")
	}
	if rings < 0 || rings > 3 {
		return fmt.Errorf("daily rings must be between 0 and 3")
	}
	return nil
}

func (s *session) reject() error {
	if s.Pending == "" {
		return fmt.Errorf("no operator is reserved")
	}
	if !containsOperator(s.Completed, s.Pending) && !containsOperator(s.Excluded, s.Pending) {
		s.Excluded = append(s.Excluded, s.Pending)
	}
	s.Prepared = false
	return nil
}

func containsOperator(operators []string, operator string) bool {
	for _, candidate := range operators {
		if candidate == operator {
			return true
		}
	}
	return false
}
