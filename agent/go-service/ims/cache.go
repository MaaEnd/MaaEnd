package ims

import (
	"strings"
	"sync"
	"time"
)

// scratchPrefix marks virtual item keys. Virtual keys live in their own in-memory
// layer: they take part in the same arithmetic and expressions as real items, but
// they are never persisted to IMS.json and never establish readiness.
const scratchPrefix = "scratch_"

// IsScratchKey reports whether item is a virtual (scratch) key.
func IsScratchKey(item string) bool {
	return strings.HasPrefix(item, scratchPrefix)
}

// cache holds in-process item inventory metadata used by IMS recognitions/actions.
// Only a successful inventory sync (A2 / markSynced) flips hasData and lastSync.
// A1 / A4 may adjust item quantities without changing readiness.
//
// items is the inventory layer: written by A2 / A1 / A3 and A4 with a catalog key,
// persisted to IMS.json, and hydrated from it on first use.
// scratch is the virtual layer: written by A1 / A4 with a scratch_ key, in memory
// only, never persisted and never hydrated.
type cache struct {
	mu       sync.Mutex
	hasData  bool
	lastSync time.Time
	items    map[string]int
	scratch  map[string]int
}

var globalCache = &cache{
	items:   make(map[string]int),
	scratch: make(map[string]int),
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
	c.scratch = make(map[string]int)
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
// Scratch keys are read from the virtual layer, everything else from inventory.
func (c *cache) quantity(item string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if IsScratchKey(item) {
		return c.scratch[item]
	}
	return c.items[item]
}

// applyDelta adds delta to an inventory item quantity and clamps the result to >= 0.
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

// applyDeltaScratch adds delta to a virtual (scratch) quantity and clamps to >= 0.
// The virtual layer is memory only: nothing is persisted and readiness is untouched.
func (c *cache) applyDeltaScratch(item string, delta int) (before, after int, clamped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before = c.scratch[item]
	after = before + delta
	if after < 0 {
		after = 0
		clamped = true
	}
	c.scratch[item] = after
	return before, after, clamped
}

// setAbsolute overwrites an inventory item quantity with an absolute value, clamped
// to >= 0. Returns a copy of all items after the write so the caller can persist the
// snapshot. Does not change hasData / lastSync (only A2 marks a real sync).
func (c *cache) setAbsolute(item string, value int) (before, after int, clamped bool, items map[string]int, lastSync time.Time, hasData bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	before = c.items[item]
	after = value
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

// setScratch overwrites a virtual (scratch) quantity with an absolute value, clamped
// to >= 0. The virtual layer is memory only: nothing is persisted.
func (c *cache) setScratch(item string, value int) (before, after int, clamped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	before = c.scratch[item]
	after = value
	if after < 0 {
		after = 0
		clamped = true
	}
	c.scratch[item] = after
	return before, after, clamped
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
