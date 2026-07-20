package sellproduct

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	sellProductCacheFileName = "SellProductCache.json"
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
	Accounts map[string]sellProductCacheAccount `json:"accounts,omitempty"`
}

// sellProductCacheEnvelope 延迟解析账号对象，使单个账号损坏时仍能保留其他账号。
type sellProductCacheEnvelope struct {
	Accounts json.RawMessage `json:"accounts,omitempty"`
}

// sellProductCacheAccount 保存一个账号的完整干员快照和各据点发展值状态。
// Operators 为 nil 表示尚未完成扫描；非 nil 且 IDs 为空表示完整扫描后没有相关干员。
type sellProductCacheAccount struct {
	Operators *sellProductOperatorSnapshot `json:"operators,omitempty"`
	Locations map[string]bool              `json:"locations,omitempty"`
}

// sellProductOperatorSnapshot 把完整干员集合与其扫描时间绑定，避免据点状态更新污染干员缓存时间。
type sellProductOperatorSnapshot struct {
	UpdatedAt time.Time `json:"updated_at"`
	IDs       []string  `json:"ids"`
}

// currentSellProductCacheUID 获取 CaptureUID 模块生成的加盐哈希；尚未捕获时使用 unknown 分区。
func currentSellProductCacheUID() string {
	uid := captureuid.GetCachedUID()
	if uid == "" {
		return sellProductCacheUnknownUID
	}
	return uid
}

// defaultSellProductCachePath 返回运行记录目录中的统一缓存文件路径。
func defaultSellProductCachePath() string {
	return filepath.Join("debug", "record", sellProductCacheFileName)
}

// readSellProductCache 读取并规范化缓存。
// 顶层结构损坏时整份缓存视为不存在；单个账号不合法时只丢弃对应账号。
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

	var envelope sellProductCacheEnvelope
	if !decodeStrictSellProductCacheJSON(raw, &envelope) {
		return sellProductCache{}, nil
	}
	if len(envelope.Accounts) == 0 {
		return sellProductCache{}, nil
	}
	accountsRaw := bytes.TrimSpace(envelope.Accounts)
	if len(accountsRaw) == 0 || accountsRaw[0] != '{' {
		return sellProductCache{}, nil
	}

	var accounts map[string]json.RawMessage
	if !decodeStrictSellProductCacheJSON(accountsRaw, &accounts) {
		return sellProductCache{}, nil
	}
	data, err := loadSellProductSelectionDataCached()
	if err != nil {
		return sellProductCache{}, fmt.Errorf("validate sell product cache accounts: %w", err)
	}
	cache := sellProductCache{Accounts: map[string]sellProductCacheAccount{}}
	for uid, accountRaw := range accounts {
		if !isValidSellProductCacheUID(uid) {
			continue
		}
		accountJSON := bytes.TrimSpace(accountRaw)
		if len(accountJSON) == 0 || accountJSON[0] != '{' {
			continue
		}
		var account sellProductCacheAccount
		if !decodeStrictSellProductCacheJSON(accountJSON, &account) {
			continue
		}
		if !sellProductCacheAccountIsValid(account, data) {
			continue
		}
		cache.Accounts[uid] = account
	}
	return normalizeSellProductCache(cache), nil
}

// writeSellProductCache 规范化并格式化缓存，然后使用原子替换方式写盘。
func writeSellProductCache(path string, cache sellProductCache) error {
	cache = normalizeSellProductCache(cache)
	valid, err := sellProductCacheIsValid(cache)
	if err != nil {
		return fmt.Errorf("validate sell product cache: %w", err)
	}
	if !valid {
		return fmt.Errorf("validate sell product cache: invalid structure")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create sell product cache dir: %w", err)
	}
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

// sellProductCacheIsValid 验证待写入缓存的 UID 和每个账号都符合当前严格结构。
func sellProductCacheIsValid(cache sellProductCache) (bool, error) {
	data, err := loadSellProductSelectionDataCached()
	if err != nil {
		return false, err
	}
	for uid, account := range cache.Accounts {
		if !isValidSellProductCacheUID(uid) {
			return false, nil
		}
		if !sellProductCacheAccountIsValid(account, data) {
			return false, nil
		}
	}
	return true, nil
}

func sellProductCacheAccountIsValid(
	account sellProductCacheAccount,
	data *sellProductSelectionDataFile,
) bool {
	if account.Operators != nil {
		if account.Operators.IDs == nil || account.Operators.UpdatedAt.IsZero() {
			return false
		}
		for _, operatorID := range account.Operators.IDs {
			if _, ok := data.Operators[operatorID]; !ok {
				return false
			}
		}
	}
	for locationID := range account.Locations {
		if _, ok := data.Locations[locationID]; !ok {
			return false
		}
	}
	return true
}

func decodeStrictSellProductCacheJSON(raw []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

// mergeOperatorSnapshot 用一次完整列表扫描结果替换当前账号的干员快照。
func mergeOperatorSnapshot(
	cache sellProductCache,
	uid string,
	scanCandidates []operatorCandidate,
	owned []string,
	now time.Time,
) sellProductCache {
	operatorSet := make(map[string]struct{}, len(owned))
	scanSet := operatorCandidateIDSet(scanCandidates)

	for _, name := range owned {
		if _, ok := scanSet[name]; ok {
			operatorSet[name] = struct{}{}
		}
	}

	return withOperatorSnapshot(cache, uid, operatorSet, now)
}

// sellProductCacheHasOperatorSnapshot 判断指定账号是否建立过完整干员快照。
// Operators 为 nil 表示只有据点状态；非 nil 且 IDs 为空表示完整扫描后没有相关干员。
func sellProductCacheHasOperatorSnapshot(cache sellProductCache, uid string) bool {
	account, ok := normalizeSellProductCache(cache).Accounts[uid]
	return ok && account.Operators != nil
}

// cachedOperatorIDsForUID 返回指定账号的规范化干员 ID 列表。
func cachedOperatorIDsForUID(cache sellProductCache, uid string) []string {
	account, ok := normalizeSellProductCache(cache).Accounts[uid]
	if !ok || account.Operators == nil {
		return nil
	}
	return account.Operators.IDs
}

// cachedOperatorUpdatedAtForUID 返回指定账号完整干员快照的扫描时间。
func cachedOperatorUpdatedAtForUID(cache sellProductCache, uid string) time.Time {
	account, ok := normalizeSellProductCache(cache).Accounts[uid]
	if !ok || account.Operators == nil {
		return time.Time{}
	}
	return account.Operators.UpdatedAt
}

// loadOutpostProsperityMaxLocations 从统一账号缓存中读取已满级据点。
func loadOutpostProsperityMaxLocations(uid string) (map[string]struct{}, error) {
	cache, err := readSellProductCache(resolveSellProductCachePathFunc())
	if err != nil {
		return nil, err
	}
	return outpostProsperityMaxLocationsForUID(cache, uid), nil
}

// persistOutpostProsperityStatus 把本次识别到的据点状态写回统一账号缓存。
func persistOutpostProsperityStatus(uid string, location string, reached bool) (bool, error) {
	return updateCachedOutpostProsperity(
		resolveSellProductCachePathFunc(),
		uid,
		location,
		reached,
	)
}

func outpostProsperityStatusesForUID(cache sellProductCache, uid string) map[string]bool {
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
) (bool, error) {
	sellProductCacheMu.Lock()
	defer sellProductCacheMu.Unlock()

	cache, err := readSellProductCache(path)
	if err != nil {
		return false, err
	}
	location = strings.TrimSpace(location)
	if location == "" {
		return false, fmt.Errorf("outpost prosperity location is empty")
	}
	if previous, ok := cache.Accounts[uid].Locations[location]; ok && previous == reached {
		return false, nil
	}

	account := cache.Accounts[uid]
	account.Locations = cloneBoolMap(account.Locations)
	if account.Locations == nil {
		account.Locations = map[string]bool{}
	}
	account.Locations[location] = reached
	if cache.Accounts == nil {
		cache.Accounts = map[string]sellProductCacheAccount{}
	}
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
	if cache.Accounts == nil {
		cache.Accounts = map[string]sellProductCacheAccount{}
	}
	account := cache.Accounts[uid]
	account.Operators = &sellProductOperatorSnapshot{
		UpdatedAt: now.UTC(),
		IDs:       sortedSetValues(operatorSet),
	}
	cache.Accounts[uid] = account
	return cache
}

// normalizeSellProductCache 对干员 ID 去重排序，并复制据点状态以避免共享可变 map。
// UID 在读取校验阶段必须已经规范，禁止在这里合并可能碰撞的账号。
func normalizeSellProductCache(cache sellProductCache) sellProductCache {
	normalized := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{},
	}
	for uid, account := range cache.Accounts {
		var operators *sellProductOperatorSnapshot
		if account.Operators != nil {
			operators = &sellProductOperatorSnapshot{
				UpdatedAt: account.Operators.UpdatedAt,
				IDs:       sortedSetValues(operatorIDSet(account.Operators.IDs)),
			}
		}
		normalized.Accounts[uid] = sellProductCacheAccount{
			Operators: operators,
			Locations: cloneBoolMap(account.Locations),
		}
	}
	if len(normalized.Accounts) == 0 {
		normalized.Accounts = nil
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

// isValidSellProductCacheUID 只接受 CaptureUID 生成的 16 位小写十六进制哈希或 unknown。
func isValidSellProductCacheUID(uid string) bool {
	if uid == sellProductCacheUnknownUID {
		return true
	}
	if len(uid) != 16 {
		return false
	}
	for _, r := range uid {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// operatorIDSet 将内部 ID 切片转换为去重集合，并忽略空 ID。
func operatorIDSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	return set
}

// operatorCandidateIDSet 提取候选域中的内部稳定 ID。
func operatorCandidateIDSet(candidates []operatorCandidate) map[string]struct{} {
	set := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Name == "" {
			continue
		}
		set[candidate.Name] = struct{}{}
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
