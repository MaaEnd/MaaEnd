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

type trackData struct {
	Type       string   `json:"type"`
	ZoneID     string   `json:"zone_id,omitempty"`
	X          *float64 `json:"x,omitempty"`
	Y          *float64 `json:"y,omitempty"`
	PhaseID    string   `json:"phase_id,omitempty"`
	Components []string `json:"components,omitempty"`
}

type trackingBlock struct {
	ActualList []trackData `json:"actualList"`
}

type objectiveData struct {
	Description               missionText     `json:"description"`
	UseMultipleDescription    bool            `json:"useMultipleDescription"`
	MultipleDescription       []missionText   `json:"multipleDescription"`
	TrackingInfoList          []trackData     `json:"trackingInfoList"`
	MultiDescTrackingInfoList []trackingBlock `json:"multiDescTrackingInfoList"`
}

type questData struct {
	QuestID       string          `json:"questId"`
	ObjectiveList []objectiveData `json:"objectiveList"`
}

type missionData struct {
	MissionID             string               `json:"missionId"`
	BaseMissionImportance *int                 `json:"baseMissionImportance"`
	MissionName           missionText          `json:"missionName"`
	MainPathQuests        []string             `json:"mainPathQuests"`
	QuestDic              map[string]questData `json:"questDic"`
}

// textEntry 是某个任务的标题，variants 为非空且去重的简繁写法。
type textEntry struct {
	missionID string
	variants  []string
}

// objectiveEntry 是某个子步骤里的一句步骤名称。
// DescIndex 为 -1 表示普通目标；多说明目标则是 multipleDescription 的下标。
type objectiveEntry struct {
	QuestID   string      `json:"quest_id"`
	DescIndex int         `json:"desc_index"`
	Tracks    []trackData `json:"-"`
	variants  []string
}

// missionQuests 保存一个任务的全部步骤名称，以及主线顺序，供说明匹配到多条时消歧。
type missionQuests struct {
	pathOrder  map[string]int
	objectives []objectiveEntry
}

type missionIndex struct {
	titles     []textEntry
	importance map[string]int
	quests     map[string]*missionQuests
}

var (
	indexOnce sync.Once
	index     *missionIndex
	indexErr  error
)

// getMissionIndex 首次调用时读取 missions.json 并建立标题、步骤名称索引，之后直接返回缓存。
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
			Int("missions", len(index.quests)).
			Msg("missions loaded")
	})
	return index, indexErr
}

func buildMissionIndex() (*missionIndex, error) {
	var missions map[string]missionData
	if err := resource.ReadJsonResource(missionsPath, &missions); err != nil {
		return nil, err
	}

	idx := &missionIndex{
		importance: make(map[string]int, len(missions)),
		quests:     make(map[string]*missionQuests, len(missions)),
	}
	for key, m := range missions {
		id := m.MissionID
		if id == "" {
			id = key
		}
		if m.BaseMissionImportance != nil {
			idx.importance[id] = *m.BaseMissionImportance
		}
		if v := variantsOf(m.MissionName); len(v) > 0 {
			idx.titles = append(idx.titles, textEntry{missionID: id, variants: v})
		}
		idx.quests[id] = indexQuests(m)
	}
	return idx, nil
}

func indexQuests(m missionData) *missionQuests {
	quests := &missionQuests{
		pathOrder: make(map[string]int, len(m.MainPathQuests)),
	}
	for i, questID := range m.MainPathQuests {
		quests.pathOrder[questID] = i
	}
	for key, quest := range m.QuestDic {
		id := quest.QuestID
		if id == "" {
			id = key
		}
		for _, objective := range quest.ObjectiveList {
			quests.objectives = append(quests.objectives, objectiveEntries(id, objective)...)
		}
	}
	return quests
}

func objectiveEntries(questID string, objective objectiveData) []objectiveEntry {
	if !objective.UseMultipleDescription {
		variants := variantsOf(objective.Description)
		if len(variants) == 0 {
			return nil
		}
		return []objectiveEntry{{
			QuestID:   questID,
			DescIndex: -1,
			Tracks:    objective.TrackingInfoList,
			variants:  variants,
		}}
	}

	entries := make([]objectiveEntry, 0, len(objective.MultipleDescription))
	for i, text := range objective.MultipleDescription {
		variants := variantsOf(text)
		if len(variants) == 0 {
			continue
		}
		var tracks []trackData
		if i < len(objective.MultiDescTrackingInfoList) {
			tracks = objective.MultiDescTrackingInfoList[i].ActualList
		}
		entries = append(entries, objectiveEntry{
			QuestID:   questID,
			DescIndex: i,
			Tracks:    tracks,
			variants:  variants,
		})
	}
	return entries
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
