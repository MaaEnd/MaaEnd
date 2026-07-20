package sellproduct

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
)

const (
	// 所有账号共享同一个 JSON 文件，并由 Accounts 按 UID 隔离状态。
	sellProductCacheFileName = "SellProductOwnedOperators.json"
	// 尚未捕获 UID 时仍允许使用临时分区，避免把空字符串作为 map 键写入文件。
	sellProductCacheUnknownUID = "unknown"
)

// resolveSellProductCachePathFunc 是单元测试替换缓存目录的注入点。
var resolveSellProductCachePathFunc = defaultSellProductCachePath

// sellProductCacheMu 串行化同一账号缓存的读取-修改-写入，防止干员与据点状态互相覆盖。
var sellProductCacheMu sync.Mutex

// sellProductCache 是 SellProduct 持久缓存的顶层格式。
// Accounts 按 UID 同时保存完整干员快照和据点发展值状态。
type sellProductCache struct {
	UpdatedAt string                             `json:"updated_at"`
	Accounts  map[string]sellProductCacheAccount `json:"accounts,omitempty"`
}

// sellProductCacheAccount 保存一个账号的完整干员快照和各据点发展值状态。
// Operators 为 nil 表示尚未完成扫描，空数组表示完整扫描后没有相关干员。
type sellProductCacheAccount struct {
	UpdatedAt string          `json:"updated_at"`
	Operators []string        `json:"operators"`
	Locations map[string]bool `json:"locations,omitempty"`
}

// currentSellProductCacheUID 获取 CaptureUID 模块最近识别到的账号并规范化为安全键名。
func currentSellProductCacheUID() string {
	return normalizeSellProductCacheUID(captureuid.GetCachedUID())
}

// defaultSellProductCachePath 返回运行记录目录中的统一缓存文件路径。
// uid 由文件内部 Accounts 分区，因此不参与文件名拼接。
func defaultSellProductCachePath(string) string {
	return filepath.Join("debug", "record", sellProductCacheFileName)
}

// readSellProductCache 读取并规范化缓存；文件不存在或为空视为尚未建立缓存，而不是错误。
func readSellProductCache(path string) (sellProductCache, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sellProductCache{}, nil
		}
		return sellProductCache{}, fmt.Errorf("read sell product cache: %w", err)
	}
	if len(raw) == 0 {
		return sellProductCache{}, nil
	}

	var cache sellProductCache
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cache); err != nil {
		return sellProductCache{}, fmt.Errorf("parse sell product cache: %w", err)
	}
	return normalizeSellProductCache(cache), nil
}

// writeSellProductCache 规范化并格式化缓存，然后使用原子替换方式写盘。
func writeSellProductCache(path string, cache sellProductCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create sell product cache dir: %w", err)
	}
	cache = normalizeSellProductCache(cache)
	raw, err := json.MarshalIndent(cache, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal sell product cache: %w", err)
	}
	raw = append(raw, '\n')
	if err := writeSellProductCacheAtomic(path, raw, 0644); err != nil {
		return fmt.Errorf("write sell product cache: %w", err)
	}
	return nil
}

// mergeOperatorSnapshot 用一次完整列表扫描结果替换当前账号的干员快照。
func mergeOperatorSnapshot(
	cache sellProductCache,
	uid string,
	scanCandidates []operatorCandidate,
	owned []string,
	now time.Time,
) sellProductCache {
	uid = normalizeSellProductCacheUID(uid)
	operatorSet := make(map[string]struct{}, len(owned))
	scanSet := operatorCandidateCacheNameSet(scanCandidates)

	for _, name := range owned {
		if _, ok := scanSet[name]; ok {
			operatorSet[name] = struct{}{}
		}
	}

	return withOperatorSnapshot(cache, uid, operatorSet, now)
}

// sellProductCacheHasOperatorSnapshot 判断指定账号是否建立过完整干员快照。
// Operators 为 nil 表示只有据点状态；空数组表示完整扫描后没有相关干员。
func sellProductCacheHasOperatorSnapshot(cache sellProductCache, uid string) bool {
	uid = normalizeSellProductCacheUID(uid)
	account, ok := normalizeSellProductCache(cache).Accounts[uid]
	return ok && account.Operators != nil
}

// cachedOperatorNamesForUID 返回指定账号的规范化干员列表。
func cachedOperatorNamesForUID(cache sellProductCache, uid string) []string {
	uid = normalizeSellProductCacheUID(uid)
	account, ok := normalizeSellProductCache(cache).Accounts[uid]
	if !ok {
		return nil
	}
	return account.Operators
}

// loadOutpostProsperityMaxLocations 从统一账号缓存中读取已满级据点。
func loadOutpostProsperityMaxLocations(uid string) (map[string]struct{}, error) {
	cache, err := readSellProductCache(resolveSellProductCachePathFunc(uid))
	if err != nil {
		return nil, err
	}
	return outpostProsperityMaxLocationsForUID(cache, uid), nil
}

// persistOutpostProsperityStatus 把本次识别到的据点状态写回统一账号缓存。
func persistOutpostProsperityStatus(uid string, location string, reached bool) (bool, error) {
	return updateCachedOutpostProsperity(
		resolveSellProductCachePathFunc(uid),
		uid,
		location,
		reached,
		time.Now(),
	)
}

func outpostProsperityStatusesForUID(cache sellProductCache, uid string) map[string]bool {
	uid = normalizeSellProductCacheUID(uid)
	account, ok := normalizeSellProductCache(cache).Accounts[uid]
	if !ok {
		return nil
	}
	return cloneBoolMap(account.Locations)
}

func outpostProsperityMaxLocationsForUID(cache sellProductCache, uid string) map[string]struct{} {
	statuses := outpostProsperityStatusesForUID(cache, uid)
	locations := make(map[string]struct{}, len(statuses))
	for location, reached := range statuses {
		if reached {
			locations[location] = struct{}{}
		}
	}
	return locations
}

func updateCachedOutpostProsperity(
	path string,
	uid string,
	location string,
	reached bool,
	now time.Time,
) (bool, error) {
	sellProductCacheMu.Lock()
	defer sellProductCacheMu.Unlock()

	cache, err := readSellProductCache(path)
	if err != nil {
		return false, err
	}
	uid = normalizeSellProductCacheUID(uid)
	location = strings.TrimSpace(location)
	if location == "" {
		return false, fmt.Errorf("outpost prosperity location is empty")
	}
	if previous, ok := cache.Accounts[uid].Locations[location]; ok && previous == reached {
		return false, nil
	}

	updatedAt := now.UTC().Format(time.RFC3339)
	account := cache.Accounts[uid]
	account.UpdatedAt = updatedAt
	account.Locations = cloneBoolMap(account.Locations)
	if account.Locations == nil {
		account.Locations = map[string]bool{}
	}
	account.Locations[location] = reached
	if cache.Accounts == nil {
		cache.Accounts = map[string]sellProductCacheAccount{}
	}
	cache.UpdatedAt = updatedAt
	cache.Accounts[uid] = account
	if err := writeSellProductCache(path, cache); err != nil {
		return false, err
	}
	return true, nil
}

// withOperatorSnapshot 把完整干员集合写回指定账号，并保留同账号的据点状态。
func withOperatorSnapshot(
	cache sellProductCache,
	uid string,
	operatorSet map[string]struct{},
	now time.Time,
) sellProductCache {
	cache = normalizeSellProductCache(cache)
	uid = normalizeSellProductCacheUID(uid)
	updatedAt := now.UTC().Format(time.RFC3339)
	cache.UpdatedAt = updatedAt
	if cache.Accounts == nil {
		cache.Accounts = map[string]sellProductCacheAccount{}
	}
	account := cache.Accounts[uid]
	account.UpdatedAt = updatedAt
	account.Operators = sortedSetValues(operatorSet)
	cache.Accounts[uid] = account
	return cache
}

// normalizeSellProductCache 消除缓存中的不稳定表示：
// 规范 UID、合并碰撞账号、对干员去重排序，并清洗据点状态。
func normalizeSellProductCache(cache sellProductCache) sellProductCache {
	normalized := sellProductCache{
		UpdatedAt: strings.TrimSpace(cache.UpdatedAt),
		Accounts:  map[string]sellProductCacheAccount{},
	}
	for uid, account := range cache.Accounts {
		uid = normalizeSellProductCacheUID(uid)
		operatorSet := operatorNameSet(account.Operators)
		existing := normalized.Accounts[uid]
		for _, name := range existing.Operators {
			operatorSet[name] = struct{}{}
		}
		hasOperatorSnapshot := account.Operators != nil || existing.Operators != nil
		var operators []string
		if hasOperatorSnapshot {
			operators = sortedSetValues(operatorSet)
		}
		updatedAt := strings.TrimSpace(account.UpdatedAt)
		if updatedAt < existing.UpdatedAt {
			updatedAt = existing.UpdatedAt
		}
		normalized.Accounts[uid] = sellProductCacheAccount{
			UpdatedAt: updatedAt,
			Operators: operators,
			Locations: mergeOutpostProsperityLocations(existing.Locations, account.Locations),
		}
	}
	if len(normalized.Accounts) == 0 {
		normalized.Accounts = nil
	}
	return normalized
}

// mergeOutpostProsperityLocations 合并并规范据点状态；异常 UID 碰撞出现冲突时按未满处理。
func mergeOutpostProsperityLocations(groups ...map[string]bool) map[string]bool {
	locations := map[string]bool{}
	for _, group := range groups {
		for location, reached := range group {
			location = strings.TrimSpace(location)
			if location == "" {
				continue
			}
			if previous, ok := locations[location]; ok {
				locations[location] = previous && reached
				continue
			}
			locations[location] = reached
		}
	}
	if len(locations) == 0 {
		return nil
	}
	return locations
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

// writeSellProductCacheAtomic 先在目标目录写入临时文件并刷盘，再原子重命名覆盖正式文件。
// 任一步失败都会清理临时文件，防止进程中断留下半截 JSON 破坏后续任务。
func writeSellProductCacheAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// normalizeSellProductCacheUID 把 UID 限制为适合作为持久化 map 键的 ASCII 字符集合。
// 非法字符统一替换为下划线；空 UID 使用 unknown 分区，后续捕获真实 UID 后自然隔离。
func normalizeSellProductCacheUID(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return sellProductCacheUnknownUID
	}

	var b strings.Builder
	b.Grow(len(uid))
	for _, r := range uid {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	normalized := b.String()
	if normalized == "" {
		return sellProductCacheUnknownUID
	}
	return normalized
}

// operatorNameSet 将名称切片转换为去重集合，并忽略空名称。
func operatorNameSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

// operatorCandidateCacheNameSet 提取候选域中所有稳定缓存键。
func operatorCandidateCacheNameSet(candidates []operatorCandidate) map[string]struct{} {
	set := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		name := operatorCandidateCacheName(candidate)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

// sortedSetValues 把集合转换为按字典序排列的稳定切片，便于缓存序列化和测试比较。
func sortedSetValues(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
