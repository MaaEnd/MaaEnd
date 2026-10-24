package everynthhit

import "sync"

var states = struct {
	sync.Mutex
	counts map[string]int
	skips  map[string]struct{}
}{
	counts: make(map[string]int),
	skips:  make(map[string]struct{}),
}

// Advance reports whether this call is the every-th hit of key. The call right
// after a hit is not counted, because the loop re-enters the recognition once
// without a completed route in between. every <= 0 never counts and never
// hits.
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

// ResetAll clears every counter together with its pending uncounted call.
func ResetAll() {
	states.Lock()
	clear(states.counts)
	clear(states.skips)
	states.Unlock()
}
