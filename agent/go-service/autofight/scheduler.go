package autofight

import (
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// FightScheduler 基于时间轴的出招调度器
type FightScheduler struct {
	mu sync.Mutex

	config *FightConfig

	// 时间轴状态
	currentIndex int  // 当前事件索引
	started      bool // 是否已经启动

	// 计时器
	battleStartTime  time.Time
	isPaused         bool
	pausedAt         time.Time
	accumulatedPause time.Duration

	// 连携技状态
	linkPending  int         // 待释放的连携技计数
	linkTimeouts []time.Time // 每个待释放连携技的超时时间

	// 终结技恢复状态
	ultimateRecovery     bool // 是否正在恢复终结技
	ultimateWaitSp       bool // 是否在等待技力满足
	ultimateOperator     int  // 恢复中的干员编号
	ultimateTargetSpSlot int  // 终结技需要的技力可释放次数
}

var scheduler *FightScheduler

// ResetScheduler 重置调度器（新战斗开始时调用）
func ResetScheduler() {
	scheduler = nil
}

// GetScheduler 获取或创建当前调度器
func GetScheduler(cfg *FightConfig) *FightScheduler {
	if scheduler == nil || scheduler.config != cfg {
		scheduler = &FightScheduler{
			config: cfg,
		}
	}
	return scheduler
}

// getElapsedSeconds 获取从战斗开始到现在的有效经过时间（秒）
func (s *FightScheduler) getElapsedSeconds() float64 {
	if !s.started {
		return 0
	}
	var elapsed time.Duration
	if s.isPaused {
		elapsed = s.pausedAt.Sub(s.battleStartTime) - s.accumulatedPause
	} else {
		elapsed = time.Since(s.battleStartTime) - s.accumulatedPause
	}
	return elapsed.Seconds()
}

// pause 暂停计时
func (s *FightScheduler) pause() {
	if !s.isPaused {
		s.isPaused = true
		s.pausedAt = time.Now()
		log.Debug().Float64("elapsed", s.getElapsedSeconds()).Msg("[Scheduler] 暂停计时")
	}
}

// resume 恢复计时
func (s *FightScheduler) resume() {
	if s.isPaused {
		s.accumulatedPause += time.Since(s.pausedAt)
		s.isPaused = false
		log.Debug().Float64("elapsed", s.getElapsedSeconds()).Msg("[Scheduler] 恢复计时")
	}
}

// restart 重启时间轴
func (s *FightScheduler) restart() {
	s.currentIndex = 0
	s.battleStartTime = time.Now()
	s.isPaused = false
	s.accumulatedPause = 0
	s.linkPending = 0
	s.linkTimeouts = nil
	s.ultimateRecovery = false
	s.ultimateWaitSp = false
	log.Info().Msg("[Scheduler] 重启时间轴循环")
}

// SchedulerTickResult 每次 tick 返回的动作建议
type SchedulerTickResult struct {
	Actions []SchedulerAction
}

// SchedulerAction 调度器建议执行的单个动作
type SchedulerAction struct {
	Type     ActionType
	Operator int
}

// Tick 调度器主循环，每次识别帧调用一次
// energyLevel: 当前技力可释放次数（0-3）
// comboAvailable: 是否有连携技可以点
// endSkillUsable: 各干员终结技是否可用（索引列表）
func (s *FightScheduler) Tick(energyLevel int, comboAvailable bool, endSkillUsable []int) SchedulerTickResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result SchedulerTickResult

	// 首次进入，启动计时器
	if !s.started {
		s.started = true
		s.battleStartTime = time.Now()
		log.Info().Int("events", len(s.config.Events)).Msg("[Scheduler] 调度器启动")
	}

	// 处理连携技超时
	s.processLinkTimeouts()

	// 如果有待释放的连携技且连携可用，立即点击
	if s.linkPending > 0 && comboAvailable {
		result.Actions = append(result.Actions, SchedulerAction{
			Type: ActionCombo,
		})
		s.linkPending--
		// 移除最早的超时计时器
		if len(s.linkTimeouts) > 0 {
			s.linkTimeouts = s.linkTimeouts[1:]
		}
		log.Debug().Int("remaining", s.linkPending).Msg("[Scheduler] 释放连携技")
	}

	// 终结技恢复模式
	if s.ultimateRecovery || s.ultimateWaitSp {
		actions := s.handleUltimateRecovery(energyLevel, endSkillUsable)
		result.Actions = append(result.Actions, actions...)
		return result
	}

	// 检查是否时间轴已结束
	if s.currentIndex >= len(s.config.Events) {
		actions := s.handleTimelineEnd(energyLevel)
		result.Actions = append(result.Actions, actions...)
		return result
	}

	elapsed := s.getElapsedSeconds()
	event := s.config.Events[s.currentIndex]

	// 还没到下一个事件的时间，继续打普攻
	if elapsed < event.Time {
		result.Actions = append(result.Actions, SchedulerAction{Type: ActionAttack})
		return result
	}

	// 处理当前事件
	actions := s.processEvent(event, energyLevel, endSkillUsable)
	result.Actions = append(result.Actions, actions...)

	return result
}

// processEvent 处理单个事件
func (s *FightScheduler) processEvent(event SchedulerEvent, energyLevel int, endSkillUsable []int) []SchedulerAction {
	var actions []SchedulerAction

	switch event.Type {
	case EventSwitchOperator:
		// 切人：按 F1-F4
		actions = append(actions, SchedulerAction{
			Type:     ActionSwitchOperator,
			Operator: event.OperatorIndex,
		})
		s.currentIndex++
		log.Debug().Int("operator", event.OperatorIndex).Float64("time", event.Time).Msg("[Scheduler] 切人")

	case EventSkill:
		// 战技：检查技力 >= 1
		if energyLevel >= 1 {
			actions = append(actions, SchedulerAction{
				Type:     ActionSkill,
				Operator: event.OperatorIndex,
			})
			s.currentIndex++
			log.Debug().Int("operator", event.OperatorIndex).Float64("time", event.Time).Msg("[Scheduler] 释放战技")
		} else {
			// 技力不足，暂停计时，继续普攻
			s.pause()
			actions = append(actions, SchedulerAction{Type: ActionAttack})
			log.Debug().Int("energyLevel", energyLevel).Msg("[Scheduler] 战技等待技力")
		}

	case EventLink:
		// 连携技：计数 +1，设置 10s 超时
		s.linkPending++
		s.linkTimeouts = append(s.linkTimeouts, time.Now().Add(10*time.Second))
		s.currentIndex++
		log.Debug().Int("pending", s.linkPending).Float64("time", event.Time).Msg("[Scheduler] 连携技计数+1")

	case EventUltimate:
		// 终结技：检查终结技可用 + 技力条件
		ultReady := isOperatorUltReady(event.OperatorIndex, endSkillUsable)
		spOk := energyLevel >= event.SpAtMoment

		if ultReady && spOk {
			// 两个条件都满足：释放终结技（长按）
			actions = append(actions, SchedulerAction{
				Type:     ActionEndSkillKeyDown,
				Operator: event.OperatorIndex,
			})
			s.currentIndex++
			log.Debug().Int("operator", event.OperatorIndex).Float64("time", event.Time).Msg("[Scheduler] 释放终结技")
		} else {
			// 暂停计时并进入恢复模式
			s.pause()
			if !ultReady {
				// 终结技不可用：打那个角色的战技来积攒终结技能量
				s.ultimateRecovery = true
				s.ultimateWaitSp = false
				s.ultimateOperator = event.OperatorIndex
				s.ultimateTargetSpSlot = event.SpAtMoment
				log.Debug().Int("operator", event.OperatorIndex).Msg("[Scheduler] 终结技未就绪，切到该干员打战技")
				// 先切到该干员
				actions = append(actions, SchedulerAction{
					Type:     ActionSwitchOperator,
					Operator: event.OperatorIndex,
				})
			} else {
				// 终结技可用但技力不足：只打普攻
				s.ultimateWaitSp = true
				s.ultimateRecovery = false
				s.ultimateOperator = event.OperatorIndex
				s.ultimateTargetSpSlot = event.SpAtMoment
				log.Debug().Int("need", event.SpAtMoment).Int("have", energyLevel).Msg("[Scheduler] 终结技就绪但技力不足")
				actions = append(actions, SchedulerAction{Type: ActionAttack})
			}
		}

	case EventDodge:
		// 闪避
		actions = append(actions, SchedulerAction{Type: ActionDodge})
		s.currentIndex++
		log.Debug().Float64("time", event.Time).Msg("[Scheduler] 闪避")
	}

	return actions
}

// handleUltimateRecovery 处理终结技恢复阶段
func (s *FightScheduler) handleUltimateRecovery(energyLevel int, endSkillUsable []int) []SchedulerAction {
	var actions []SchedulerAction
	event := s.config.Events[s.currentIndex]

	if s.ultimateRecovery {
		// 终结技不可用：不断打该干员的战技
		ultReady := isOperatorUltReady(s.ultimateOperator, endSkillUsable)
		if ultReady {
			// 终结技准备好了，再检查技力
			if energyLevel >= event.SpAtMoment {
				// 都满足了，释放终结技并恢复
				actions = append(actions, SchedulerAction{
					Type:     ActionEndSkillKeyDown,
					Operator: s.ultimateOperator,
				})
				s.ultimateRecovery = false
				s.currentIndex++
				s.resume()
				log.Info().Int("operator", s.ultimateOperator).Msg("[Scheduler] 终结技恢复完成，释放")
			} else {
				// 终结技可用了，但技力不足，转为等技力模式
				s.ultimateRecovery = false
				s.ultimateWaitSp = true
				actions = append(actions, SchedulerAction{Type: ActionAttack})
				log.Debug().Msg("[Scheduler] 终结技已就绪，转为等待技力")
			}
		} else {
			// 终结技还没好，尝试释放该干员的战技来攒能量
			if energyLevel >= 1 {
				actions = append(actions, SchedulerAction{
					Type:     ActionSkill,
					Operator: s.ultimateOperator,
				})
			} else {
				// 技力也不够放战技，先普攻
				actions = append(actions, SchedulerAction{Type: ActionAttack})
			}
		}
	} else if s.ultimateWaitSp {
		// 终结技可用但技力不足：只打普攻
		if energyLevel >= event.SpAtMoment {
			// 技力满足了，释放终结技
			actions = append(actions, SchedulerAction{
				Type:     ActionEndSkillKeyDown,
				Operator: s.ultimateOperator,
			})
			s.ultimateWaitSp = false
			s.currentIndex++
			s.resume()
			log.Info().Int("operator", s.ultimateOperator).Msg("[Scheduler] 技力恢复完成，释放终结技")
		} else {
			actions = append(actions, SchedulerAction{Type: ActionAttack})
		}
	}

	return actions
}

// handleTimelineEnd 时间轴结束后：继续普攻直到技力满足 initialSp，然后重启
func (s *FightScheduler) handleTimelineEnd(energyLevel int) []SchedulerAction {
	var actions []SchedulerAction

	// 计算 initialSp 对应的可释放次数
	initialSpSlots := int(math.Floor(float64(s.config.InitialSp) / 100.0))

	if energyLevel >= initialSpSlots {
		// 技力满足起始条件，重启时间轴
		s.restart()
	} else {
		// 继续普攻攒技力
		actions = append(actions, SchedulerAction{Type: ActionAttack})
	}

	return actions
}

// processLinkTimeouts 处理连携技超时
func (s *FightScheduler) processLinkTimeouts() {
	now := time.Now()
	for len(s.linkTimeouts) > 0 && now.After(s.linkTimeouts[0]) {
		s.linkTimeouts = s.linkTimeouts[1:]
		if s.linkPending > 0 {
			s.linkPending--
			log.Debug().Int("remaining", s.linkPending).Msg("[Scheduler] 连携技超时，计数-1")
		}
	}
}

// isOperatorUltReady 检查指定干员的终结技是否可用
func isOperatorUltReady(operatorIndex int, endSkillUsable []int) bool {
	for _, idx := range endSkillUsable {
		if idx == operatorIndex {
			return true
		}
	}
	return false
}
