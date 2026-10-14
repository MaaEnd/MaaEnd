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
	Type       string      `json:"type"`
	ZoneID     string      `json:"zone_id,omitempty"`
	X          *float64    `json:"x,omitempty"`
	Y          *float64    `json:"y,omitempty"`
	PhaseID    string      `json:"phase_id,omitempty"`
	Components []string    `json:"components,omitempty"`
	NpcName    missionText `json:"npcName,omitempty"`
	// NpcNameOverrides 是关卡脚本运行时给 NPC 改的名字，与 NpcName 一起作为交互名称候选。
	NpcNameOverrides []missionText `json:"npcNameOverrides,omitempty"`
	// EntityDataIDKey 是实体追踪对应的解包实体类型，如 int_fixable_robot。
	EntityDataIDKey string `json:"entityDataIdKey,omitempty"`
	// InteractTexts 是实体追踪的交互选项文字，可能有多个。
	InteractTexts []missionText `json:"interactTexts,omitempty"`
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
	QuestID         string          `json:"questId"`
	PrevQuestIDList []string        `json:"prevQuestIdList"`
	ObjectiveList   []objectiveData `json:"objectiveList"`
}

type missionData struct {
	MissionID             string               `json:"missionId"`
	BaseMissionImportance *int                 `json:"baseMissionImportance"`
	MissionName           missionText          `json:"missionName"`
	QuestDic              map[string]questData `json:"questDic"`
}

// textEntry 是某个任务的标题，variants 为非空且去重的简繁写法。
type textEntry struct {
	missionID string
	variants  []string
}

// objectiveEntry 是某个子步骤里的一句步骤名称。
// DescIndex 为 -1 表示普通目标；多说明目标则是 multipleDescription 的下标。
// Order 是所属子步骤在前置链上的深度，同名步骤按它排列候选。
type objectiveEntry struct {
	QuestID   string      `json:"quest_id"`
	DescIndex int         `json:"desc_index"`
	Order     int         `json:"-"`
	Tracks    []trackData `json:"-"`
	variants  []string
}

// missionQuests 保存一个任务的全部步骤名称。
type missionQuests struct {
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
	quests := &missionQuests{}
	order := questOrder(m)
	for key, quest := range m.QuestDic {
		id := quest.QuestID
		if id == "" {
			id = key
		}
		for _, objective := range quest.ObjectiveList {
			entries := objectiveEntries(id, objective)
			for i := range entries {
				entries[i].Order = order[id]
			}
			quests.objectives = append(quests.objectives, entries...)
		}
	}
	return quests
}

// questOrder 按 prevQuestIdList 算每个子步骤的深度，用来给同名步骤排先后。
// 解包数据里 questDic 按 questId 字符串排序，与实际流程顺序无关，只有前置关系可靠。
//
// 深度就是沿 prev 往前追的层数，即离起点有多远：自己的深度 = prev 的深度 + 1，
// 没有 prev 的起点为 0。以「驮兽与我」(sm2l2m1) 为例：
//
//	q#15  prev 无       → 深 0（起点）
//	q#1   prev = q#15   → 深 1
//	q#2   prev = q#1    → 深 2
//	q#17  prev = q#2    → 深 3
//	q#3   prev = q#17   → 深 4
//	q#4   prev = q#3    → 深 5
//	q#18  prev = q#4    → 深 6
//
// 同名的 q#2 / q#17、q#4 / q#18 按深度排出先后；按 questId 排会把 q#17 排在 q#2 前面。
//
// 边界情况：
//   - 有多个 prev 时取最深的那个再加 1，即所有前置都完成后才轮到它；
//   - prev 指向本任务以外的 quest 时忽略；
//   - prev 成环时，遇到正在计算的步骤按 0 处理，避免无限递归。
func questOrder(m missionData) map[string]int {
	prev := make(map[string][]string, len(m.QuestDic))
	for key, quest := range m.QuestDic {
		id := quest.QuestID
		if id == "" {
			id = key
		}
		prev[id] = quest.PrevQuestIDList
	}

	depth := make(map[string]int, len(prev))
	visiting := make(map[string]bool)
	var depthOf func(id string) int
	depthOf = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		if visiting[id] {
			return 0
		}
		visiting[id] = true
		d := 0
		for _, p := range prev[id] {
			if _, ok := prev[p]; ok {
				d = max(d, depthOf(p)+1)
			}
		}
		visiting[id] = false
		depth[id] = d
		return d
	}
	for id := range prev {
		depthOf(id)
	}
	return depth
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

// npcNameVariants 合并 NPC 本名和运行时改名的简繁写法，去重后返回。
func npcNameVariants(track trackData) []string {
	return textVariants(append([]missionText{track.NpcName}, track.NpcNameOverrides...))
}

// interactTextVariants 合并实体全部交互选项的简繁写法，去重后返回。
func interactTextVariants(track trackData) []string {
	return textVariants(track.InteractTexts)
}

func textVariants(texts []missionText) []string {
	var names []string
	seen := make(map[string]struct{})
	for _, text := range texts {
		for _, v := range variantsOf(text) {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			names = append(names, v)
		}
	}
	return names
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
