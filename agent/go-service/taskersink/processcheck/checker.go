package processcheck

import (
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

type blacklistEntry struct {
	keyword     string
	displayName string
}

// Keywords matched against process names via exact (case-sensitive) equality.
var blacklist = []blacklistEntry{
	{"RTSSHooksLoader64.exe", "RTSSHooksLoader64"},
	{"RTSSHooksLoader.exe", "RTSSHooksLoader"},
}

// ProcessChecker detects blacklisted processes before task execution
type ProcessChecker struct {
	warned bool
}

// OnTaskerTask handles tasker task events
func (c *ProcessChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}

	if c.warned {
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking for blacklisted processes before task execution")

	found := checkBlacklistedProcesses()
	if len(found) == 0 {
		log.Debug().Msg("Process check passed: no blacklisted processes found")
		return
	}

	log.Warn().
		Strs("processes", found).
		Msg("Blacklisted processes detected!")

	names := strings.Join(found, ", ")
	fmt.Println(i18n.RenderHTML("tasker.process_warning", map[string]any{"ProcessNames": names}))

	c.warned = true
}

func checkBlacklistedProcesses() []string {
	procs, err := process.Processes()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to enumerate processes")
		return nil
	}

	seen := make(map[string]bool)
	var found []string

	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		for _, entry := range blacklist {
			if name == entry.keyword && !seen[entry.displayName] {
				seen[entry.displayName] = true
				found = append(found, entry.displayName)
			}
		}
	}

	return found
}
