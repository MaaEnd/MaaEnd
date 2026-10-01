package creditshopping

import (
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const creditShoppingScanItemActionName = "CreditShoppingScanItemAction"

// RecordShelfSnapshotsAction 信用商店货架快照（best-effort，失败不阻断购物）：
//  1. 截图并识别当日第几次刷新（RefreshCost）；
//  2. 取 UID，查本地 JSON 是否已有 uid+game_date+refresh_index；有则直接结束（不跑 Icon/商品/折扣）；
//  3. 尚无记录时再 CreditIcon 定格 → 商品模板挂格 → 折扣 OCR → 追加写入（保留第一次）。
type RecordShelfSnapshotsAction struct{}

var _ maa.CustomActionRunner = (*RecordShelfSnapshotsAction)(nil)

func (a *RecordShelfSnapshotsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || ctx.GetTasker() == nil {
		log.Error().Str("component", component).Msg("record: nil context or tasker")
		return false
	}
	ctrl := ctx.GetTasker().GetController()
	if ctrl == nil {
		log.Error().Str("component", component).Msg("record: nil controller")
		return false
	}
	path := resolveShelfSnapshotPathFunc()
	now := time.Now()
	gameDate := gameDateLocal(now)

	img, err := recordScreencap(ctrl)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("record: screencap failed")
		return true
	}

	refreshIndex, refreshCost := resolveRefreshIndex(ctx, img)

	uid, err := captureuid.Capture(ctx, ctrl, true, true, true, captureuid.OutputTypeHashed)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("record: uid capture failed")
		return true
	}
	exists, err := shelfSnapshotExists(path, uid, gameDate, refreshIndex)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("path", path).Msg("record: read snapshot failed")
		return true
	}
	if exists {
		log.Info().
			Str("component", component).
			Str("uid", uid).
			Str("game_date", gameDate).
			Int("refresh_index", refreshIndex).
			Int("refresh_cost", refreshCost).
			Msg("record: snapshot already exists, skip")
		return true
	}

	slots := RecordShelfFromImage(ctx, img)
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
		Msg("record: shelf captured")

	n, err := upsertShelfSnapshots(path, []snapshotEntry{entry})
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("path", path).Msg("record: write failed")
		return true
	}
	logSnapshotSaved(path, n)
	return true
}
