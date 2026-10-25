package cultivation

import (
	"errors"
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func ocrTexts(ctx *maa.Context, img image.Image, roi [4]int) ([]core.Text, error) {
	const node = "CultivationOCR"
	patch := map[string]any{node: map[string]any{"recognition": map[string]any{"type": "OCR", "param": map[string]any{"roi": roi}}}}
	detail, err := ctx.RunRecognition(node, img, patch)
	if err != nil {
		return nil, err
	}
	detail, err = recogtarget.SelectDetail(ctx, node, detail)
	if err != nil {
		return nil, err
	}
	if detail == nil || detail.Results == nil {
		return nil, errors.New("missing OCR detail")
	}
	results := detail.Results.Filtered
	if len(results) == 0 {
		results = detail.Results.All
	}
	var out []core.Text
	for _, r := range results {
		if r == nil {
			continue
		}
		if ocr, ok := r.AsOCR(); ok {
			out = append(out, core.Text{Text: ocr.Text, Box: [4]int(ocr.Box), Score: ocr.Score})
		}
	}
	return out, nil
}

func header(ctx *maa.Context, img image.Image) (core.Header, error) {
	if img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 720 {
		return core.Header{}, errors.New("the cultivation scan requires 1280x720")
	}
	// Avoid account currency/UID areas while retaining the overview title and identity.
	title, err := ocrTexts(ctx, img, [4]int{35, 20, 165, 42})
	if err != nil {
		return core.Header{}, err
	}
	identity, err := ocrTexts(ctx, img, [4]int{470, 75, 740, 85})
	if err != nil {
		return core.Header{}, err
	}
	return core.ReadHeader(append(title, identity...))
}

// readMaterialPage identifies only slots whose owned quantity is red and counts
// red labels whose icon was not identified. White quantities are never
// converted to goals or persisted.
func readMaterialPage(ctx *maa.Context, img image.Image, texts []core.Text, scrolled bool) ([]core.Material, int, bool, error) {
	texts = core.SplitOwned(texts)
	ownedRows := core.OwnedRows(texts, scrolled)
	var missing []core.Material
	unrecognized := 0
	for _, owned := range ownedRows {
		if !core.RedOwned(img, owned.Box) {
			continue
		}
		cell := core.AlignCellX(img, image.Rect(owned.Box[0]+owned.Box[2]/2-40, owned.Box[1]-83, owned.Box[0]+owned.Box[2]/2+40, owned.Box[1]-3), 24)
		if !cell.In(img.Bounds()) {
			return nil, 0, false, errors.New("invalid material icon ROI")
		}
		params := iconrecognition.NewParams(iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI),
			iconrecognition.WithItemFilters("ValuableDepot:SpecialItem", "Normal:Nurturance", "Isolate:Gold", "ValuableDepot:CommercialItem"),
			iconrecognition.WithThreshold(0.75))
		roi := maa.Rect{cell.Min.X, cell.Min.Y, 80, 80}
		detail, err := ctx.RunRecognition("CultivationIcon", img, map[string]any{"CultivationIcon": map[string]any{"recognition": map[string]any{"type": "Custom", "param": map[string]any{
			"custom_recognition": iconrecognition.CustomRecognitionName, "custom_recognition_param": params, "roi": roi}}}})
		if err != nil {
			return nil, 0, false, err
		}
		icons, _, err := iconrecognition.ParseRecognitionDetail(detail)
		if err != nil {
			return nil, 0, false, err
		}
		if !icons.Matched || len(icons.Matches) != 1 || len(icons.Matches[0].Aliases) != 0 {
			log.Warn().Str("component", "Cultivation").Ints("roi", roi[:]).Msg("unrecognized missing material; skipped")
			unrecognized++
			continue
		}
		item := icons.Matches[0]
		if item.CategoryType == "CommercialItem" {
			continue
		}
		missing = append(missing, core.Material{ID: item.ItemID, Name: item.Name})
	}
	observed := len(ownedRows) > 0 || core.NothingNeeded(texts)
	if !observed {
		return nil, 0, false, errors.New("material page could not be read")
	}
	return missing, unrecognized, observed, nil
}
