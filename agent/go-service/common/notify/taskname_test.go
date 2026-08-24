package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// TestScanTaskDir 验证从任务定义文件构建 entry → taskInfo 映射。
func TestScanTaskDir(t *testing.T) {
	// 构造临时目录模拟 tasks/
	dir := t.TempDir()
	files := map[string]string{
		"TaskA.json": `{
			"task": [
				{ "name": "TaskA", "entry": "TaskAStart", "label": "$task.TaskA.label" },
				{ "name": "TaskA2", "entry": "TaskA2Start", "label": "" }
			]
		}`,
		"TaskB.json": `{
			"task": [ { "name": "TaskB", "entry": "TaskBStart", "label": "$task.TaskB.label" } ]
		}`,
		// 无 task 顶层键的文件应被跳过
		"NotATask.json": `{ "option": { "x": {} } }`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	m := scanTaskDir(dir)
	checks := []struct {
		entry, name, labelKey string
		want                  bool
	}{
		{"TaskAStart", "TaskA", "task.TaskA.label", true},
		{"TaskA2Start", "TaskA2", "task.TaskA2.label", true}, // label 为空 → 退化 task.<name>.label
		{"TaskBStart", "TaskB", "task.TaskB.label", true},
		{"NoSuchEntry", "", "", false},
	}
	for _, c := range checks {
		info, ok := m[c.entry]
		if ok != c.want {
			t.Errorf("entry %q present = %v, want %v", c.entry, ok, c.want)
			continue
		}
		if ok && (info.name != c.name || info.labelKey != c.labelKey) {
			t.Errorf("entry %q = %+v, want name=%q labelKey=%q", c.entry, info, c.name, c.labelKey)
		}
	}
}

// TestResolveTaskName 验证显示名解析与回退逻辑（端到端：真实映射 + 真实 i18n 翻译）。
func TestResolveTaskName(t *testing.T) {
	t.Setenv("PI_CLIENT_LANGUAGE", "zh_cn")
	i18n.Init() // 幂等；首次以 zh_cn 初始化，使 task.*.label 翻译可用

	// 真实任务：AccountSwitch → 🔑自动切换账号
	if got := resolveTaskName("AccountSwitchStart"); got != "🔑自动切换账号" {
		t.Errorf("resolveTaskName(AccountSwitchStart) = %q, want 🔑自动切换账号", got)
	}
	// 无映射 → 回退 entry
	if got := resolveTaskName("NoSuchEntryXyz"); got != "NoSuchEntryXyz" {
		t.Errorf("unknown entry should fall back, got %q", got)
	}
	// 空 → 空
	if got := resolveTaskName(""); got != "" {
		t.Errorf("empty entry should stay empty, got %q", got)
	}
}
