package itemtransfer

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

type Point struct {
	Row int
	Col int
}

type TransferSession struct {
	ItemName     string
	CategoryNode string
	// 分别记录两个区域的最后位置
	LastPosRepo     Point
	LastPosBackpack Point
	MaxTimes        int // 目标次数 (<=0 代表无限)
	CurrentCount    int // 当前已搬运次数
}

// 初始化全局缓存，默认坐标设为 -1 代表未初始化
var currentSession = TransferSession{
	LastPosRepo:     Point{-1, -1},
	LastPosBackpack: Point{-1, -1},
}

func runLocate(ctx *maa.Context, arg *maa.CustomRecognitionArg, targetInv Inventory, currentNodeName string) (*maa.CustomRecognitionResult, bool) {
	var taskParam map[string]any
	json.Unmarshal([]byte(arg.CustomRecognitionParam), &taskParam)

	rawName, _ := taskParam["ItemName"].(string)
	rawCat, _ := taskParam["CategoryNode"].(string)

	// 判断是否为新任务（有效参数传入）
	isValidNewParams := rawName != "" && !strings.Contains(rawName, "{") && !strings.Contains(rawName, "ItemParName")
	if isValidNewParams {
		// [情况 A] 新任务：重置 Session
		// 如果名字变了，才重置坐标；如果名字没变（比如暂停后继续），保留坐标
		if currentSession.ItemName != rawName {
			currentSession.ItemName = rawName
			currentSession.CategoryNode = rawCat
			currentSession.LastPosRepo = Point{0, 0}     // 重置回起点
			currentSession.LastPosBackpack = Point{0, 0} // 重置回起点
			currentSession.CurrentCount = 0
			log.Info().Str("Item", rawName).Msg("GoService: New Session Started, Cache Reset")
		}
	} else {
		// [情况 B] 循环回来的参数丢失：读取 Session
		if currentSession.ItemName == "" {
			return nil, false
		}
	}

	if currentSession.MaxTimes > 0 && currentSession.CurrentCount >= currentSession.MaxTimes {
		log.Info().
			Int("Current", currentSession.CurrentCount).
			Int("Max", currentSession.MaxTimes).
			Msg("⚠️ Max transfer limit reached. Stopping recognition.")

		return nil, false
	}

	finalItemName := currentSession.ItemName
	finalCategoryNode := currentSession.CategoryNode

	var startRow, startCol int
	if targetInv == REPOSITORY {
		startRow, startCol = currentSession.LastPosRepo.Row, currentSession.LastPosRepo.Col
	} else {
		startRow, startCol = currentSession.LastPosBackpack.Row, currentSession.LastPosBackpack.Col
	}
	maxRows := RowsPerPage
	maxCols := targetInv.Columns()
	if startRow >= maxRows || startCol >= maxCols {
		startRow, startCol = 0, 0
	}

	if finalCategoryNode != "" && targetInv == REPOSITORY {
		status := ctx.RunTask(finalCategoryNode).Status

		if !status.Success() {
			log.Warn().Str("task", finalCategoryNode).Msg("Failed to switch category tab, trying scan anyway...")
		} else {
			log.Debug().Msg("Category switch successful.")
		}
	}

	log.Debug().
		Str("ItemName", finalItemName).
		Str("Target", targetInv.String()).
		Any("ContainerContent", taskParam["ContainerContent"]).
		Msg("Task parameters initialized")

	checkSlot := func(row, col int) (*maa.CustomRecognitionResult, bool) {
		img := MoveAndShot(ctx, targetInv, row, col)
		if img == nil {
			return nil, false
		}

		roi := TooltipRoi(targetInv, row, col)
		detail := ctx.RunRecognitionDirect(
			maa.NodeRecognitionTypeOCR,
			maa.NodeOCRParam{
				ROI:      maa.NewTargetRect(roi),
				OrderBy:  "Expected",
				Expected: []string{finalItemName},
			},
			img,
		)

		if detail.Hit {
			log.Info().Str("target", targetInv.String()).Int("r", row).Int("c", col).Msg("Item Found!")

			//  更新缓存：记录这次找到的位置
			newPoint := Point{row, col}
			if targetInv == REPOSITORY {
				currentSession.LastPosRepo = newPoint
			} else {
				currentSession.LastPosBackpack = newPoint
			}
			if targetInv == BACKPACK {
				currentSession.CurrentCount += 1
			}
			return &maa.CustomRecognitionResult{
				Box:    ItemBoxRoi(targetInv, row, col),
				Detail: detail.DetailJson,
			}, true
		}
		return nil, false
	}
	totalSlots := maxRows * maxCols
	startIndex := startRow*maxCols + startCol
	for i := 0; i < totalSlots; i++ {
		currentIndex := (startIndex + i) % totalSlots

		currentRow := currentIndex / maxCols
		currentCol := currentIndex % maxCols

		if res, ok := checkSlot(currentRow, currentCol); ok {
			return res, true
		}
	}

	return nil, false
	//todo: switch to next page

}

// const (
// 	OCRFilter = "^(?![^a-zA-Z0-9]*(?:升序|降序|默认|品质|一键存放|材料|战术物品|消耗品|功能设备|普通设备|培养晶核)[^a-zA-Z0-9]*$)[^a-zA-Z0-9]+$"
// )

type RepoLocate struct{}

func (*RepoLocate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 强制指定 REPOSITORY
	// 强制指定节点名 ItemTransferToBackpack 用于缓存
	return runLocate(ctx, arg, REPOSITORY, "ItemTransferToBackpack")
}

type BackpackLocate struct{}

func (*BackpackLocate) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 强制指定 BACKPACK
	// 强制指定节点名 ItemTransferToRepository 用于缓存
	return runLocate(ctx, arg, BACKPACK, "ItemTransferToRepository")
}

type TransferLimitChecker struct{}

func (*TransferLimitChecker) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	// 如果设置了上限，且当前次数已达标

	var taskParam map[string]any
	json.Unmarshal([]byte(arg.CustomRecognitionParam), &taskParam)
	inputMax := -1
	if v, ok := taskParam["MaxTimes"].(float64); ok {
		inputMax = int(v)
		log.Debug().
			Int("inputMax", inputMax).Msg("GoService: Limit Checker Running")
	}
	if inputMax >= 0 {
		currentSession.MaxTimes = inputMax
	}
	if currentSession.MaxTimes > 0 && currentSession.CurrentCount >= currentSession.MaxTimes {
		log.Info().
			Int("Count", currentSession.CurrentCount).
			Int("Max", currentSession.MaxTimes).
			Msg("GoService: Transfer limit reached. Signaling pipeline to stop.")

		return &maa.CustomRecognitionResult{}, true

	}

	// 返回 Miss (False)，表示“没达标，继续干”
	return nil, false
}
