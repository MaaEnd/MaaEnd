//go:build linux

package ziplineimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ziplineMark 是 Ziplines.json 里一条滑索记录。字段与 cpp-algo ZiplineStore 完全一致
// （template_id / level_id / x / y / z；x/z 张成水平面、y 是高度），保证 Go 写出、
// cpp-algo 的 ZiplineFrames / 滑索寻路能原样读回。这就是两侧共有的数据契约，用注释固定。
type ziplineMark struct {
	TemplateID string  `json:"template_id"`
	LevelID    string  `json:"level_id"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Z          float64 `json:"z"`
}

// ziplineMapRecord 是一张森空岛地图在某个账号下的全部滑索，按 (account_id, map_id) 整张替换，
// 不做逐条合并。
//
// account_id 是网页 roleId 经 captureuid 同款加盐哈希得到的伪匿名标识；空值表示旧版本遗留记录。
// 遗留记录不能自动归入当前账号，否则换号后仍会静默使用错误坐标。
type ziplineMapRecord struct {
	AccountID string        `json:"account_id,omitempty"`
	MapID     string        `json:"map_id"`
	FetchedAt string        `json:"fetched_at"`
	Marks     []ziplineMark `json:"marks"`
}

type recordFile struct {
	UpdatedAt string             `json:"updated_at"`
	Maps      []ziplineMapRecord `json:"maps"`
}

// exeDir 返回当前可执行文件所在目录（与 cpp 的 get_exe_dir 一致，作为资源锚点）。
func exeDir() string {
	exe, err := os.Executable()
	if err == nil && exe != "" {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// defaultRecordPath: <exe>/../debug/record/Ziplines.json，与 cpp ZiplineStore::DefaultPath 相同。
func defaultRecordPath() string {
	return filepath.Join(exeDir(), "..", "debug", "record", "Ziplines.json")
}

// load 读取现有记录。文件不存在或为空返回空库且无错误；内容坏掉才返回错误（此时拒绝覆盖，
// 避免把一个半截/损坏的现有记录冲掉，语义与 cpp ZiplineStore::load 一致）。
func loadRecord(path string) (*recordFile, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return &recordFile{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read record: %w", err)
	}
	if len(raw) == 0 {
		return &recordFile{}, nil
	}
	var rec recordFile
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("parse existing record: %w", err)
	}
	return &rec, nil
}

// replaceMap 按 (account_id, map_id) 整张替换，拆掉的滑索必须随之消失（与 cpp 一致）。
// 不同账号的同一张图、以及旧版无 account_id 的同名图都是不同的记录，互不覆盖。
func (r *recordFile) replaceMap(rec ziplineMapRecord) {
	for i := range r.Maps {
		if r.Maps[i].AccountID == rec.AccountID && r.Maps[i].MapID == rec.MapID {
			r.Maps[i] = rec
			return
		}
	}
	r.Maps = append(r.Maps, rec)
}

// save 先写临时文件再原子改名，导入中途崩掉不会把已有记录截成半截（与 cpp 一致）。
func (r *recordFile) save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create record dir: %w", err)
	}

	r.UpdatedAt = currentTimestampUTC()
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp record: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename temp record into place: %w", err)
	}
	return nil
}

func currentTimestampUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}
