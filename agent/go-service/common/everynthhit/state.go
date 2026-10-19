package everynthhit

import (
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var states = struct {
	sync.Mutex
	counts map[string]int
	skips  map[string]struct{}
}{
	counts: make(map[string]int),
	skips:  make(map[string]struct{}),
}

// Advance reports whether this call is the every-th hit of key. The call right
// after a hit or a reset is not counted, because the loop re-enters the
// recognition once without a completed route in between. every <= 0 never
// counts and never hits.
func Advance(key string, every int) bool {
	if every <= 0 {
		return false
	}

	states.Lock()
	defer states.Unlock()

	if _, ok := states.skips[key]; ok {
		delete(states.skips, key)
		return false
	}

	states.counts[key]++
	if states.counts[key] < every {
		return false
	}
	delete(states.counts, key)
	states.skips[key] = struct{}{}
	return true
}

// Reset clears the counter of key and leaves the next call uncounted, so the
// first loop scan after a task start is not treated as a completed route.
func Reset(key string) {
	states.Lock()
	delete(states.counts, key)
	states.skips[key] = struct{}{}
	states.Unlock()
}

// ResetAll clears every counter together with its pending uncounted call.
func ResetAll() {
	states.Lock()
	clear(states.counts)
	clear(states.skips)
	states.Unlock()
}

var _ maa.TaskerEventSink = &stateResetSink{}

// stateResetSink clears every counter once a task ends, so counters left over
// by a stopped task never affect the next run. It is a fallback only: callers
// are expected to reset the keys they use.
type stateResetSink struct{}

// OnTaskerTask implements maa.TaskerEventSink.
func (*stateResetSink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, _ maa.TaskerTaskDetail) {
	if event != maa.EventStatusSucceeded && event != maa.EventStatusFailed {
		return
	}
	ResetAll()
}
