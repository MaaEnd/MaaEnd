package everynthhit

import (
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var states = struct {
	sync.Mutex
	counts   map[string]int
	skips    map[string]struct{}
	skipNext bool
}{
	counts: make(map[string]int),
	skips:  make(map[string]struct{}),
}

// Advance reports whether this call is the every-th hit of key. The first call
// after a task starts and the call right after a hit are not counted, because
// both run before another route completes. every <= 0 never counts and never
// hits.
func Advance(key string, every int) bool {
	if every <= 0 {
		return false
	}

	states.Lock()
	defer states.Unlock()

	if states.skipNext {
		states.skipNext = false
		return false
	}

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

// startRun clears every counter and leaves the next call uncounted, because
// the first recognition after a task starts runs before any route completes.
func startRun() {
	states.Lock()
	clear(states.counts)
	clear(states.skips)
	states.skipNext = true
	states.Unlock()
}

// ResetAll clears every counter together with its pending uncounted call.
func ResetAll() {
	states.Lock()
	clear(states.counts)
	clear(states.skips)
	states.skipNext = false
	states.Unlock()
}

var _ maa.TaskerEventSink = &stateResetSink{}

// stateResetSink starts every task from an empty counter, so a stopped task
// never affects the next run and the pre-route scan is not counted.
type stateResetSink struct{}

// OnTaskerTask implements maa.TaskerEventSink.
func (*stateResetSink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, _ maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}
	startRun()
}
