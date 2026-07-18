package sellproduct

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
)

const (
	// 所有账号共享同一个 JSON 文件，并由 Accounts 按 UID 隔离快照。
	operatorCacheFilePrefix = "SellProductOwnedOperators"
	operatorCacheFileExt    = ".json"
	// 尚未捕获 UID 时仍允许使用临时分区，避免把空字符串作为 map 键写入文件。
	operatorCacheUnknownUID    = "unknown"
	operatorCacheSchemaVersion = 1
)

// resolveOperatorCachePathFunc 是单元测试替换缓存目录的注入点。
var resolveOperatorCachePathFunc = defaultOperatorCachePath

// operatorCacheFile 是拥有干员缓存的顶层格式。
// UpdatedAt 记录最后一次任意账号更新，Accounts 中则保存各账号独立快照。
type operatorCacheFile struct {
	SchemaVersion int                             `json:"schema_version"`
	UpdatedAt     string                          `json:"updated_at"`
	Accounts      map[string]operatorCacheAccount `json:"accounts,omitempty"`
}

// operatorCacheAccount 保存一个账号已经确认拥有的相关干员。
// Operators 使用稳定 CacheName，并在写盘前排序，避免产生无意义的文件 diff。
type operatorCacheAccount struct {
	UpdatedAt string   `json:"updated_at"`
	Operators []string `json:"operators"`
	Complete  bool     `json:"complete"`
}

// currentOperatorCacheUID 获取 CaptureUID 模块最近识别到的账号并规范化为安全键名。
func currentOperatorCacheUID() string {
	return normalizeOperatorCacheUID(captureuid.GetCachedUID())
}

// defaultOperatorCachePath 返回运行记录目录中的统一缓存文件路径。
// uid 由文件内部 Accounts 分区，因此不参与文件名拼接。
func defaultOperatorCachePath(uid string) string {
	return filepath.Join("debug", "record", operatorCacheFilePrefix+operatorCacheFileExt)
}

// readOperatorCache 读取并规范化缓存；文件不存在或为空视为尚未建立快照，而不是错误。
func readOperatorCache(path string) (operatorCacheFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return operatorCacheFile{}, nil
		}
		return operatorCacheFile{}, fmt.Errorf("read operator cache: %w", err)
	}
	if len(raw) == 0 {
		return operatorCacheFile{}, nil
	}

	var cache operatorCacheFile
	if err := json.Unmarshal(raw, &cache); err != nil {
		return operatorCacheFile{}, fmt.Errorf("parse operator cache: %w", err)
	}
	return normalizeOperatorCacheFile(cache), nil
}

// writeOperatorCache 用单账号数据创建全新的缓存文件。
// 该函数主要用于初始化和测试；运行时增量更新应通过 merge* 后写入，避免覆盖其他账号。
func writeOperatorCache(path string, uid string, operators []string, now time.Time) error {
	uid = normalizeOperatorCacheUID(uid)
	updatedAt := now.UTC().Format(time.RFC3339)

	cache := operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		UpdatedAt:     updatedAt,
		Accounts: map[string]operatorCacheAccount{
			uid: {
				UpdatedAt: updatedAt,
				Operators: sortedSetValues(operatorNameSet(operators)),
				Complete:  true,
			},
		},
	}
	return writeOperatorCacheFile(path, cache)
}

// writeOperatorCacheFile 规范化并格式化缓存，然后使用原子替换方式写盘。
func writeOperatorCacheFile(path string, cache operatorCacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create operator cache dir: %w", err)
	}
	cache = normalizeOperatorCacheFile(cache)
	raw, err := json.MarshalIndent(cache, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal operator cache: %w", err)
	}
	raw = append(raw, '\n')
	if err := writeOperatorCacheAtomic(path, raw, 0644); err != nil {
		return fmt.Errorf("write operator cache: %w", err)
	}
	return nil
}

// mergeOperatorCache 用一次完整列表扫描结果替换“本次可扫描候选域”内的缓存。
// 算法先从旧快照删除 scanCandidates，再仅写回本轮实际识别到的 owned；候选域之外
// 的记录保持不变。这样既能移除已不再拥有的干员，也不会误删本轮资源未覆盖的记录。
func mergeOperatorCache(
	cache operatorCacheFile,
	uid string,
	scanCandidates []operatorCandidate,
	owned []string,
	now time.Time,
) operatorCacheFile {
	uid = normalizeOperatorCacheUID(uid)
	operatorSet := operatorNameSet(operatorCacheOperatorsForUID(cache, uid))
	scanSet := operatorCandidateCacheNameSet(scanCandidates)

	for name := range scanSet {
		delete(operatorSet, name)
	}
	for _, name := range owned {
		if _, ok := scanSet[name]; ok {
			operatorSet[name] = struct{}{}
		}
	}

	return withOperatorCacheAccount(cache, uid, operatorSet, true, now)
}

// mergeObservedOperatorCache 合并一次局部观察结果。
// 局部识别只能证明“拥有”，不能证明未出现的干员“不拥有”，因此这里只做集合加法。
func mergeObservedOperatorCache(cache operatorCacheFile, uid string, observed []string, now time.Time) operatorCacheFile {
	uid = normalizeOperatorCacheUID(uid)
	operatorSet := operatorNameSet(operatorCacheOperatorsForUID(cache, uid))
	complete := operatorCacheHasSnapshot(cache, uid)
	for _, name := range observed {
		if name == "" {
			continue
		}
		operatorSet[name] = struct{}{}
	}

	return withOperatorCacheAccount(cache, uid, operatorSet, complete, now)
}

// operatorCacheHasSnapshot 判断指定账号是否建立过快照。
// 即使 Operators 为空，只要有更新时间也代表已经完成过一次有效的空列表扫描。
func operatorCacheHasSnapshot(cache operatorCacheFile, uid string) bool {
	uid = normalizeOperatorCacheUID(uid)
	account, ok := normalizeOperatorCacheFile(cache).Accounts[uid]
	return ok && account.Complete
}

// operatorCacheOperatorsForUID 返回指定账号的规范化干员列表。
func operatorCacheOperatorsForUID(cache operatorCacheFile, uid string) []string {
	uid = normalizeOperatorCacheUID(uid)
	account, ok := normalizeOperatorCacheFile(cache).Accounts[uid]
	if !ok {
		return nil
	}
	return account.Operators
}

// withOperatorCacheAccount 把集合写回指定账号，并同步账号级和文件级更新时间。
func withOperatorCacheAccount(
	cache operatorCacheFile,
	uid string,
	operatorSet map[string]struct{},
	complete bool,
	now time.Time,
) operatorCacheFile {
	cache = normalizeOperatorCacheFile(cache)
	uid = normalizeOperatorCacheUID(uid)
	updatedAt := now.UTC().Format(time.RFC3339)
	cache.SchemaVersion = operatorCacheSchemaVersion
	cache.UpdatedAt = updatedAt
	if cache.Accounts == nil {
		cache.Accounts = map[string]operatorCacheAccount{}
	}
	cache.Accounts[uid] = operatorCacheAccount{
		UpdatedAt: updatedAt,
		Operators: sortedSetValues(operatorSet),
		Complete:  complete,
	}
	return cache
}

// normalizeOperatorCacheFile 修复可兼容的旧数据并消除不稳定表示：
// 规范 UID、合并碰撞账号、去重并排序干员，同时补齐已有 Accounts 的 schema 版本。
func normalizeOperatorCacheFile(cache operatorCacheFile) operatorCacheFile {
	normalized := operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		UpdatedAt:     strings.TrimSpace(cache.UpdatedAt),
		Accounts:      map[string]operatorCacheAccount{},
	}
	for uid, account := range cache.Accounts {
		uid = normalizeOperatorCacheUID(uid)
		operatorSet := operatorNameSet(account.Operators)
		existing := normalized.Accounts[uid]
		for _, name := range existing.Operators {
			operatorSet[name] = struct{}{}
		}
		updatedAt := strings.TrimSpace(account.UpdatedAt)
		if updatedAt == "" {
			updatedAt = existing.UpdatedAt
		}
		normalized.Accounts[uid] = operatorCacheAccount{
			UpdatedAt: updatedAt,
			Operators: sortedSetValues(operatorSet),
			Complete:  account.Complete || existing.Complete,
		}
	}
	if len(normalized.Accounts) == 0 {
		normalized.Accounts = nil
	}
	return normalized
}

// writeOperatorCacheAtomic 先在目标目录写入临时文件并刷盘，再原子重命名覆盖正式文件。
// 任一步失败都会清理临时文件，防止进程中断留下半截 JSON 破坏后续任务。
func writeOperatorCacheAtomic(path string, content []byte, perm os.FileMode) error {
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

// normalizeOperatorCacheUID 把 UID 限制为适合作为持久化 map 键的 ASCII 字符集合。
// 非法字符统一替换为下划线；空 UID 使用 unknown 分区，后续捕获真实 UID 后自然隔离。
func normalizeOperatorCacheUID(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return operatorCacheUnknownUID
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
		return operatorCacheUnknownUID
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
