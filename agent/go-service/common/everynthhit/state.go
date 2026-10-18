package everynthhit

import (
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var states = struct {
	sync.Mutex
	counts map[string]int
}{counts: make(map[string]int)}

// Advance increases the counter of key by one and reports whether this call is
// the every-th hit. A hit clears the counter, so the next cycle starts from
// one. every <= 0 never counts and never hits.
func Advance(key string, every int) bool {
	if every <= 0 {
		return false
	}

	states.Lock()
	defer states.Unlock()
	states.counts[key]++
	if states.counts[key] < every {
		return false
	}
	delete(states.counts, key)
	return true
}

// Reset clears the counter of key.
func Reset(key string) {
	states.Lock()
	delete(states.counts, key)
	states.Unlock()
}

// ResetAll clears every counter.
func ResetAll() {
	states.Lock()
	clear(states.counts)
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
