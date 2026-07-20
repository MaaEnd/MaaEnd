package sellproduct

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// outpostProsperityCacheAccount 保存一个账号最近确认的各据点发展值状态。
// Locations 缺少某个据点表示状态未知，规划时按发展值未满处理。
type outpostProsperityCacheAccount struct {
	UpdatedAt string          `json:"updated_at"`
	Locations map[string]bool `json:"locations"`
}

func loadOutpostProsperityMaxLocations(uid string) (map[string]struct{}, error) {
	cache, err := readOperatorCache(resolveOperatorCachePathFunc(uid))
	if err != nil {
		return nil, err
	}
	return outpostProsperityMaxLocationsForUID(cache, uid), nil
}

func persistOutpostProsperityStatus(uid string, location string, reached bool) (bool, error) {
	return updateOutpostProsperityCache(
		resolveOperatorCachePathFunc(uid),
		uid,
		location,
		reached,
		time.Now(),
	)
}

func outpostProsperityStatusesForUID(cache operatorCacheFile, uid string) map[string]bool {
	uid = normalizeOperatorCacheUID(uid)
	account, ok := normalizeOperatorCacheFile(cache).OutpostProsperity[uid]
	if !ok {
		return nil
	}
	return cloneBoolMap(account.Locations)
}

func outpostProsperityMaxLocationsForUID(cache operatorCacheFile, uid string) map[string]struct{} {
	statuses := outpostProsperityStatusesForUID(cache, uid)
	locations := make(map[string]struct{}, len(statuses))
	for location, reached := range statuses {
		if reached {
			locations[location] = struct{}{}
		}
	}
	return locations
}

func updateOutpostProsperityCache(
	path string,
	uid string,
	location string,
	reached bool,
	now time.Time,
) (bool, error) {
	sellProductCacheMu.Lock()
	defer sellProductCacheMu.Unlock()

	cache, err := readOperatorCache(path)
	if err != nil {
		return false, err
	}
	uid = normalizeOperatorCacheUID(uid)
	location = strings.TrimSpace(location)
	if location == "" {
		return false, fmt.Errorf("outpost prosperity location is empty")
	}
	if previous, ok := cache.OutpostProsperity[uid].Locations[location]; ok && previous == reached {
		return false, nil
	}

	updatedAt := now.UTC().Format(time.RFC3339)
	account := cache.OutpostProsperity[uid]
	account.UpdatedAt = updatedAt
	account.Locations = cloneBoolMap(account.Locations)
	if account.Locations == nil {
		account.Locations = map[string]bool{}
	}
	account.Locations[location] = reached
	if cache.OutpostProsperity == nil {
		cache.OutpostProsperity = map[string]outpostProsperityCacheAccount{}
	}
	cache.UpdatedAt = updatedAt
	cache.OutpostProsperity[uid] = account
	if err := writeOperatorCacheFile(path, cache); err != nil {
		return false, err
	}
	return true, nil
}

// normalizeOutpostProsperityAccounts 规范 UID、据点名和时间，并稳定处理 UID 规范化后的碰撞。
func normalizeOutpostProsperityAccounts(
	accounts map[string]outpostProsperityCacheAccount,
) map[string]outpostProsperityCacheAccount {
	normalized := map[string]outpostProsperityCacheAccount{}
	rawUIDs := make([]string, 0, len(accounts))
	for uid := range accounts {
		rawUIDs = append(rawUIDs, uid)
	}
	sort.Slice(rawUIDs, func(i, j int) bool {
		left := strings.TrimSpace(accounts[rawUIDs[i]].UpdatedAt)
		right := strings.TrimSpace(accounts[rawUIDs[j]].UpdatedAt)
		if left != right {
			return left < right
		}
		return rawUIDs[i] < rawUIDs[j]
	})
	for _, rawUID := range rawUIDs {
		account := accounts[rawUID]
		uid := normalizeOperatorCacheUID(rawUID)
		existing := normalized[uid]
		locations := cloneBoolMap(existing.Locations)
		if locations == nil {
			locations = map[string]bool{}
		}
		for location, reached := range account.Locations {
			location = strings.TrimSpace(location)
			if location != "" {
				locations[location] = reached
			}
		}
		updatedAt := strings.TrimSpace(account.UpdatedAt)
		if updatedAt < existing.UpdatedAt {
			updatedAt = existing.UpdatedAt
		}
		normalized[uid] = outpostProsperityCacheAccount{
			UpdatedAt: updatedAt,
			Locations: locations,
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func cloneBoolMap(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
