package processcheck

import (
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const maaendExeName = "MaaEnd.exe"

type blacklistEntry struct {
	keyword      string
	displayName  string
	recommendKey string // i18n suffix under tasker.process_warning
}

// Keywords matched against process names via exact (case-sensitive) equality.
// Matches only warn (do not PostStop); session warns at most once.
var blacklist = []blacklistEntry{
	{"DNFAutoFire.exe", "DNFAutoFire.exe", "recommend_1"}, // 会让alt按键事件失效
	{"DAF连发工具.exe", "DAF连发工具.exe", "recommend_1"},         // 会让alt按键事件失效
	{"AltSnap.exe", "AltSnap.exe", "recommend_1"},         // 会让alt按键事件失效
	{"RTSS.exe", "RTSS.exe", "recommend_rtss"},            // MSI Afterburner / RivaTuner OSD 可能遮挡识别
}

// ProcessChecker warns once per session about blacklisted processes and multiple MaaEnd.exe instances.
type ProcessChecker struct {
	blacklistWarned bool
	instanceWarned  bool
}

// OnTaskerTask handles tasker task events
func (c *ProcessChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	if c.blacklistWarned && c.instanceWarned {
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking processes before task execution")

	found, maaendCount := scanProcesses()
	c.warnBlacklist(found)
	c.warnMultipleInstances(maaendCount)
}

func (c *ProcessChecker) warnBlacklist(found []blacklistEntry) {
	if c.blacklistWarned {
		return
	}
	if len(found) == 0 {
		log.Debug().Msg("Process check passed: no blacklisted processes found")
		return
	}

	names := make([]string, 0, len(found))
	recommendSeen := make(map[string]bool)
	var recommendLines []string
	for _, entry := range found {
		names = append(names, entry.displayName)
		key := entry.recommendKey
		if key == "" {
			key = "recommend_1"
		}
		if recommendSeen[key] {
			continue
		}
		recommendSeen[key] = true
		recommendLines = append(recommendLines, i18n.T("tasker.process_warning."+key))
	}

	log.Warn().
		Strs("processes", names).
		Msg("Blacklisted processes detected!")

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.process_warning", map[string]any{
			"ProcessNames":   strings.Join(names, ", "),
			"RecommendLines": recommendLines,
		}),
	)

	c.blacklistWarned = true
}

func (c *ProcessChecker) warnMultipleInstances(count int) {
	if c.instanceWarned {
		return
	}
	if count < 2 {
		log.Debug().
			Int("count", count).
			Msg("MaaEnd instance check passed")
		return
	}

	log.Warn().
		Int("count", count).
		Msg("Multiple MaaEnd.exe instances detected")

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.multi_instance_warning", map[string]any{
			"Count": count,
		}),
	)

	c.instanceWarned = true
}

func scanProcesses() (found []blacklistEntry, maaendCount int) {
	procs, err := process.Processes()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to enumerate processes")
		return nil, 0
	}

	seen := make(map[string]bool)
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		if strings.EqualFold(name, maaendExeName) {
			maaendCount++
		}
		for _, entry := range blacklist {
			if name == entry.keyword && !seen[entry.displayName] {
				seen[entry.displayName] = true
				found = append(found, entry)
			}
		}
	}

	return found, maaendCount
}
