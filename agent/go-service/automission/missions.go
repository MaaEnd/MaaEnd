package automission

import (
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

const missionsPath = "data/AutoMission/missions.json"

type missionText struct {
	ZhCN string `json:"zh_cn"`
	ZhTW string `json:"zh_tw"`
}

type missionData struct {
	MissionID          string      `json:"missionId"`
	MissionName        missionText `json:"missionName"`
	MissionDescription missionText `json:"missionDescription"`
}

// textEntry 是某个任务的一段可匹配文本，variants 为非空且去重的简繁写法。
type textEntry struct {
	missionID string
	variants  []string
}

type missionIndex struct {
	titles       []textEntry
	descriptions []textEntry
}

var (
	indexOnce sync.Once
	index     *missionIndex
	indexErr  error
)

// getMissionIndex 首次调用时读取 missions.json 并建立标题、说明索引，之后直接返回缓存。
func getMissionIndex() (*missionIndex, error) {
	indexOnce.Do(func() {
		index, indexErr = buildMissionIndex()
		if indexErr != nil {
			log.Error().
				Err(indexErr).
				Str("component", componentName).
				Str("path", missionsPath).
				Msg("load missions failed")
			return
		}
		log.Info().
			Str("component", componentName).
			Int("titles", len(index.titles)).
			Int("descriptions", len(index.descriptions)).
			Msg("missions loaded")
	})
	return index, indexErr
}

func buildMissionIndex() (*missionIndex, error) {
	var missions map[string]missionData
	if err := resource.ReadJsonResource(missionsPath, &missions); err != nil {
		return nil, err
	}

	idx := &missionIndex{}
	for key, m := range missions {
		id := m.MissionID
		if id == "" {
			id = key
		}
		if v := variantsOf(m.MissionName); len(v) > 0 {
			idx.titles = append(idx.titles, textEntry{missionID: id, variants: v})
		}
		if v := variantsOf(m.MissionDescription); len(v) > 0 {
			idx.descriptions = append(idx.descriptions, textEntry{missionID: id, variants: v})
		}
	}
	return idx, nil
}

func variantsOf(t missionText) []string {
	cn := strings.TrimSpace(t.ZhCN)
	tw := strings.TrimSpace(t.ZhTW)
	switch {
	case cn == "" && tw == "":
		return nil
	case cn == "" || cn == tw:
		return []string{tw}
	case tw == "":
		return []string{cn}
	default:
		return []string{cn, tw}
	}
}
