package stashbackpack

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	snapshotActionName = "StashBackpackSnapshotAction"

	snapshotPrepareRecognitionNode = "__StashBackpackSnapshotPrepareRecognitionStep"
	snapshotScrollUpwardNode       = "__StashBackpackSnapshotScrollUpwardStep"
	snapshotScrollDownwardNode     = "__StashBackpackSnapshotScrollDownwardStep"
	scrollbarRecognitionName       = "ScrollbarRecognition"
	emptyGridDetectionErrorCode    = "grid_detection_failed"
	emptyGridDetectionErrorMessage = "grid ROI contains no formal cells"

	// snapshotMaxScrollCount 沿用原 Pipeline 的滚动上限，防止滚动条异常时无限扫描。
	snapshotMaxScrollCount = 20
	// snapshotScrollbarTolerance 是滚动条上下边界允许的最大偏差，单位为 720p 像素。
	// 调大可容忍轻微抖动，但也会增加将短距离滚动误判为到底的风险。
	snapshotScrollbarTolerance = 2
	// snapshotMissingScrollbarConfirmations 要求连续缺失两次才按不可滚动列表处理，避免单帧漏识别。
	snapshotMissingScrollbarConfirmations = 2
)

var (
	// snapshotItemROI 是 1280x720 基准下背包物品网格的识别区域。
	snapshotItemROI = maa.Rect{739, 202, 398, 291}
	// snapshotScrollbarROI 是 1280x720 基准下背包滚动条滑块的搜索区域。
	snapshotScrollbarROI = maa.Rect{1119, 220, 5, 255}
)

type snapshotActionParam struct {
	Snapshot string `json:"snapshot"`
}

type snapshotScrollbarPosition struct {
	Top    int
	Bottom int
}

// SnapshotAction 完整扫描背包所有页面，并在扫描成功后原子写入指定快照。
type SnapshotAction struct{}

var _ maa.CustomActionRunner = &SnapshotAction{}

// Run 回顶后逐页识别背包；空页正常继续，真实识别错误会使动作失败。
func (a *SnapshotAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", snapshotActionName).Msg("snapshot action received nil context or arg")
		return false
	}

	var param snapshotActionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", snapshotActionName).Msg("failed to parse snapshot action params")
		return false
	}
	param.Snapshot = strings.TrimSpace(param.Snapshot)
	if param.Snapshot == "" {
		log.Error().Str("component", snapshotActionName).Msg("snapshot name is empty")
		return false
	}

	pages, err := captureSnapshotPages(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", snapshotActionName).Str("snapshot", param.Snapshot).
			Msg("failed to capture backpack snapshot")
		return false
	}
	total, err := globalState.replaceSnapshotPages(param.Snapshot, pages)
	if err != nil {
		log.Error().Err(err).Str("component", snapshotActionName).Str("snapshot", param.Snapshot).
			Msg("failed to store backpack snapshot")
		return false
	}

	log.Info().Str("component", snapshotActionName).Str("snapshot", param.Snapshot).
		Int("page_count", len(pages)).Int("item_count", total).Msg("captured backpack snapshot")
	return true
}

func captureSnapshotPages(ctx *maa.Context) ([][]snapshotItemWithPosition, error) {
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}
	controller := tasker.GetController()
	if controller == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	currentImage, currentScrollbar, err := scrollSnapshotToTop(ctx, controller)
	if err != nil {
		return nil, err
	}

	pages := make([][]snapshotItemWithPosition, 0, snapshotMaxScrollCount+1)
	missingScrollbarCount := 0
	if currentScrollbar == nil {
		missingScrollbarCount = 1
	}
	for scrollCount := 0; ; scrollCount++ {
		if tasker.Stopping() {
			return nil, fmt.Errorf("task is stopping")
		}

		page, err := recognizeSnapshotPage(ctx, currentImage, currentScrollbar != nil)
		if err != nil {
			return nil, fmt.Errorf("recognize page %d: %w", len(pages)+1, err)
		}
		pages = append(pages, page)
		log.Info().Str("component", snapshotActionName).Int("page", len(pages)).Int("item_count", len(page)).
			Msg("recognized backpack snapshot page")

		if scrollCount >= snapshotMaxScrollCount {
			return nil, fmt.Errorf("backpack bottom not reached after %d downward scrolls", snapshotMaxScrollCount)
		}
		if err := runSnapshotStep(ctx, snapshotScrollDownwardNode); err != nil {
			return nil, err
		}
		nextImage, nextScrollbar, err := prepareSnapshotImage(ctx, controller)
		if err != nil {
			return nil, err
		}

		if currentScrollbar != nil && nextScrollbar != nil &&
			snapshotScrollbarPositionsMatch(*currentScrollbar, *nextScrollbar) {
			log.Info().Str("component", snapshotActionName).Int("page_count", len(pages)).
				Int("downward_scroll_count", scrollCount+1).Msg("backpack snapshot reached bottom")
			return pages, nil
		}
		if nextScrollbar == nil {
			missingScrollbarCount++
		} else {
			missingScrollbarCount = 0
		}
		if missingScrollbarCount >= snapshotMissingScrollbarConfirmations {
			log.Info().Str("component", snapshotActionName).Int("page_count", len(pages)).
				Int("downward_scroll_count", scrollCount+1).
				Msg("scrollbar missing in consecutive observations, backpack is not scrollable")
			return pages, nil
		}

		currentImage = nextImage
		currentScrollbar = nextScrollbar
	}
}

func scrollSnapshotToTop(
	ctx *maa.Context,
	controller *maa.Controller,
) (image.Image, *snapshotScrollbarPosition, error) {
	_, currentScrollbar, err := prepareSnapshotImage(ctx, controller)
	if err != nil {
		return nil, nil, err
	}
	missingScrollbarCount := 0
	if currentScrollbar == nil {
		missingScrollbarCount = 1
	}

	for scrollCount := 0; scrollCount < snapshotMaxScrollCount; scrollCount++ {
		if ctx.GetTasker().Stopping() {
			return nil, nil, fmt.Errorf("task is stopping")
		}
		if err := runSnapshotStep(ctx, snapshotScrollUpwardNode); err != nil {
			return nil, nil, err
		}
		nextImage, nextScrollbar, err := prepareSnapshotImage(ctx, controller)
		if err != nil {
			return nil, nil, err
		}

		if currentScrollbar != nil && nextScrollbar != nil &&
			snapshotScrollbarPositionsMatch(*currentScrollbar, *nextScrollbar) {
			log.Info().Str("component", snapshotActionName).Int("upward_scroll_count", scrollCount+1).
				Msg("backpack snapshot reached top")
			return nextImage, nextScrollbar, nil
		}
		if nextScrollbar == nil {
			missingScrollbarCount++
		} else {
			missingScrollbarCount = 0
		}
		if missingScrollbarCount >= snapshotMissingScrollbarConfirmations {
			log.Info().Str("component", snapshotActionName).Int("upward_scroll_count", scrollCount+1).
				Msg("scrollbar missing in consecutive observations while returning to top")
			return nextImage, nextScrollbar, nil
		}

		currentScrollbar = nextScrollbar
	}
	return nil, nil, fmt.Errorf("backpack top not reached after %d upward scrolls", snapshotMaxScrollCount)
}

func prepareSnapshotImage(
	ctx *maa.Context,
	controller *maa.Controller,
) (image.Image, *snapshotScrollbarPosition, error) {
	if err := runSnapshotStep(ctx, snapshotPrepareRecognitionNode); err != nil {
		return nil, nil, err
	}
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		return nil, nil, fmt.Errorf("cache screenshot: %w", err)
	}
	if img == nil {
		return nil, nil, fmt.Errorf("cached screenshot is nil")
	}

	scrollbar, err := recognizeSnapshotScrollbar(ctx, img)
	if err != nil {
		return nil, nil, err
	}
	return img, scrollbar, nil
}

func recognizeSnapshotPage(
	ctx *maa.Context,
	img image.Image,
	backpackScrollbarVisible bool,
) ([]snapshotItemWithPosition, error) {
	params := iconrecognition.NewParams(
		iconrecognition.WithGridType(iconrecognition.GridTypeTransfer),
		iconrecognition.WithItemFilters(iconrecognition.StorageFilter().Normal.Any),
		iconrecognition.WithDebug(true),
	)
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:                    maa.NewTargetRect(snapshotItemROI),
			CustomRecognition:      iconrecognition.CustomRecognitionName,
			CustomRecognitionParam: params,
		},
		img,
	)
	if err != nil {
		return nil, fmt.Errorf("run IconRecognition: %w", err)
	}
	parsed, _, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		if parsed.Error.Code == iconrecognition.ErrorCodeNoMatch {
			return []snapshotItemWithPosition{}, nil
		}
		// 当前截图中滚动条仍可识别时，网格不存在正式格子表示背包本页已经完全清空。
		// 仅兼容 IconRecognition 的精确空网格错误，其他定位失败仍向上返回。
		if backpackScrollbarVisible &&
			parsed.Error.Code == emptyGridDetectionErrorCode &&
			parsed.Error.Message == emptyGridDetectionErrorMessage {
			log.Warn().Str("component", snapshotActionName).
				Msg("treated empty backpack grid detection as an empty snapshot page")
			return []snapshotItemWithPosition{}, nil
		}
		return nil, fmt.Errorf("IconRecognition %s: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if !parsed.Matched {
		return nil, fmt.Errorf("IconRecognition returned unmatched result without an error")
	}

	positioned := make([]snapshotItemWithPosition, 0, len(parsed.Matches))
	for _, match := range parsed.Matches {
		row := match.CellBox.Y()
		column := match.CellBox.X()
		if match.Row != nil {
			row = *match.Row
		}
		if match.Column != nil {
			column = *match.Column
		}
		positioned = append(positioned, snapshotItemWithPosition{
			ItemID:       match.ItemID,
			CategoryType: match.CategoryType,
			Row:          row,
			Column:       column,
			CellBox:      match.CellBox,
		})
	}
	return positioned, nil
}

func recognizeSnapshotScrollbar(
	ctx *maa.Context,
	img image.Image,
) (*snapshotScrollbarPosition, error) {
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:               maa.NewTargetRect(snapshotScrollbarROI),
			CustomRecognition: scrollbarRecognitionName,
		},
		img,
	)
	if err != nil {
		return nil, fmt.Errorf("run scrollbar recognition: %w", err)
	}
	if detail == nil {
		return nil, fmt.Errorf("scrollbar recognition detail is nil")
	}
	if !detail.Hit {
		return nil, nil
	}
	if detail.Box.Height() <= 0 {
		return nil, fmt.Errorf("scrollbar recognition returned invalid box %v", detail.Box)
	}
	return &snapshotScrollbarPosition{
		Top:    detail.Box.Y(),
		Bottom: detail.Box.Y() + detail.Box.Height() - 1,
	}, nil
}

func runSnapshotStep(ctx *maa.Context, node string) error {
	detail, err := ctx.RunTask(node)
	if err != nil {
		return fmt.Errorf("run snapshot step %q: %w", node, err)
	}
	if detail == nil || !detail.Status.Success() {
		return fmt.Errorf("snapshot step %q did not succeed", node)
	}
	return nil
}

func snapshotScrollbarPositionsMatch(previous, current snapshotScrollbarPosition) bool {
	return snapshotWithinTolerance(previous.Top, current.Top) &&
		snapshotWithinTolerance(previous.Bottom, current.Bottom)
}

func snapshotWithinTolerance(left, right int) bool {
	difference := left - right
	if difference < 0 {
		difference = -difference
	}
	return difference <= snapshotScrollbarTolerance
}
