package batchaddfriends

import (
	"image"
	"sort"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 本文件维护「已拒绝行」的黑名单，并据此挑选下一个可添加的行。
//
// 黑名单的用途：名片页判定不达标时，该行按钮仍然是「添加好友」，下一轮扫描还会
// 挑到同一个人，导致反复打开同一张名片。列表不滚动，行的纵向位置在两次「换一批」
// 之间保持不变，因此用「按钮框的 y 坐标」作为行的标识，换一批时清空。
const (
	componentPickRow      = "BatchAddFriendsStrangersPickRowRecognition"
	componentRecordReject = "BatchAddFriendsStrangersRecordRejectAction"
	componentClearRejects = "BatchAddFriendsStrangersClearRejectsAction"

	// 按钮列 OCR 节点名。
	pickRowButtonOCR = "BatchAddFriendsStrangersAddButtonColumn"

	// 头像列中心的横坐标（720p）。
	// 头像列版式固定、与游戏语言无关，所以用常量；按钮框是 OCR 出的文字范围，
	// 宽度随语言变化，不能用它推 X。
	pickRowAvatarCenterX = 89

	// 头像中心相对按钮框中心的纵向偏移（720p，向上为负）。
	// 列表页两者重合，保持 0；调整前先按截图量头像框中心 Y 与按钮文字框中心 Y。
	pickRowAvatarDy = 0

	// 点击框边长。必须是 1：Click 未指定 target 时会在识别框内随机取点，
	// 框越大落点越漂；1×1 等价于精确点。
	pickRowAvatarSize = 1

	// 行位置匹配容差。
	// 同一行的按钮框 y 在相邻两次识别之间会有像素级抖动，容差必须覆盖它，
	// 否则已拒绝的行会被当成新行重复挑中。行距约 105px，20 不会把相邻行并成一行。
	rejectMatchTolerance = 20
)

var rejectedRows = struct {
	mu   sync.Mutex
	ys   []int // 已拒绝行的按钮框 y
	last int   // 最近一次挑中的按钮框 y，供拒绝动作使用
}{}

// PickStrangerRowRecognition 挑选最上面一个「未被拒绝」的可添加行，返回其头像位置。
type PickStrangerRowRecognition struct{}

var _ maa.CustomRecognitionRunner = &PickStrangerRowRecognition{}

// Run implements maa.CustomRecognitionRunner.
func (r *PickStrangerRowRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}

	buttons := runButtonOCR(ctx, arg.Img)
	if len(buttons) == 0 {
		return nil, false
	}

	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()

	for _, button := range buttons {
		if isRejectedLocked(button.Y()) {
			log.Debug().
				Str("component", componentPickRow).
				Int("y", button.Y()).
				Msg("row is in reject list, skip")
			continue
		}
		rejectedRows.last = button.Y()
		return &maa.CustomRecognitionResult{Box: avatarBox(button)}, true
	}

	// 屏幕上还剩下的人全在黑名单里，当作「没人可加」，让 Pipeline 去换一批。
	// 这里要报「屏幕上还剩几个可添加的」，所以取 len(buttons)；
	// 黑名单条数会包含已经滚出视野或被换批替换掉的旧记录，和界面上的数量对不上。
	message := i18n.T("batchaddfriends.pick.allSkipped", len(buttons))
	log.Info().
		Str("component", componentPickRow).
		Int("visible", len(buttons)).
		Int("rejected", len(rejectedRows.ys)).
		Msg(message)
	maafocus.Print(ctx, message)
	return nil, false
}

// RecordRejectAction 把最近一次挑中的行记入黑名单（名片页判定不达标后调用）。
type RecordRejectAction struct{}

var _ maa.CustomActionRunner = &RecordRejectAction{}

// Run implements maa.CustomActionRunner.
func (a *RecordRejectAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()

	if rejectedRows.last == 0 {
		log.Warn().Str("component", componentRecordReject).Msg("no picked row to reject")
		return true
	}

	rejectedRows.ys = append(rejectedRows.ys, rejectedRows.last)
	log.Info().
		Str("component", componentRecordReject).
		Int("y", rejectedRows.last).
		Int("total", len(rejectedRows.ys)).
		Msg("row added to reject list")

	// 被拒绝也算「判定过一个玩家」，计入尝试次数。
	state.strangersAttempts++
	endIfAttemptsExceeded(ctx, arg.CurrentTaskName)
	return true
}

// ClearRejectsAction 清空黑名单（换一批后行位置全部改变，旧记录作废）。
type ClearRejectsAction struct{}

var _ maa.CustomActionRunner = &ClearRejectsAction{}

// Run implements maa.CustomActionRunner.
func (a *ClearRejectsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()

	rejectedRows.ys = nil
	rejectedRows.last = 0
	log.Debug().Str("component", componentClearRejects).Msg("reject list cleared")
	return true
}

// isRejectedLocked 判断某个按钮框 y 是否落在黑名单内（调用方需已持锁）。
func isRejectedLocked(y int) bool {
	for _, rejected := range rejectedRows.ys {
		diff := rejected - y
		if diff < 0 {
			diff = -diff
		}
		if diff <= rejectMatchTolerance {
			return true
		}
	}
	return false
}

// runButtonOCR 执行按钮列 OCR 节点，返回按纵向排序的按钮框。
func runButtonOCR(ctx *maa.Context, img image.Image) []maa.Rect {
	detail, err := ctx.RunRecognition(pickRowButtonOCR, img, nil)
	if err != nil || detail == nil || detail.Results == nil || !detail.Hit {
		return nil
	}

	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}

	boxes := make([]maa.Rect, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		if ocr, ok := result.AsOCR(); ok && ocr != nil {
			boxes = append(boxes, ocr.Box)
		}
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Y() < boxes[j].Y() })
	return boxes
}

// avatarBox 把按钮文字框换算成该行头像的点击框。
//
// X 固定用头像列常量（与语言无关），Y 取按钮框中心，按钮在这里只回答「命中第几行」。
func avatarBox(button maa.Rect) maa.Rect {
	centerX := pickRowAvatarCenterX
	centerY := button.Y() + button.Height()/2 + pickRowAvatarDy
	return maa.Rect{centerX, centerY, pickRowAvatarSize, pickRowAvatarSize}
}
