package ims

import (
	"sync"
	"time"
)

// cache holds in-process item inventory metadata used by IMS recognitions/actions.
// Only a successful inventory sync (A2 / markSynced) flips hasData and lastSync.
// A1 may adjust item quantities without changing readiness.
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

// quantity returns cached count for item; missing item is 0.
func (c *cache) quantity(item string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.items[item]
}

// applyDelta adds delta to item quantity and clamps the result to >= 0.
// Does not change hasData / lastSync. Returns a copy of all items after update.
func (c *cache) applyDelta(item string, delta int) (before, after int, clamped bool, items map[string]int, lastSync time.Time, hasData bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before = c.items[item]
	after = before + delta
	if after < 0 {
		after = 0
		clamped = true
	}
	c.items[item] = after
	items = make(map[string]int, len(c.items))
	for k, v := range c.items {
		items[k] = v
	}
	return before, after, clamped, items, c.lastSync, c.hasData
}

// setItemsOnly replaces item quantities without changing hasData / lastSync.
func (c *cache) setItemsOnly(items map[string]int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]int, len(items))
	for name, qty := range items {
		c.items[name] = qty
	}
}

// MarkSynced records a successful inventory sync for later A2 use.
func MarkSynced(at time.Time, items map[string]int) {
	globalCache.markSynced(at, items)
}

// ItemsSnapshot returns a copy of cached item quantities.
func ItemsSnapshot() map[string]int {
	return globalCache.itemsCopy()
}

// ClearCache clears IMS cache state (tests / account switch).
// Marks hydrate complete so the empty state is intentional and disk is not reloaded.
func ClearCache() {
	recordMu.Lock()
	defer recordMu.Unlock()
	globalCache.clear()
	hydrated = true
}
