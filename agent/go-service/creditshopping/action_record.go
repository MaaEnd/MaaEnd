package creditshopping

import (
	"image"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const creditShoppingScanItemActionName = "CreditShoppingScanItemAction"

// RecordShelfSnapshotsAction 信用点商店货架库存快照：
//  1) OCR UID 与 RefreshCost，推断本地游戏日（04:00 切日）与第几次刷新；
//  2) PC 一屏 7+3；ADB 两屏各一排（首屏 slot 0–5 含折扣，滑动后 slot 6–9 含折扣）；
//  3) 以 uid + game_date + refresh_index 为键写入 JSON，键冲突则覆盖。
type RecordShelfSnapshotsAction struct{}

var _ maa.CustomActionRunner = (*RecordShelfSnapshotsAction)(nil)

func (a *RecordShelfSnapshotsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || ctx.GetTasker() == nil {
		log.Error().Str("component", component).Msg("record shelf: nil context or tasker")
		return false
	}
	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Error().Str("component", component).Msg("record shelf: nil controller")
		return false
	}
	path := resolveShelfSnapshotPathFunc()
	now := time.Now()
	gameDate := gameDateLocal(now)

	var imgForMeta image.Image
	var slots []SlotRecord
	if isADBController(ctrl) {
		first, err := screencap(ctrl)
		if err != nil {
			log.Error().Err(err).Str("component", component).Msg("record shelf adb: screencap failed")
			return false
		}
		imgForMeta = first
		slots = scanShelfSlotsADB(ctx, ctrl, first)
	} else {
		first, err := screencap(ctrl)
		if err != nil {
			log.Error().Err(err).Str("component", component).Msg("record shelf: screencap failed")
			return false
		}
		imgForMeta = first
		slots = ScanShelfSlotsPC(ctx, first)
	}

	uid := uidFromImage(ctx, imgForMeta)
	refreshIndex, refreshCost := resolveRefreshIndex(ctx, imgForMeta)
	entry := snapshotEntry{
		UID:          uid,
		GameDate:     gameDate,
		RefreshIndex: refreshIndex,
		RefreshCost:  refreshCost,
		UTCTime:      now.UTC().Format(time.RFC3339),
		Slots:        slots,
	}
	log.Info().
		Str("component", component).
		Str("uid", uid).
		Str("game_date", gameDate).
		Int("refresh_index", refreshIndex).
		Int("refresh_cost", refreshCost).
		Int("slots", len(slots)).
		Bool("adb", isADBController(ctrl)).
		Msg("credit shopping shelf snapshot captured")

	n, err := upsertShelfSnapshots(path, []snapshotEntry{entry})
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("path", path).Msg("record shelf: write failed")
		return false
	}
	logSnapshotSaved(path, n)
	return true
}
