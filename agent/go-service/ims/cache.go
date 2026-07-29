package ims

import (
	"sync"
	"time"
)

// cache holds in-process item inventory metadata used by IMS recognitions/actions.
// Only a successful inventory sync (A2) should call markSynced.
type cache struct {
	mu       sync.Mutex
	hasData  bool
	lastSync time.Time
	items    map[string]int
}

var globalCache = &cache{
	items: make(map[string]int),
}

func (c *cache) snapshot() (hasData bool, lastSync time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hasData, c.lastSync
}

// markSynced records a successful inventory sync. items may be nil/empty.
func (c *cache) markSynced(at time.Time, items map[string]int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasData = true
	c.lastSync = at
	c.items = make(map[string]int, len(items))
	for name, qty := range items {
		c.items[name] = qty
	}
}

func (c *cache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasData = false
	c.lastSync = time.Time{}
	c.items = make(map[string]int)
}

func (c *cache) itemsCopy() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.items))
	for k, v := range c.items {
		out[k] = v
	}
	return out
}

// MarkSynced records a successful inventory sync for later A2 use.
func MarkSynced(at time.Time, items map[string]int) {
	globalCache.markSynced(at, items)
}

// ItemsSnapshot returns a copy of cached item quantities.
func ItemsSnapshot() map[string]int {
	return globalCache.itemsCopy()
}

// ClearCache clears IMS cache state (tests / future account switch).
func ClearCache() {
	globalCache.clear()
}
