package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/rs/zerolog/log"
)

// taskInfo 任务定义中与显示名相关的信息。
type taskInfo struct {
	name     string // 任务名（如 AccountSwitch）
	labelKey string // i18n key（如 task.AccountSwitch.label）
}

var (
	taskNameOnce sync.Once
	taskByName   map[string]taskInfo // entry → 任务信息，进程内构建一次
)

// resolveTaskName 返回任务的显示名（i18n label，如 🔑自动切换账号）。
// 映射缺失或 label 查不到翻译时回退 entry 本身。
func resolveTaskName(entry string) string {
	if entry == "" {
		return ""
	}
	info, ok := loadTaskMap()[entry]
	if !ok {
		return entry
	}
	if label := i18n.T(info.labelKey); label != info.labelKey {
		return label
	}
	return entry
}

// loadTaskMap 懒加载 entry → 任务信息映射（进程内一次，之后缓存）。
func loadTaskMap() map[string]taskInfo {
	taskNameOnce.Do(func() {
		taskByName = scanTaskDir(resolveTaskDir())
	})
	return taskByName
}

// resolveTaskDir 定位任务定义目录：从 cwd 与 exe 目录逐级向上找 tasks/ 或 assets/tasks/。
// 运行时（install/tasks）与开发/测试（仓库 assets/tasks）均能命中。
func resolveTaskDir() string {
	roots := make([]string, 0, 16)
	seen := make(map[string]struct{})
	addRoots := func(start string) {
		if start == "" {
			return
		}
		dir := filepath.Clean(start)
		for depth := 0; depth < 6; depth++ {
			if _, ok := seen[dir]; !ok {
				roots = append(roots, dir)
				seen[dir] = struct{}{}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		addRoots(cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		addRoots(filepath.Dir(exePath))
	}
	for _, root := range roots {
		for _, rel := range []string{"tasks", "assets/tasks"} {
			candidate := filepath.Join(root, rel)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

// scanTaskDir 扫描 tasks/*.json 的 task 数组，建立 entry → taskInfo 映射。
// 非任务文件（无 task 顶层键）自动跳过。
func scanTaskDir(dir string) map[string]taskInfo {
	result := make(map[string]taskInfo)
	if dir == "" {
		return result
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn().Err(err).Str("component", "Notify").Str("dir", dir).Msg("failed to read tasks dir, {{task_name}} falls back to entry")
		return result
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var def struct {
			Task []struct {
				Name  string `json:"name"`
				Entry string `json:"entry"`
				Label string `json:"label"`
			} `json:"task"`
		}
		if err := json.Unmarshal(data, &def); err != nil {
			continue
		}
		for _, t := range def.Task {
			if t.Entry == "" || t.Name == "" {
				continue
			}
			labelKey := strings.TrimPrefix(t.Label, "$")
			if labelKey == "" {
				labelKey = "task." + t.Name + ".label"
			}
			result[t.Entry] = taskInfo{name: t.Name, labelKey: labelKey}
		}
	}
	return result
}
