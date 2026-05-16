package creditshopping

import (
	"encoding/json"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/rs/zerolog/log"
)

const recordShelfSnapshotsActionName = "CreditShopping.RecordShelfSnapshots"

type recordShelfSnapshotsParam struct {
	Loops int `json:"loops"`
}

// RecordShelfSnapshotsAction 信用点商店库存快照（分步）：
//  1) 运行 pipeline 节点 RecordItemName 获取各槽位名称；
//  2) 遍历 ItemNameOCR_Expected 命中框，以其 box 覆盖 RecordItemDiscount 的 roi 锚点识别折扣；
//  3) 将本轮名称与折扣（未命中为 None）写入本地 JSON（含去重）。
// 不判断售罄、买不起。
// custom_action_param: {"loops":<正整数>}；省略或 <=0 时：ADB 默认 2 次，其它默认 1 次。
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
	loops := resolveLoops(arg, ctrl)
	if loops <= 0 {
		loops = 1
	}
	path := resolveShelfSnapshotPathFunc()
	var batch []snapshotEntry
	for i := 0; i < loops; i++ {
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			log.Error().Err(err).Str("component", component).Int("loop", i).Msg("record shelf: screencap failed")
			return false
		}
		uid := uidFromImage(ctx, img)
		slots := ScanShelfSlots(ctx, img)
		batch = append(batch, snapshotEntry{
			UID:       uid,
			UTCTime:   time.Now().UTC().Format(time.RFC3339),
			LoopIndex: i,
			Slots:     slots,
		})
		log.Info().
			Str("component", component).
			Str("uid", uid).
			Int("loop", i).
			Int("slots", len(slots)).
			Msg("credit shopping shelf snapshot captured")
	}
	n, err := appendShelfSnapshots(path, batch)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("path", path).Msg("record shelf: write failed")
		return false
	}
	logSnapshotSaved(path, n, len(batch)-n)
	return true
}

func resolveLoops(arg *maa.CustomActionArg, ctrl *maa.Controller) int {
	def := defaultLoopCount(ctrl)
	if arg == nil || strings.TrimSpace(arg.CustomActionParam) == "" {
		return def
	}
	var p recordShelfSnapshotsParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Warn().Err(err).Str("component", component).Msg("record shelf: bad param, use default loops")
		return def
	}
	if p.Loops > 0 {
		return p.Loops
	}
	return def
}

func defaultLoopCount(ctrl *maa.Controller) int {
	t, err := control.GetControlType(ctrl)
	if err == nil && t == control.CONTROL_TYPE_ADB {
		return 2
	}
	return 1
}
