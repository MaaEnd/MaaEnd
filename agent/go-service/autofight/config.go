package autofight

import (
	"fmt"
	"sort"
	"sync"
)

// FightConfig stores the parsed battle strategy for auto-fighting.
type FightConfig struct {
	ActiveScenarioID string
	ScenarioName     string
	DataCode         string // Keep track of the source data code to avoid redundant reloading
	Tracks           []EndaxisTrack
	// Processed sequence of actions can be added here in the future
}

var (
	currentConfig *FightConfig
	configMutex   sync.RWMutex
)

// LoadConfig decodes the given Endaxis data code and sets it as the active configuration.
func LoadConfig(dataCode string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	// Avoid redundant loading if it's the exact same data code
	if currentConfig != nil && currentConfig.DataCode == dataCode {
		return nil
	}

	if dataCode == "" {
		currentConfig = nil
		return nil
	}

	project, err := DecodeDataCode(dataCode)
	if err != nil {
		return fmt.Errorf("failed to decode Endaxis data code: %w", err)
	}

	var activeScenario *EndaxisScenario
	for _, sc := range project.ScenarioList {
		if sc.ID == project.ActiveScenarioID {
			activeScenario = &sc
			break
		}
	}

	if activeScenario == nil {
		if len(project.ScenarioList) > 0 {
			activeScenario = &project.ScenarioList[0]
		} else {
			return fmt.Errorf("no valid scenario found in Endaxis data")
		}
	}

	// Sort actions by start time for easier processing later
	for i := range activeScenario.Data.Tracks {
		sort.Slice(activeScenario.Data.Tracks[i].Actions, func(j, k int) bool {
			return activeScenario.Data.Tracks[i].Actions[j].StartTime < activeScenario.Data.Tracks[i].Actions[k].StartTime
		})
	}

	currentConfig = &FightConfig{
		ActiveScenarioID: activeScenario.ID,
		ScenarioName:     activeScenario.Name,
		DataCode:         dataCode,
		Tracks:           activeScenario.Data.Tracks,
	}

	return nil
}

// GetConfig returns the currently active fight configuration.
func GetConfig() *FightConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return currentConfig
}

// HasConfig returns true if a valid configuration is loaded.
func HasConfig() bool {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return currentConfig != nil
}

// ClearConfig removes the current configuration.
func ClearConfig() {
	configMutex.Lock()
	defer configMutex.Unlock()
	currentConfig = nil
}
