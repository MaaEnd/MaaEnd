package autofight

import (
	"reflect"
	"testing"
)

func TestFightInputCleanupPreservesFailure(t *testing.T) {
	const attackDown = "__AutoFightActionAttackTouchDown"
	const attackUp = "__AutoFightActionAttackTouchUp"
	const moveDown = "__AutoFightActionMoveBackKeyDown"
	const moveUp = "__AutoFightActionMoveBackKeyUp"
	const dodge = "__AutoFightActionDodge"
	for _, tc := range []struct {
		name                       string
		steps, failures, wantCalls []string
		firstFailed                string
	}{
		{"normal release", []string{attackDown, attackUp}, nil, []string{attackDown, attackUp}, ""},
		{"final release fails", []string{attackDown, attackUp}, []string{attackUp}, []string{attackDown, attackUp}, attackUp},
		{"middle input fails", []string{attackDown, moveDown, dodge}, []string{dodge}, []string{attackDown, moveDown, dodge, moveUp, attackUp}, dodge},
		{"cleanup also fails", []string{attackDown, moveDown, dodge}, []string{dodge, moveUp, attackUp}, []string{attackDown, moveDown, dodge, moveUp, attackUp}, dodge},
		{"uncertain down", []string{attackDown}, []string{attackDown}, []string{attackDown, attackUp}, attackDown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			in := &fightInput{run: func(name string) bool {
				calls = append(calls, name)
				for _, failed := range tc.failures {
					if name == failed {
						return false
					}
				}
				return true
			}}
			result := true
			for _, step := range tc.steps {
				if !in.execute(step) {
					result = false
					break
				}
			}
			actionQueue = []fightAction{{action: ActionDodge}}
			finishBattle(in, &result)
			if !reflect.DeepEqual(calls, tc.wantCalls) || in.firstFailed != tc.firstFailed || result != (tc.firstFailed == "") || len(actionQueue) != 0 {
				t.Fatalf("calls=%v first=%q result=%v queue=%d", calls, in.firstFailed, result, len(actionQueue))
			}
		})
	}
}
