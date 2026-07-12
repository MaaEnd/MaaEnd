package failurecollector

import "sync"

type state struct {
	failures []string
}

var states = struct {
	sync.Mutex
	byKey map[string]state
}{byKey: make(map[string]state)}

func Reset(key string) {
	states.Lock()
	states.byKey[key] = state{}
	states.Unlock()
}

func Record(key, name string) {
	states.Lock()
	value := states.byKey[key]
	value.failures = append(value.failures, name)
	states.byKey[key] = value
	states.Unlock()
}

func Finish(key string) []string {
	states.Lock()
	defer states.Unlock()
	failures := append([]string(nil), states.byKey[key].failures...)
	delete(states.byKey, key)
	return failures
}
