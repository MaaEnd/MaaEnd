package taskfail

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const issuesURL = "https://github.com/MaaEnd/MaaEnd/issues"

// 较稳定的任务一般不会失败，如果遇到失败可能是环境问题、游戏更新等，引导用户及时反馈。
// 添加entry节点名称即可
var feedbackHintEntries = map[string]struct{}{
	"AutoSellMain":                {},
	"AutoStockpileMain":           {},
	"AutoStockStapleSchedule":     {},
	"AutoUseSpMedicationEntry":    {},
	"BatchAddFriendsMain":         {},
	"BatchUseDetectorMain":        {},
	"SimSpaceEntry":               {},
	"CreditShoppingMain":          {},
	"DailyRewardStart":            {},
	"DeliveryJobsMain":            {},
	"DijiangRewards":              {},
	"GearAssemblyStart":           {},
	"GiftOperatorMain":            {},
	"ImportBluePrints":            {},
	"ProtocolSpaceSchedule":       {},
	"RealTimeTaskMain":            {},
	"ProdManualStart":             {},
	"ResourceRecycleStationStart": {},
	"SellProductSchedule":         {},
	"StashBackpackMain":           {},
	"SwitchTeamMain":              {},
	"VisitFriendsMain":            {},
	"WeaponUpgradeStart":          {},
}

// Sink prints a localized feedback hint when a task fails.
type Sink struct{}

// OnTaskerTask handles tasker task lifecycle events.
func (s *Sink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusFailed {
		return
	}

	if _, ok := feedbackHintEntries[detail.Entry]; !ok {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("Task failed but entry is not in feedback hint list, skip feedback hint")
		return
	}

	log.Info().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Task failed, showing feedback hint")

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.task_failed_feedback_hint", map[string]any{
			"IssuesURL": issuesURL,
		}),
	)
}
