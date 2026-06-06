package pullcount

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// --- Warehouse Recognition --- //

const (
	voucherNodePermit  = "PullCountCalculatorFindCarryToNextPermit"
	voucherNodeDossier = "PullCountCalculatorFindHeadhuntingDossier"
	voucherNodeBond    = "PullCountCalculatorFindBondQuota"
	voucherQuantityOCR = "PullCountCalculatorVoucherQuantityOCR"
)

type voucherPageHit struct {
	Kind     string
	Quantity int
	Box      maa.Rect
}

// scanVoucherPage reads all pull-count warehouse item stacks visible on the current page.
func scanVoucherPage(ctx *maa.Context) ([]voucherPageHit, error) {
	img, err := captureCurrentPage(ctx)
	if err != nil {
		return nil, err
	}

	kinds := []struct {
		node string
		kind string
	}{
		{voucherNodePermit, voucherKindPermit},
		{voucherNodeDossier, voucherKindDossier},
		{voucherNodeBond, voucherKindBondQuota},
	}

	var hits []voucherPageHit
	for _, item := range kinds {
		itemHits, err := scanVoucherKind(ctx, img, item.node, item.kind)
		if err != nil {
			return nil, err
		}
		hits = append(hits, itemHits...)
	}
	return hits, nil
}

// captureCurrentPage takes one screenshot for all warehouse recognitions on this page.
func captureCurrentPage(ctx *maa.Context) (image.Image, error) {
	controller := ctx.GetTasker().GetController()
	if !controller.PostScreencap().Wait().Success() {
		return nil, fmt.Errorf("warehouse screenshot failed")
	}
	img, err := controller.CacheImage()
	if err != nil {
		return nil, fmt.Errorf("warehouse screenshot cache failed: %w", err)
	}
	return img, nil
}

// scanVoucherKind locates one pull-count item kind and reads each matched stack quantity.
func scanVoucherKind(ctx *maa.Context, img image.Image, nodeName string, kind string) ([]voucherPageHit, error) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", nodeName, err)
	}
	if detail == nil || !detail.Hit {
		return nil, nil
	}

	boxes := templateMatchBoxes(detail)
	if len(boxes) == 0 {
		return nil, fmt.Errorf("%s hit without template boxes", nodeName)
	}
	hits := make([]voucherPageHit, 0, len(boxes))
	for _, box := range boxes {
		quantity, err := readVoucherQuantity(ctx, img, box)
		if err != nil {
			return nil, err
		}
		hits = append(hits, voucherPageHit{
			Kind:     kind,
			Quantity: quantity,
			Box:      box,
		})
	}
	return hits, nil
}

// templateMatchBoxes extracts template boxes from a MaaFramework recognition detail.
func templateMatchBoxes(detail *maa.RecognitionDetail) []maa.Rect {
	if detail == nil || detail.Results == nil {
		return nil
	}

	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}
	if len(results) == 0 && detail.Results.Best != nil {
		results = []*maa.RecognitionResult{detail.Results.Best}
	}

	boxes := make([]maa.Rect, 0, len(results))
	for _, result := range results {
		template, ok := result.AsTemplateMatch()
		if ok {
			boxes = append(boxes, template.Box)
		}
	}
	return boxes
}

// readVoucherQuantity OCRs the item stack number near one template hit box.
func readVoucherQuantity(ctx *maa.Context, img image.Image, box maa.Rect) (int, error) {
	detail, err := ctx.RunRecognition(voucherQuantityOCR, img, map[string]any{
		voucherQuantityOCR: map[string]any{
			"roi": voucherQuantityROI(box),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("%s failed: %w", voucherQuantityOCR, err)
	}

	quantity, err := readIntegerFromRecognition(detail)
	if err != nil {
		return 0, fmt.Errorf("voucher quantity OCR failed at %s: %w", voucherKey(box), err)
	}
	return quantity, nil
}

// voucherQuantityROI returns the counter area at the bottom of a warehouse item card.
func voucherQuantityROI(box maa.Rect) maa.Rect {
	width := box.Width()
	if width < 32 {
		width = 32
	}
	return maa.Rect{
		box.X(),
		box.Y() + box.Height() - 10,
		width,
		34,
	}
}
