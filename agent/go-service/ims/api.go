package ims

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// EnsureHydrated loads debug/record/IMS.json into memory at most once per
// process (until ClearCache). Later reads stay in memory. Missing or empty
// files become an empty cache; corrupt files are reset. Other go-service
// packages should call this before reading if Pipeline may not have touched
// IMS yet. Repeat calls are cheap no-ops.
func EnsureHydrated() error {
	return ensureHydrated()
}

// ItemQuantity returns the cached count for itemID. Missing IDs are 0.
// The first call in a process hydrates from disk when needed. After
// ClearCache the empty memory is kept and disk is not reloaded.
func ItemQuantity(itemID string) int {
	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Str("item_id", itemID).
			Msg("failed to hydrate ims cache")
		return 0
	}
	return globalCache.quantity(strings.TrimSpace(itemID))
}

// HasData reports whether a successful A2 sync has been recorded.
// Distinguishes "never synced" from "synced but this item is 0". TTL /
// freshness is still Pipeline R2 (ItemDataReady). Hydrates from disk on
// first access.
func HasData() bool {
	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Msg("failed to hydrate ims cache")
		return false
	}
	has, _ := globalCache.snapshot()
	return has
}

// ItemsSnapshot returns a copy of cached item quantities. Hydrates from
// disk on first access in this process.
func ItemsSnapshot() map[string]int {
	if err := ensureHydrated(); err != nil {
		log.Error().
			Err(err).
			Msg("failed to hydrate ims cache")
	}
	return globalCache.itemsCopy()
}
