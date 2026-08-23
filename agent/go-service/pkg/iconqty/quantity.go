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

// Quantity OCR uses Maa roi_offset relative to IconRecognition cell_box.
// Win32: [0, 78, 0, -78]; ADB: [0, 98, 0, -98].
var (
	QuantityROIOffsetWin32 = [4]int{0, 78, 0, -78}
	QuantityROIOffsetADB   = [4]int{0, 98, 0, -98}
)

// QuantityHit is one IconRecognition match with OCR'd stack quantity.
type QuantityHit struct {
	ItemID string
	Qty    int
}

func quantityROIOffset() [4]int {
	if isADBController() {
		return QuantityROIOffsetADB
	}
	return QuantityROIOffsetWin32
}

// ApplyROIOffset applies a Maa-style roi_offset [x,y,w,h] to box.
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

// QuantityROIFromCellBox applies the controller-specific quantity roi_offset.
func QuantityROIFromCellBox(cell maa.Rect) (maa.Rect, bool) {
	return ApplyROIOffset(cell, quantityROIOffset())
}

// RecognizeQuantities runs one IconRecognition pass, then OCRs quantity from
// each match cell_box. Returns one hit per match (no ID aggregation) so
// callers can add multiple stacks (A3) or overwrite by ID (A2).
func RecognizeQuantities(ctx *maa.Context, img image.Image, req Request) ([]QuantityHit, error) {
	matches, err := recognizeIcons(ctx, img, req)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	out := make([]QuantityHit, 0, len(matches))
	for _, m := range matches {
		itemID := strings.TrimSpace(m.ItemID)
		if itemID == "" {
			continue
		}
		if m.CellBox[2] <= 0 || m.CellBox[3] <= 0 {
			return nil, fmt.Errorf("IconRecognition match missing cell_box for %s", itemID)
		}
		qtyROI, ok := QuantityROIFromCellBox(m.CellBox)
		if !ok {
			log.Info().
				Str("component", "iconqty").
				Str("item_id", itemID).
				Interface("cell_box", m.CellBox).
				Msg("quantity roi invalid after offset, skip")
			continue
		}
		qty, hit, err := RecognizeQuantityInROI(ctx, img, qtyROI)
		if err != nil {
			return nil, fmt.Errorf("quantity ocr for %s: %w", itemID, err)
		}
		if !hit {
			log.Info().
				Str("component", "iconqty").
				Str("item_id", itemID).
				Msg("icon matched but quantity ocr missed, skip")
			continue
		}
		out = append(out, QuantityHit{ItemID: itemID, Qty: qty})
	}
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
