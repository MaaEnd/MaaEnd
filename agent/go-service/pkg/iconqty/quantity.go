package iconqty

import (
	"fmt"
	"image"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// quantityBandHeightAtBaseScale is the quantity text band height at
// IconRecognition grid_scale 1.0. Larger controller profiles (ADB is 1.25)
// are recovered from the actual cell_box, not from a second constant.
const quantityBandHeightAtBaseScale = 18

// 历史标定值：quantity OCR 曾按控制器类型选择固定 roi_offset（96px Win32 格 /
// 120px ADB 格）。运行时已改为 QuantityROIFromCellBox 从 grid_type + 实际
// cell_box 推导，这里仅保留标定记录，不参与识别路径。
var (
	QuantityROIOffsetWin32 = [4]int{0, 78, 0, -78}
	QuantityROIOffsetADB   = [4]int{0, 98, 0, -98}
)

// QuantityHit is one IconRecognition match with OCR'd stack quantity.
type QuantityHit struct {
	ItemID string
	Qty    int
}

func quantityBaseCellHeight(gridType string) (int, bool) {
	switch strings.TrimSpace(gridType) {
	case GridTransfer:
		return 64, true
	case GridValuables, GridRewards:
		return 96, true
	default:
		return 0, false
	}
}

// ApplyROIOffset applies a Maa-style roi_offset [x,y,w,h] to box.
// 同上，运行时已无调用方。
func ApplyROIOffset(box maa.Rect, off [4]int) (maa.Rect, bool) {
	out := maa.Rect{
		box[0] + off[0],
		box[1] + off[1],
		box[2] + off[2],
		box[3] + off[3],
	}
	if out[2] <= 0 || out[3] <= 0 {
		return maa.Rect{}, false
	}
	return out, true
}

// QuantityROIFromCellBox returns the quantity text band at the bottom of a
// grid-specific IconRecognition cell. The actual cell height carries the
// controller scale because IconRecognition maps cells back to source pixels.
func QuantityROIFromCellBox(gridType string, cell maa.Rect) (maa.Rect, error) {
	if cell[2] <= 0 || cell[3] <= 0 {
		return maa.Rect{}, fmt.Errorf("invalid cell_box %v", cell)
	}
	baseCellHeight, ok := quantityBaseCellHeight(gridType)
	if !ok {
		return maa.Rect{}, fmt.Errorf("unsupported quantity grid_type %q", strings.TrimSpace(gridType))
	}
	quantityHeight := cell[3] * quantityBandHeightAtBaseScale / baseCellHeight
	if quantityHeight <= 0 || quantityHeight > cell[3] {
		return maa.Rect{}, fmt.Errorf(
			"invalid quantity band for grid_type %q and cell_box %v",
			strings.TrimSpace(gridType),
			cell,
		)
	}
	return maa.Rect{
		cell[0],
		cell[1] + cell[3] - quantityHeight,
		cell[2],
		quantityHeight,
	}, nil
}

// RecognizeQuantities runs one IconRecognition pass, then OCRs quantity from
// each match cell_box. Returns one hit per match (no ID aggregation) so
// callers can add multiple stacks (A3) or overwrite by ID (A2).
//
// Failures are graded. A grid_type with no calibrated quantity band is a
// config error and fails the whole call. A single unusable cell only skips
// that cell: one malformed cell must not abort a depot sync (A2) or block
// closing the rewards UI (A3, see ims.AddItemData).
func RecognizeQuantities(ctx *maa.Context, img image.Image, req Request) ([]QuantityHit, error) {
	matches, err := recognizeIcons(ctx, img, req)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	// Checked after the empty-match return so that "screen has no cards" stays
	// a success for A3 (TolerateEmptyGrid), and before the loop because an
	// uncalibrated grid can never yield a band for any cell.
	if _, ok := quantityBaseCellHeight(req.GridType); !ok {
		return nil, fmt.Errorf("unsupported quantity grid_type %q", strings.TrimSpace(req.GridType))
	}

	out := make([]QuantityHit, 0, len(matches))
	roiSkipped, ocrMissed := 0, 0
	for _, m := range matches {
		itemID := strings.TrimSpace(m.ItemID)
		if itemID == "" {
			continue
		}
		if m.CellBox[2] <= 0 || m.CellBox[3] <= 0 {
			return nil, fmt.Errorf("IconRecognition match missing cell_box for %s", itemID)
		}
		// grid_type is already validated, so a failure here is a degenerate
		// cell: skip it instead of losing every other item on the page.
		qtyROI, err := QuantityROIFromCellBox(req.GridType, m.CellBox)
		if err != nil {
			roiSkipped++
			log.Info().
				Err(err).
				Str("component", "iconqty").
				Str("item_id", itemID).
				Str("grid_type", req.GridType).
				Interface("cell_box", m.CellBox).
				Msg("quantity roi unavailable for cell, skip")
			continue
		}
		qty, hit, err := RecognizeQuantityInROI(ctx, img, qtyROI)
		if err != nil {
			return nil, fmt.Errorf("quantity ocr for %s: %w", itemID, err)
		}
		if !hit {
			ocrMissed++
			log.Info().
				Str("component", "iconqty").
				Str("item_id", itemID).
				Msg("icon matched but quantity ocr missed, skip")
			continue
		}
		out = append(out, QuantityHit{ItemID: itemID, Qty: qty})
	}
	log.Info().
		Str("component", "iconqty").
		Str("grid_type", req.GridType).
		Int("matched", len(matches)).
		Int("quantity_hits", len(out)).
		Int("roi_skipped", roiSkipped).
		Int("ocr_missed", ocrMissed).
		Msg("quantity recognition finished")
	return out, nil
}

// RecognizeQuantityInROI OCRs a numeric quantity inside roi.
func RecognizeQuantityInROI(ctx *maa.Context, img image.Image, roi maa.Rect) (int, bool, error) {
	if ctx == nil {
		return 0, false, fmt.Errorf("nil context")
	}
	if img == nil {
		return 0, false, fmt.Errorf("nil image")
	}
	if roi[2] <= 0 || roi[3] <= 0 {
		return 0, false, fmt.Errorf("invalid quantity roi")
	}
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		&maa.OCRParam{
			ROI:      maa.NewTargetRect(roi),
			Expected: []string{`\d`},
			OnlyRec:  true,
		},
		img,
	)
	if err != nil {
		return 0, false, fmt.Errorf("ocr quantity: %w", err)
	}
	if detail == nil || !detail.Hit {
		return 0, false, nil
	}
	qty, err := ocrnum.Extract(detail)
	if err != nil {
		log.Info().
			Err(err).
			Str("component", "iconqty").
			Interface("roi", roi).
			Msg("quantity ocr hit but numeric parse failed, treat as miss")
		return 0, false, nil
	}
	return qty, true, nil
}

// ItemDisplayName resolves a localized display name from IconRecognition i18n
// (iconRecognition.name.<id> in interface locales). Missing names fall back to the
// original item ID.
func ItemDisplayName(itemID string) string {
	original := strings.TrimSpace(itemID)
	if original == "" {
		return itemID
	}
	key := "iconRecognition.name." + original
	name := i18n.T(key)
	if name != key && strings.TrimSpace(name) != "" {
		return name
	}
	return original
}
