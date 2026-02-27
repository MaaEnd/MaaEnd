package autofight

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// SchedulerEventType 调度器事件类型
type SchedulerEventType int

const (
	EventSwitchOperator SchedulerEventType = iota // 切人（attack 事件触发）
	EventSkill                                    // 战技
	EventLink                                     // 连携技
	EventUltimate                                 // 终结技
	EventDodge                                    // 闪避
)

func (t SchedulerEventType) String() string {
	switch t {
	case EventSwitchOperator:
		return "SwitchOperator"
	case EventSkill:
		return "Skill"
	case EventLink:
		return "Link"
	case EventUltimate:
		return "Ultimate"
	case EventDodge:
		return "Dodge"
	default:
		return "Unknown"
	}
}

// SchedulerEvent 展平后的单个调度事件
type SchedulerEvent struct {
	Time          float64 // 相对战斗开始的秒数（已减去 prepDuration）
	Type          SchedulerEventType
	OperatorIndex int     // 1-4，所属干员
	SpCost        float64 // 该动作消耗的技力
	GaugeCost     float64 // 该动作消耗的终结技能量
	// SpAtMoment 记录该事件发生时，时间轴上理论剩余的技力可释放次数（用于终结技条件判断）
	SpAtMoment int
}

// FightConfig 解析后的战斗策略配置
type FightConfig struct {
	ActiveScenarioID string
	ScenarioName     string
	DataCode         string // 来源数据码，用于避免重复加载
	Tracks           []EndaxisTrack
	Events           []SchedulerEvent // 展平并按时间排序的事件列表
	InitialSp        int              // 起始技力（systemConstants.initialSp）
	MaxSp            int              // 最大技力
	PrepDuration     float64          // 准备时间偏移
}

var (
	currentConfig *FightConfig
	configMutex   sync.RWMutex
)

// LoadConfig 解码 Endaxis 数据码并设置为当前配置
func LoadConfig(dataCode string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	// 避免重复加载相同数据码
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

	prepDuration := activeScenario.Data.PrepDuration
	initialSp := project.SystemConstants.InitialSp
	maxSp := project.SystemConstants.MaxSp
	if initialSp <= 0 {
		initialSp = 200 // 默认起始技力
	}
	if maxSp <= 0 {
		maxSp = 300 // 默认最大技力
	}

	// 展平所有轨道的事件到统一时间轴
	events := flattenEvents(activeScenario.Data.Tracks, prepDuration, initialSp, maxSp, project.SystemConstants.SpRegenRate)

	currentConfig = &FightConfig{
		ActiveScenarioID: activeScenario.ID,
		ScenarioName:     activeScenario.Name,
		DataCode:         dataCode,
		Tracks:           activeScenario.Data.Tracks,
		Events:           events,
		InitialSp:        initialSp,
		MaxSp:            maxSp,
		PrepDuration:     prepDuration,
	}

	return nil
}

// flattenEvents 将所有轨道的动作展平为按时间排序的事件列表
func flattenEvents(tracks []EndaxisTrack, prepDuration float64, initialSp, maxSp int, spRegenRate float64) []SchedulerEvent {
	if spRegenRate <= 0 {
		spRegenRate = 8.0 // 默认每秒 8 技力
	}

	var events []SchedulerEvent

	// 模拟技力消耗以计算每个时刻的技力状态
	type rawEvent struct {
		time          float64
		eventType     SchedulerEventType
		operatorIndex int
		spCost        float64
		gaugeCost     float64
	}
	var rawEvents []rawEvent

	var pauseWindows []pauseWindow

	for trackIdx, track := range tracks {
		operatorIndex := trackIdx + 1 // 1-based
		for _, action := range track.Actions {
			relativeTime := action.StartTime - prepDuration
			if relativeTime < 0 {
				relativeTime = 0
			}

			switch action.Type {
			case "attack":
				// 攻击事件 → 切人
				rawEvents = append(rawEvents, rawEvent{
					time:          relativeTime,
					eventType:     EventSwitchOperator,
					operatorIndex: operatorIndex,
				})
			case "skill":
				rawEvents = append(rawEvents, rawEvent{
					time:          relativeTime,
					eventType:     EventSkill,
					operatorIndex: operatorIndex,
					spCost:        action.SpCost,
				})
				pauseWindows = append(pauseWindows, pauseWindow{
					start: relativeTime,
					end:   relativeTime + 0.5,
				})
			case "link":
				rawEvents = append(rawEvents, rawEvent{
					time:          relativeTime,
					eventType:     EventLink,
					operatorIndex: operatorIndex,
				})
				animTime := action.AnimationTime
				if animTime <= 0 {
					animTime = 0.5
				}
				pauseWindows = append(pauseWindows, pauseWindow{
					start: relativeTime,
					end:   relativeTime + animTime,
				})
			case "ultimate":
				rawEvents = append(rawEvents, rawEvent{
					time:          relativeTime,
					eventType:     EventUltimate,
					operatorIndex: operatorIndex,
					gaugeCost:     action.GaugeCost,
				})
				animTime := action.AnimationTime
				if animTime <= 0 {
					animTime = 1.5
				}
				pauseWindows = append(pauseWindows, pauseWindow{
					start: relativeTime,
					end:   relativeTime + animTime,
				})
			case "dodge":
				rawEvents = append(rawEvents, rawEvent{
					time:          relativeTime,
					eventType:     EventDodge,
					operatorIndex: operatorIndex,
				})
				// execution 等其他类型忽略
			}
		}
	}

	// 按时间排序
	sort.Slice(rawEvents, func(i, j int) bool {
		return rawEvents[i].time < rawEvents[j].time
	})

	// 计算每个事件发生时，理论上累计消耗后的技力可释放次数
	currentSp := float64(initialSp)
	prevTime := 0.0

	for _, re := range rawEvents {
		dt := re.time - prevTime
		if dt > 0 {
			// 在这 dt 时间内，计算有多少时间是在 pauseWindows 中的
			pausedTime := 0.0
			for _, pw := range pauseWindows {
				// 窗口 [pw.start, pw.end] 和 考察区间 [prevTime, re.time] 的交集
				startIntersection := math.Max(prevTime, pw.start)
				endIntersection := math.Min(re.time, pw.end)
				if startIntersection < endIntersection {
					pausedTime += endIntersection - startIntersection
				}
			}

			// 如果有重叠窗口，可能导致 pausedTime > dt（简化处理，严格来说应合并重叠区间，但战斗中一般技能/大招时停不互相覆盖太多或游戏也是线性叠加）
			// 这里做一个简化的时间轴合并或者限制以防回流，最简单的方法是合并重叠区间
			// 但 Endaxis 实际上是记录哪些区间是 pause，然后 dt 不计算 pause。这等价于合并区间。
			// 为确保正确，先合并 pauseWindows：
			mergedPauses := mergePauseWindows(pauseWindows)
			pausedTime = 0.0
			for _, pw := range mergedPauses {
				si := math.Max(prevTime, pw.start)
				ei := math.Min(re.time, pw.end)
				if si < ei {
					pausedTime += ei - si
				}
			}

			effectiveDt := dt - pausedTime
			if effectiveDt > 0 {
				currentSp += effectiveDt * spRegenRate
			}
			if currentSp > float64(maxSp) {
				currentSp = float64(maxSp)
			}
		}

		spAtMoment := int(math.Floor(currentSp / 100.0))
		if spAtMoment < 0 {
			spAtMoment = 0
		}

		events = append(events, SchedulerEvent{
			Time:          re.time,
			Type:          re.eventType,
			OperatorIndex: re.operatorIndex,
			SpCost:        re.spCost,
			GaugeCost:     re.gaugeCost,
			SpAtMoment:    spAtMoment,
		})

		// 扣除消耗
		currentSp -= re.spCost
		prevTime = re.time
	}

	return events
}

type pauseWindow struct {
	start float64
	end   float64
}

func mergePauseWindows(windows []pauseWindow) []pauseWindow {
	if len(windows) == 0 {
		return nil
	}
	// 按 start 升序
	sort.Slice(windows, func(i, j int) bool {
		return windows[i].start < windows[j].start
	})

	var res []pauseWindow
	curr := windows[0]
	for _, w := range windows[1:] {
		if w.start <= curr.end {
			if w.end > curr.end {
				curr.end = w.end
			}
		} else {
			res = append(res, curr)
			curr = w
		}
	}
	res = append(res, curr)
	return res
}

// GetConfig 获取当前出招配置
func GetConfig() *FightConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return currentConfig
}

// HasConfig 检查是否已加载配置
func HasConfig() bool {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return currentConfig != nil
}

// ClearConfig 清除当前配置
func ClearConfig() {
	configMutex.Lock()
	defer configMutex.Unlock()
	currentConfig = nil
}
