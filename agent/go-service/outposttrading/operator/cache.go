package operator

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
	"github.com/MaaXYZ/MaaEnd/agent/go-service/outposttrading/internal/selectiondata"
)

const (
	// 所有账号共享同一个 JSON 文件，并由 Accounts 按 UID 隔离状态。
	outpostTradingCacheFileName = "OutpostTradingCache.json"
	// 尚未捕获 UID 时仍允许使用临时分区，避免把空字符串作为 map 键写入文件。
	outpostTradingCacheUnknownUID = "unknown"
)

// resolveOutpostTradingCachePathFunc 是单元测试替换缓存目录的注入点。
var resolveOutpostTradingCachePathFunc = defaultOutpostTradingCachePath

// outpostTradingCacheMu 串行化同一账号缓存的读取-修改-写入，防止干员与据点状态互相覆盖。
var outpostTradingCacheMu sync.Mutex

// outpostTradingCache 是 OutpostTrading 持久缓存的顶层格式。
// Accounts 按 UID 同时保存完整干员快照和据点发展值状态。
type outpostTradingCache struct {
	Accounts map[string]outpostTradingCacheAccount `json:"accounts,omitempty"`
}

// outpostTradingCacheEnvelope 延迟解析账号对象，使单个账号损坏时仍能保留其他账号。
type outpostTradingCacheEnvelope struct {
	Accounts json.RawMessage `json:"accounts,omitempty"`
}

// outpostTradingCacheAccount 保存一个账号的完整干员快照和各据点发展值状态。
// Operators 为 nil 表示尚未完成扫描；非 nil 且 IDs 为空表示完整扫描后没有相关干员。
type outpostTradingCacheAccount struct {
	Operators *outpostTradingOperatorSnapshot `json:"operators,omitempty"`
	Locations map[string]bool                 `json:"locations,omitempty"`
}

// outpostTradingOperatorSnapshot 把完整干员集合与其扫描时间绑定，避免据点状态更新污染干员缓存时间。
type outpostTradingOperatorSnapshot struct {
	UpdatedAt time.Time `json:"updated_at"`
	IDs       []string  `json:"ids"`
}

// currentOutpostTradingCacheUID 获取 CaptureUID 模块生成的加盐哈希；尚未捕获时使用 unknown 分区。
func currentOutpostTradingCacheUID() string {
	uid := captureuid.GetCachedUID(captureuid.OutputTypeHashed)
	if uid == "" {
		return outpostTradingCacheUnknownUID
	}
	return uid
}

// defaultOutpostTradingCachePath 返回运行记录目录中的统一缓存文件路径。
func defaultOutpostTradingCachePath() string {
	return filepath.Join("debug", "record", outpostTradingCacheFileName)
}

// readOutpostTradingCache 读取并规范化缓存。
// 顶层结构损坏时整份缓存视为不存在；单个账号不合法时只丢弃对应账号。
func readOutpostTradingCache(path string) (outpostTradingCache, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return outpostTradingCache{}, nil
		}
		return outpostTradingCache{}, fmt.Errorf("read outpost trading cache: %w", err)
	}
	if len(raw) == 0 {
		return outpostTradingCache{}, nil
	}

	var envelope outpostTradingCacheEnvelope
	if !decodeStrictOutpostTradingCacheJSON(raw, &envelope) {
		return outpostTradingCache{}, nil
	}
	if len(envelope.Accounts) == 0 {
		return outpostTradingCache{}, nil
	}
	accountsRaw := bytes.TrimSpace(envelope.Accounts)
	if len(accountsRaw) == 0 || accountsRaw[0] != '{' {
		return outpostTradingCache{}, nil
	}

	var accounts map[string]json.RawMessage
	if !decodeStrictOutpostTradingCacheJSON(accountsRaw, &accounts) {
		return outpostTradingCache{}, nil
	}
	data, err := loadSelectionData()
	if err != nil {
		return outpostTradingCache{}, fmt.Errorf("validate outpost trading cache accounts: %w", err)
	}
	cache := outpostTradingCache{Accounts: map[string]outpostTradingCacheAccount{}}
	for uid, accountRaw := range accounts {
		if !isValidOutpostTradingCacheUID(uid) {
			continue
		}
		accountJSON := bytes.TrimSpace(accountRaw)
		if len(accountJSON) == 0 || accountJSON[0] != '{' {
			continue
		}
		var account outpostTradingCacheAccount
		if !decodeStrictOutpostTradingCacheJSON(accountJSON, &account) {
			continue
		}
		if !outpostTradingCacheAccountIsValid(account, data) {
			continue
		}
		cache.Accounts[uid] = account
	}
	return normalizeOutpostTradingCache(cache), nil
}

// writeOutpostTradingCache 规范化并格式化缓存，然后使用原子替换方式写盘。
func writeOutpostTradingCache(path string, cache outpostTradingCache) error {
	cache = normalizeOutpostTradingCache(cache)
	valid, err := outpostTradingCacheIsValid(cache)
	if err != nil {
		return fmt.Errorf("validate outpost trading cache: %w", err)
	}
	if !valid {
		return fmt.Errorf("validate outpost trading cache: invalid structure")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create outpost trading cache dir: %w", err)
	}
	raw, err := json.MarshalIndent(cache, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal outpost trading cache: %w", err)
	}
	raw = append(raw, '\n')
	if err := writeOutpostTradingCacheAtomic(path, raw, 0644); err != nil {
		return fmt.Errorf("write outpost trading cache: %w", err)
	}
	return nil
}

// outpostTradingCacheIsValid 验证待写入缓存的 UID 和每个账号都符合当前严格结构。
func outpostTradingCacheIsValid(cache outpostTradingCache) (bool, error) {
	data, err := loadSelectionData()
	if err != nil {
		return false, err
	}
	for uid, account := range cache.Accounts {
		if !isValidOutpostTradingCacheUID(uid) {
			return false, nil
		}
		if !outpostTradingCacheAccountIsValid(account, data) {
			return false, nil
		}
	}
	return true, nil
}

func outpostTradingCacheAccountIsValid(
	account outpostTradingCacheAccount,
	data *selectiondata.File,
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

func decodeStrictOutpostTradingCacheJSON(raw []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

// mergeOperatorSnapshot 用一次完整列表扫描结果替换当前账号的干员快照。
func mergeOperatorSnapshot(
	cache outpostTradingCache,
	uid string,
	scanCandidates []operatorCandidate,
	owned []string,
	now time.Time,
) outpostTradingCache {
	operatorSet := make(map[string]struct{}, len(owned))
	scanSet := operatorCandidateIDSet(scanCandidates)

	for _, name := range owned {
		if _, ok := scanSet[name]; ok {
			operatorSet[name] = struct{}{}
		}
	}

	return withOperatorSnapshot(cache, uid, operatorSet, now)
}

// invalidateOperatorSnapshotForUID 移除指定账号的干员快照并保留据点状态。
// 当前派驻干员不在快照中时说明快照已过期，移除后既有扫描流程会重新完整扫描。
func invalidateOperatorSnapshotForUID(path string, uid string) error {
	outpostTradingCacheMu.Lock()
	defer outpostTradingCacheMu.Unlock()
	cache, err := readOutpostTradingCache(path)
	if err != nil {
		return err
	}
	account, ok := cache.Accounts[uid]
	if !ok || account.Operators == nil {
		return nil
	}
	account.Operators = nil
	cache.Accounts[uid] = account
	return writeOutpostTradingCache(path, cache)
}

// outpostTradingCacheHasOperatorSnapshot 判断指定账号是否建立过完整干员快照。
// Operators 为 nil 表示只有据点状态；非 nil 且 IDs 为空表示完整扫描后没有相关干员。
func outpostTradingCacheHasOperatorSnapshot(cache outpostTradingCache, uid string) bool {
	account, ok := normalizeOutpostTradingCache(cache).Accounts[uid]
	return ok && account.Operators != nil
}

// cachedOperatorIDsForUID 返回指定账号的规范化干员 ID 列表。
func cachedOperatorIDsForUID(cache outpostTradingCache, uid string) []string {
	account, ok := normalizeOutpostTradingCache(cache).Accounts[uid]
	if !ok || account.Operators == nil {
		return nil
	}
	return account.Operators.IDs
}

// cachedOperatorUpdatedAtForUID 返回指定账号完整干员快照的扫描时间。
func cachedOperatorUpdatedAtForUID(cache outpostTradingCache, uid string) time.Time {
	account, ok := normalizeOutpostTradingCache(cache).Accounts[uid]
	if !ok || account.Operators == nil {
		return time.Time{}
	}
	return account.Operators.UpdatedAt
}

// loadOutpostProsperityMaxLocations 从统一账号缓存中读取已满级据点。
func loadOutpostProsperityMaxLocations(uid string) (map[string]struct{}, error) {
	cache, err := readOutpostTradingCache(resolveOutpostTradingCachePathFunc())
	if err != nil {
		return nil, err
	}
	return outpostProsperityMaxLocationsForUID(cache, uid), nil
}

// persistOutpostProsperityStatus 把本次识别到的据点状态写回统一账号缓存。
func persistOutpostProsperityStatus(uid string, location string, reached bool) (bool, error) {
	return updateCachedOutpostProsperity(
		resolveOutpostTradingCachePathFunc(),
		uid,
		location,
		reached,
	)
}

func outpostProsperityStatusesForUID(cache outpostTradingCache, uid string) map[string]bool {
	account, ok := normalizeOutpostTradingCache(cache).Accounts[uid]
	if !ok {
		return nil
	}
	return cloneBoolMap(account.Locations)
}

func outpostProsperityMaxLocationsForUID(cache outpostTradingCache, uid string) map[string]struct{} {
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
	outpostTradingCacheMu.Lock()
	defer outpostTradingCacheMu.Unlock()

	cache, err := readOutpostTradingCache(path)
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
		cache.Accounts = map[string]outpostTradingCacheAccount{}
	}
	cache.Accounts[uid] = account
	if err := writeOutpostTradingCache(path, cache); err != nil {
		return false, err
	}
	return true, nil
}

// withOperatorSnapshot 把完整干员集合写回指定账号，并保留同账号的据点状态。
func withOperatorSnapshot(
	cache outpostTradingCache,
	uid string,
	operatorSet map[string]struct{},
	now time.Time,
) outpostTradingCache {
	cache = normalizeOutpostTradingCache(cache)
	if cache.Accounts == nil {
		cache.Accounts = map[string]outpostTradingCacheAccount{}
	}
	account := cache.Accounts[uid]
	account.Operators = &outpostTradingOperatorSnapshot{
		UpdatedAt: now.UTC(),
		IDs:       sortedSetValues(operatorSet),
	}
	cache.Accounts[uid] = account
	return cache
}

// normalizeOutpostTradingCache 对干员 ID 去重排序，并复制据点状态以避免共享可变 map。
// UID 在读取校验阶段必须已经规范，禁止在这里合并可能碰撞的账号。
func normalizeOutpostTradingCache(cache outpostTradingCache) outpostTradingCache {
	normalized := outpostTradingCache{
		Accounts: map[string]outpostTradingCacheAccount{},
	}
	for uid, account := range cache.Accounts {
		var operators *outpostTradingOperatorSnapshot
		if account.Operators != nil {
			operators = &outpostTradingOperatorSnapshot{
				UpdatedAt: account.Operators.UpdatedAt,
				IDs:       sortedSetValues(operatorIDSet(account.Operators.IDs)),
			}
		}
		normalized.Accounts[uid] = outpostTradingCacheAccount{
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

// writeOutpostTradingCacheAtomic 先在目标目录写入临时文件并刷盘，再原子重命名覆盖正式文件。
// 任一步失败都会清理临时文件，防止进程中断留下半截 JSON 破坏后续任务。
func writeOutpostTradingCacheAtomic(path string, content []byte, perm os.FileMode) error {
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

// isValidOutpostTradingCacheUID 只接受 CaptureUID 生成的 16 位小写十六进制哈希或 unknown。
func isValidOutpostTradingCacheUID(uid string) bool {
	if uid == outpostTradingCacheUnknownUID {
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
