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
// 挑到同一个人，导致反复打开同一张名片。同一批列表里行的先后顺序稳定，因此用
// 「本批内从上往下的序号」标识行，换一批时清空。
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

	// 连续空批上限。
	// 「换一批」回路不经过任何计数节点，推荐列表翻到底后会一直空转，
	// 连续这么多次换批都没挑到人就直接结束。
	maxConsecutiveEmptyBatches = 3
)

// rejectedRows 是「本批已拒绝的行」，行身份用从上往下的序号而不是像素坐标：
// 同一批列表里行序稳定，而按钮框 y 会随渲染与测量轻微抖动，
// 用 y 加容差匹配会在容差边缘把邻居行误判成同一行，导致挑行顺序错乱。
var rejectedRows = struct {
	mu      sync.Mutex
	indexes []int // 已拒绝行的序号
	last    int   // 最近一次挑中的行序号
	hasLast bool  // last 是否有效，序号 0 也是合法行
}{}

// consecutiveEmptyBatches 记录连续「换一批后仍然没有任何可添加行」的次数。
// Scan → ChangeBatch → ClearRejects 这条回路不经过任何计数节点，
// 单靠尝试次数兜不住，需要单独设上限。
var consecutiveEmptyBatches int

// PickStrangerRowRecognition 挑选最上面一个「未被拒绝」的可添加行，返回其头像位置。
type PickStrangerRowRecognition struct{}

var _ maa.CustomRecognitionRunner = &PickStrangerRowRecognition{}

// Run implements maa.CustomRecognitionRunner.
// 按从上往下的顺序找第一个未被拒绝的行，返回该行头像的点击框。
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

	for index, button := range buttons {
		if isRejectedLocked(index) {
			log.Debug().
				Str("component", componentPickRow).
				Int("index", index).
				Int("y", button.Y()).
				Msg("row is in reject list, skip")
			continue
		}
		rejectedRows.last = index
		rejectedRows.hasLast = true
		// 挑到了人，连续空批计数归零。
		consecutiveEmptyBatches = 0
		return &maa.CustomRecognitionResult{Box: avatarBox(button)}, true
	}

	// 屏幕上还剩下的人全在黑名单里，当作「没人可加」，让 Pipeline 去换一批。
	// 这里要报「屏幕上还剩几个可添加的」，所以取 len(buttons)；
	// 黑名单条数会包含已经滚出视野或被换批替换掉的旧记录，和界面上的数量对不上。
	message := i18n.T("batchaddfriends.pick.allSkipped", len(buttons))
	log.Info().
		Str("component", componentPickRow).
		Int("visible", len(buttons)).
		Int("rejected", len(rejectedRows.indexes)).
		Msg(message)
	maafocus.Print(ctx, message)
	return nil, false
}

// RecordRejectAction 把最近一次挑中的行记入黑名单（名片页判定不达标后调用）。
type RecordRejectAction struct{}

var _ maa.CustomActionRunner = &RecordRejectAction{}

// Run implements maa.CustomActionRunner.
// 把最近一次挑中的行记入黑名单，并累加尝试次数。
func (a *RecordRejectAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()

	if !rejectedRows.hasLast {
		log.Warn().Str("component", componentRecordReject).Msg("no picked row to reject")
		return true
	}

	rejectedRows.indexes = append(rejectedRows.indexes, rejectedRows.last)
	log.Info().
		Str("component", componentRecordReject).
		Int("index", rejectedRows.last).
		Int("total", len(rejectedRows.indexes)).
		Msg("row added to reject list")

	// 被拒绝也算「判定过一个玩家」，计入尝试次数。
	state.strangersAttempts++
	endIfAttemptsExceeded(ctx, arg.CurrentTaskName)
	return true
}

// ClearRejectsAction 清空黑名单：换一批后列表内容全部更换，旧序号作废。
type ClearRejectsAction struct{}

var _ maa.CustomActionRunner = &ClearRejectsAction{}

// Run implements maa.CustomActionRunner.
// 清空黑名单；连续空批达到上限时结束陌生人模式。
func (a *ClearRejectsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()

	rejectedRows.indexes = nil
	rejectedRows.last = 0
	rejectedRows.hasLast = false

	// 「换一批」回路不经过任何计数节点，连续空批要单独兜底，
	// 否则推荐列表翻到底之后会一直换批空转。
	consecutiveEmptyBatches++
	if consecutiveEmptyBatches > maxConsecutiveEmptyBatches {
		log.Warn().
			Str("component", componentClearRejects).
			Int("emptyBatches", consecutiveEmptyBatches).
			Msg("too many consecutive empty batches, end strangers mode")
		maafocus.Print(ctx, i18n.T("batchaddfriends.strangers_empty_batch_limit", consecutiveEmptyBatches))
		_ = ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
			{Name: "BatchAddFriendsStrangersEnd"},
		})
		return true
	}

	log.Debug().
		Str("component", componentClearRejects).
		Int("emptyBatches", consecutiveEmptyBatches).
		Msg("reject list cleared")
	return true
}

// resetRejectedRows 清空黑名单并重置最近挑中的行。
//
// 任务开始时必须调用：这些记录是包级状态，上一次任务结束后会残留，
// 而行的序号只在同一批列表内有效，跨任务复用会误跳过新玩家。
func resetRejectedRows() {
	rejectedRows.mu.Lock()
	defer rejectedRows.mu.Unlock()

	rejectedRows.indexes = nil
	rejectedRows.last = 0
	rejectedRows.hasLast = false
}

// isRejectedLocked 判断某个行序号是否落在黑名单内（调用方需已持锁）。
func isRejectedLocked(index int) bool {
	for _, rejected := range rejectedRows.indexes {
		if rejected == index {
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
