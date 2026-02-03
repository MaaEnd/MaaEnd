package itemtransfer

import (
	"encoding/json"
	"errors"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	// 仓库布局 (默认值)
	RepoFirstX  = 161
	RepoFirstY  = 217
	RepoColumns = 8

	// 背包布局 (默认值)
	BackpackFirstX  = 771
	BackpackFirstY  = 217
	BackpackColumns = 5
	BackpackRows    = 7 // 备用，暂未强依赖

	// 物品格参数
	RowsPerPage  = 4
	BoxSize      = 64
	GridInterval = 5

	// Tooltip (悬浮提示) 参数
	ToolTipCursorOffset = 32
	TooltipRoiScaleX    = 275
	TooltipRoiScaleY    = 130

	// 动作参数
	ResetInvViewScrollDistance = 1200 // 120 * 10
	ResetInvViewSwipeTimes     = 5
)

type configJSON struct {
	Repo struct {
		FirstX  int `json:"FirstX"`
		FirstY  int `json:"FirstY"`
		Columns int `json:"Columns"`
	} `json:"Repo"`
	Backpack struct {
		FirstX  int `json:"FirstX"`
		FirstY  int `json:"FirstY"`
		Columns int `json:"Columns"`
	} `json:"Backpack"`
	Grid struct {
		RowsPerPage         int `json:"RowsPerPage"`
		BoxSize             int `json:"BoxSize"`
		GridInterval        int `json:"GridInterval"`
		ToolTipCursorOffset int `json:"ToolTipCursorOffset"`
		TooltipRoiScaleX    int `json:"TooltipRoiScaleX"`
		TooltipRoiScaleY    int `json:"TooltipRoiScaleY"`
	} `json:"Grid"`
	Action struct {
		ResetInvViewScrollDistance int `json:"ResetInvViewScrollDistance"`
	} `json:"Action"`
}

type ConfigLoader struct{}

func (*ConfigLoader) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var cfg configJSON
	// 尝试解析传入的 JSON
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &cfg); err != nil {
		log.Warn().Err(err).Msg("ConfigLoader: JSON parsing failed, using default hardcoded values.")
		return nil, false
	}

	// 仅当 JSON 中提供了有效值时才覆盖全局变量
	if cfg.Repo.FirstX != 0 {
		RepoFirstX = cfg.Repo.FirstX
	}
	if cfg.Repo.FirstY != 0 {
		RepoFirstY = cfg.Repo.FirstY
	}
	if cfg.Repo.Columns != 0 {
		RepoColumns = cfg.Repo.Columns
	}

	if cfg.Backpack.FirstX != 0 {
		BackpackFirstX = cfg.Backpack.FirstX
	}
	if cfg.Backpack.FirstY != 0 {
		BackpackFirstY = cfg.Backpack.FirstY
	}
	if cfg.Backpack.Columns != 0 {
		BackpackColumns = cfg.Backpack.Columns
	}

	if cfg.Grid.BoxSize != 0 {
		BoxSize = cfg.Grid.BoxSize
	}
	if cfg.Grid.GridInterval != 0 {
		GridInterval = cfg.Grid.GridInterval
	}
	if cfg.Grid.RowsPerPage != 0 {
		RowsPerPage = cfg.Grid.RowsPerPage
	}

	if cfg.Action.ResetInvViewScrollDistance != 0 {
		ResetInvViewScrollDistance = cfg.Action.ResetInvViewScrollDistance
	}

	log.Info().
		Int("RepoX", RepoFirstX).
		Int("BackpackX", BackpackFirstX).
		Int("BoxSize", BoxSize).
		Msg("ConfigLoader: UI Configuration Updated")

	return &maa.CustomRecognitionResult{}, true
}

type SequenceParam struct {
	Sequence []string `json:"sequence"`
}

type SequenceAction struct{}

func (*SequenceAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 1. 解析名单
	var param SequenceParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Msg("SequenceAction: JSON parse failed")
		return false
	}

	// 2. 拿到刚才识别到的坐标 (这就是你说的“识别节点读入box位置”)
	targetBox := arg.RecognitionDetail.Box

	// 3. 挨个点名执行
	for _, nodeName := range param.Sequence {
		// 🔥 核心：调用 Pipeline 里的节点，并把 targetBox 塞给它
		// 如果那个节点是 Click，它就会点这个 Box
		// 如果那个节点是 KeyDown，它会忽略 Box 直接按键
		_, err := ctx.RunAction(nodeName, targetBox, "")
		if err != nil {
			log.Error().Err(err).Str("Node", nodeName).Msg("Step failed")
			return false
		}
	}
	return true
}

type Inventory int

const (
	REPOSITORY Inventory = iota
	BACKPACK
)

func (inv Inventory) String() string {
	switch inv {
	case REPOSITORY:
		return "Repository"
	case BACKPACK:
		return "Backpack"
	default:
		return "Unknown"
	}
}

func (inv Inventory) FirstX() int {
	switch inv {
	case REPOSITORY:
		return RepoFirstX
	case BACKPACK:
		return BackpackFirstX
	default:
		return 0
	}
}
func (inv Inventory) FirstY() int {
	switch inv {
	case REPOSITORY:
		return RepoFirstY
	case BACKPACK:
		return BackpackFirstY
	default:
		return 0
	}
}

func (inv Inventory) Columns() int {
	switch inv {
	case REPOSITORY:
		return RepoColumns
	case BACKPACK:
		return BackpackColumns
	default:
		return 0
	}
}

func TooltipRoi(inv Inventory, gridRowY, gridColX int) maa.Rect {
	x := inv.FirstX() + gridColX*(BoxSize+GridInterval) + ToolTipCursorOffset
	y := inv.FirstY() + gridRowY*(BoxSize+GridInterval) + ToolTipCursorOffset
	w := TooltipRoiScaleX
	h := TooltipRoiScaleY
	return maa.Rect{x, y, w, h}
}

func ItemBoxRoi(inv Inventory, gridRowY, gridColX int) maa.Rect {
	x := inv.FirstX() + gridColX*(BoxSize+GridInterval)
	y := inv.FirstY() + gridRowY*(BoxSize+GridInterval)
	w := BoxSize
	h := BoxSize
	return maa.Rect{x, y, w, h}
}

func HoverOnto(ctx *maa.Context, inv Inventory, gridRowY, gridColX int) error {
	// 获取物品的完整区域 (Rect)
	screenshotArea := TooltipRoi(inv, gridRowY, gridColX)
	interactionPoint := Pointize(screenshotArea)
	_, err := ctx.RunAction("Action_Hover_Item", interactionPoint, "")
	if err != nil {
		// 如果 err 不为 nil，说明动作执行失败
		log.Error().Err(err).Msg("HoverOnto: RunAction failed")
		return errors.New("interaction with item failed")
	}

	return nil
}

func MoveAndShot(ctx *maa.Context, inv Inventory, gridRowY, gridColX int) (img image.Image) {
	// Step 1 - Hover to item
	if HoverOnto(ctx, inv, gridRowY, gridColX) != nil {
		log.Error().
			Str("inventory", inv.String()).
			Int("grid_row_y", gridRowY).
			Int("grid_col_x", gridColX).
			Msg("Failed to hover onto item")
		return nil
	}
	// Step 2 - Make screenshot
	controller := ctx.GetTasker().GetController()
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("Failed to cache image")
		return nil
	}
	return img
}
