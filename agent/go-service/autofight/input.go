package autofight

import "strings"

// fightInput owns only this callback's requested held contacts. A failed Down can
// still have reached the device, so its release is tracked before submitting it.
type fightInput struct {
	run         func(string) bool
	held        map[string]bool
	firstFailed string
}

func (in *fightInput) execute(name string) bool {
	if up := releaseFor(name); up != "" {
		if in.held == nil {
			in.held = make(map[string]bool)
		}
		in.held[up] = true
	} else {
		// A release is attempted at most once, even if its result is uncertain.
		delete(in.held, name)
	}
	ok := in.run(name)
	if !ok && in.firstFailed == "" {
		in.firstFailed = name
	}
	return ok
}

func releaseFor(name string) string {
	if name == "__AutoFightActionAttackTouchDown" {
		return "__AutoFightActionAttackTouchUp"
	}
	if strings.HasPrefix(name, "__AutoFightActionMove") && strings.HasSuffix(name, "KeyDown") {
		return strings.TrimSuffix(name, "KeyDown") + "KeyUp"
	}
	return ""
}

func (in *fightInput) release() bool {
	// Fixed order keeps cleanup bounded and its evidence reproducible.
	for _, name := range []string{
		"__AutoFightActionMoveBackKeyUp", "__AutoFightActionMoveForwardKeyUp",
		"__AutoFightActionMoveLeftKeyUp", "__AutoFightActionMoveRightKeyUp",
		"__AutoFightActionAttackTouchUp",
	} {
		if in.held[name] {
			in.execute(name)
		}
	}
	return in.firstFailed == ""
}

func finishBattle(in *fightInput, result *bool) {
	if !in.release() {
		*result = false
	}
	actionQueue = nil
}
