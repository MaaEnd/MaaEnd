package creditshopping

import (
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const creditShoppingScanItemActionName = "CreditShoppingScanItemAction"

// RecordShelfSnapshotsAction 信用点商店货架库存快照（best-effort，失败仅记日志，不阻断购物主流程）：
//  1. 截图识别当前刷新次数，再取 UID 与本地游戏日（04:00 切日）；
//  2. 以 uid + game_date + refresh_index 为键，已有记录则直接返回，保留第一次；
//  3. 尚无记录时再扫一屏货架并追加写入，不再滑动翻页。
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
	adb := isADBController(ctrl)

	img, err := screencap(ctrl)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("record shelf: screencap failed")
		return true
	}
	refreshIndex, refreshCost := resolveRefreshIndex(ctx, img)

	uid, err := captureuid.Capture(ctx, ctrl, true, true, true, captureuid.OutputTypeHashed)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("record shelf: uid capture failed")
		return true
	}
	exists, err := shelfSnapshotExists(path, uid, gameDate, refreshIndex)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("path", path).Msg("record shelf: read existing snapshot failed")
		return true
	}
	if exists {
		log.Info().
			Str("component", component).
			Str("uid", uid).
			Str("game_date", gameDate).
			Int("refresh_index", refreshIndex).
			Int("refresh_cost", refreshCost).
			Msg("credit shopping shelf snapshot already recorded, skip")
		return true
	}

	slots := ScanShelfSlots(ctx, img, adb)
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
		Bool("adb", adb).
		Msg("credit shopping shelf snapshot captured")

	n, err := upsertShelfSnapshots(path, []snapshotEntry{entry})
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("path", path).Msg("record shelf: write failed")
		return true
	}
	logSnapshotSaved(path, n)
	return true
}
