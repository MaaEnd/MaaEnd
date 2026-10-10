package giftoperator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// SessionAction records recipient selection and confirmed gift results.
// Pipeline remains responsible for every UI action and transition.
type SessionAction struct{}

var _ maa.CustomActionRunner = &SessionAction{}

type sessionActionParam struct {
	Operation string `json:"operation"`
	Count     *int   `json:"count,omitempty"`
}

// Run updates the current task's gift session without operating the game UI.
func (a *SessionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "GiftOperatorSessionAction").Msg("nil context or action argument")
		return false
	}
	var param sessionActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", "GiftOperatorSessionAction").Msg("failed to parse action parameters")
		return false
	}
	if err := updateSession(ctx, param, arg.RecognitionDetail); err != nil {
		log.Error().Err(err).Str("component", "GiftOperatorSessionAction").Str("operation", param.Operation).Msg("failed to update gift session")
		return false
	}
	return true
}

func updateSession(store nodeStore, param sessionActionParam, detail *maa.RecognitionDetail) error {
	if param.Operation == "init" {
		count := 1
		if param.Count != nil {
			count = *param.Count
		}
		return initSession(store, count)
	}
	if param.Count != nil {
		return fmt.Errorf("count is only supported for init")
	}
	s, err := loadSession(store)
	if err != nil {
		return err
	}
	switch param.Operation {
	case "reserve":
		var info candidate
		if err := decodeCustomDetail(detail, &info); err != nil {
			return err
		}
		if err := s.reserve(info.Operator); err != nil {
			return err
		}
		if err := setSelectionReadiness(store, false, false); err != nil {
			return err
		}
		s.Generic = info.Template != ""
		if s.Generic {
			if info.Portrait == "" {
				return fmt.Errorf("generic candidate image is required")
			}
			if len(info.Names) != 0 {
				return fmt.Errorf("generic candidate must not use configured names")
			}
			if _, exists := pendingPortrait(s); !exists {
				s.Portraits = append(s.Portraits, portraitEntry{Operator: info.Operator, Template: info.Template, Image: info.Portrait})
			}
		} else if err := restrictDialogue(store, info.Names); err != nil {
			return err
		}
		if err := store.OverridePipeline(map[string]any{
			"GiftOperatorSendSetGenericDialoguePhase": map[string]any{"enabled": s.Generic},
		}); err != nil {
			return fmt.Errorf("set dialogue phase: %w", err)
		}
	case "identify":
		if !s.Generic || s.Pending == "" {
			return fmt.Errorf("no generic recipient is reserved")
		}
		var encounter struct {
			Name string `json:"name"`
		}
		if err := decodeCustomDetail(detail, &encounter); err != nil {
			return err
		}
		s.EncounteredName = normalizeName(encounter.Name)
		if s.EncounteredName == "" || blockedDialogueName(s, s.EncounteredName) {
			return fmt.Errorf("interaction name is unavailable")
		}
		if err := restrictDialogue(store, []string{s.EncounteredName}); err != nil {
			return err
		}
	case "skip_dialogue":
		if !s.Generic || s.EncounteredName == "" {
			return fmt.Errorf("unverified interaction name is missing")
		}
		if !containsOperator(s.AvoidNames, s.EncounteredName) {
			s.AvoidNames = append(s.AvoidNames, s.EncounteredName)
		}
		s.EncounteredName = ""
		s.Prepared = false
	case "observe_before", "observe_after", "observe_selection":
		var status selectionStatus
		if err := decodeCustomDetail(detail, &status); err != nil {
			return err
		}
		if err := applyGiftObservation(store, &s, param.Operation, status); err != nil {
			return err
		}
	case "reject":
		if err := s.reject(); err != nil {
			return err
		}
	case "finish":
		log.Info().Str("component", "GiftOperatorSessionAction").
			Int("completed", len(s.Completed)).Int("remaining", s.Remaining).
			Strs("completed_operators", s.Completed).Strs("excluded_operators", s.Excluded).
			Msg("gift recipient session finished")
		if s.Remaining > 0 {
			return fmt.Errorf("gift candidates exhausted with %d recipients remaining", s.Remaining)
		}
	default:
		return fmt.Errorf("unsupported operation %q", param.Operation)
	}
	return saveSession(store, s)
}

func applyGiftObservation(store nodeStore, s *session, operation string, status selectionStatus) error {
	if operation != "observe_selection" && status.Preview {
		return fmt.Errorf("gift preview cannot confirm actual progress")
	}
	if s.Generic && !status.IdentityMatch {
		if operation != "observe_before" {
			return fmt.Errorf("verification screen belongs to another operator")
		}
		s.Prepared = false
		return setGiftEligibility(store, false, false, true)
	}
	if status.Trust == nil || status.Limited == nil {
		return fmt.Errorf("gift status must include trust and limited")
	}
	if status.CompletedRings == nil && *status.Trust != 200 {
		return fmt.Errorf("gift status must include completed daily rings")
	}
	if s.Generic {
		if err := bindGiftIdentity(s, status.Name, operation == "observe_before"); err != nil {
			return err
		}
	}
	// Missing tier is permitted only at terminal max trust. Keep the baseline
	// ring value, rather than inventing a newly completed ring for that case.
	rings := s.BeforeRings
	if status.CompletedRings != nil {
		rings = *status.CompletedRings
	}
	switch operation {
	case "observe_before":
		if s.Generic && processedGiftName(*s, status.Name) {
			s.Prepared = false
			log.Info().Str("component", "GiftOperatorSessionAction").Str("name", status.Name).Msg("excluded previously processed gift UI name")
			return setGiftEligibility(store, false, true, false)
		}
		if err := s.observeBefore(*status.Trust, rings); err != nil {
			return err
		}
		return setGiftEligibility(store, s.Prepared, !s.Prepared, false)
	case "observe_selection":
		if !status.Preview || status.SelectedCount == nil {
			return fmt.Errorf("gift selection must include preview and selected count")
		}
		ready, err := s.observeSelection(*status.Trust, rings, *status.SelectedCount)
		if err != nil {
			return err
		}
		log.Info().Str("component", "GiftOperatorSessionAction").Str("operator", s.Pending).
			Int("selected_count", s.SelectedCount).Int("preview_rings", rings).Bool("ready", ready).Msg("observed selected gift total")
		return setSelectionReadiness(store, ready, !ready)
	case "observe_after":
		committed, err := s.commitAfter(*status.Trust, rings)
		if err != nil {
			return err
		}
		log.Info().Str("component", "GiftOperatorSessionAction").Str("operator", s.Pending).
			Int("remaining", s.Remaining).Int("target_rings", s.TargetRings).Int("completed_rings", rings).
			Bool("rings_known", status.CompletedRings != nil).Bool("committed", committed).Msg("confirmed gift result")
		return nil
	default:
		return fmt.Errorf("unsupported gift observation %q", operation)
	}
}

func setSelectionReadiness(store nodeStore, ready, more bool) error {
	return store.OverridePipeline(map[string]any{
		"GiftOperatorSendSelectionReady": map[string]any{"enabled": ready},
		"GiftOperatorSendSelectionMore":  map[string]any{"enabled": more},
	})
}

func decodeCustomDetail(detail *maa.RecognitionDetail, target any) error {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return fmt.Errorf("custom recognition detail is missing")
	}
	custom, ok := detail.Results.Best.AsCustom()
	if !ok || custom == nil {
		return fmt.Errorf("expected a custom recognition result")
	}
	if err := json.Unmarshal([]byte(custom.Detail), target); err != nil {
		return fmt.Errorf("parse custom recognition detail: %w", err)
	}
	return nil
}

func restrictDialogue(store nodeStore, names []string) error {
	expected := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			expected = append(expected, "^"+regexp.QuoteMeta(name)+"$")
		}
	}
	if len(expected) == 0 {
		return fmt.Errorf("candidate operator names are required")
	}
	if err := store.OverridePipeline(map[string]any{"GiftOperatorName": map[string]any{"expected": expected}}); err != nil {
		return fmt.Errorf("restrict dialogue to observed operator: %w", err)
	}
	return nil
}

func setGiftEligibility(store nodeStore, prepared, full, wrong bool) error {
	return store.OverridePipeline(map[string]any{
		"GiftOperatorSendCanGift":        map[string]any{"enabled": prepared},
		"GiftOperatorSendAlreadyFull":    map[string]any{"enabled": full},
		"GiftOperatorSendWrongRecipient": map[string]any{"enabled": wrong},
	})
}

// bindGiftIdentity accepts UI names only after the runtime portrait has matched.
// Interaction OCR can use a different spelling; retain it separately for dialogue.
func bindGiftIdentity(s *session, name string, before bool) error {
	name = normalizeName(name)
	if name == "" {
		return fmt.Errorf("gift UI name is required")
	}
	for i := range s.Portraits {
		portrait := &s.Portraits[i]
		if portrait.Operator != s.Pending {
			continue
		}
		if portrait.Name != "" && portrait.Name != name {
			return fmt.Errorf("gift UI name changed for the reserved portrait")
		}
		if !before && portrait.Name == "" {
			return fmt.Errorf("initial recipient identity is missing")
		}
		if before {
			if s.EncounteredName == "" {
				return fmt.Errorf("initial interaction name is missing")
			}
			portrait.Name = name
			portrait.DialogueName = s.EncounteredName
		}
		return nil
	}
	return fmt.Errorf("reserved runtime portrait is missing")
}

// processedGiftName prevents a changed portrait match from counting the same person twice.
func processedGiftName(s session, name string) bool {
	name = normalizeName(name)
	for _, portrait := range s.Portraits {
		if portrait.Operator != s.Pending && normalizeName(portrait.Name) == name &&
			(containsOperator(s.Completed, portrait.Operator) || containsOperator(s.Excluded, portrait.Operator)) {
			return true
		}
	}
	return false
}
