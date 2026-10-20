package cultivation

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation/core"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
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
		return core.Header{}, errors.New("prototype requires 1280x720")
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

func readMaterialPage(ctx *maa.Context, img image.Image) ([]core.Material, bool, error) {
	texts, err := ocrTexts(ctx, img, [4]int{420, 245, 820, 420})
	if err != nil {
		return nil, false, err
	}
	for _, text := range texts {
		label := strings.ReplaceAll(text.Text, " ", "")
		if strings.HasPrefix(label, "拥有") {
			if _, err := core.Amount(strings.TrimPrefix(label, "拥有")); err != nil || text.Score < .6 {
				return nil, false, errors.New("unreadable owned-material slot")
			}
		}
	}
	var rows []core.Material
	for _, owned := range core.OwnedRows(texts) {
		x := owned.Box[0] + owned.Box[2]/2 - 40
		y := owned.Box[1] - 83
		roi := maa.Rect{x, y, 80, 80}
		if x < 0 || y < 0 || x+80 > 1280 || y+80 > 720 {
			return nil, false, errors.New("invalid icon ROI")
		}
		// Reuse upstream single_roi rather than importing SRC's icon matcher/runtime.
		filters := iconrecognition.StorageFilter()
		params := iconrecognition.NewParams(iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI), iconrecognition.WithItemFilters(filters.ValuableDepot.Any, filters.Normal.Any, filters.Isolate.Gold))
		patch := map[string]any{"CultivationIcon": map[string]any{"recognition": map[string]any{"type": "Custom", "param": map[string]any{"custom_recognition": iconrecognition.CustomRecognitionName, "custom_recognition_param": params, "roi": roi}}}}
		detail, err := ctx.RunRecognition("CultivationIcon", img, patch)
		if err != nil {
			return nil, false, err
		}
		item, err := identifyIcon(img, roi, detail)
		if err != nil {
			return nil, false, err
		}
		if strings.Contains(item.Name, "箱") {
			continue
		}
		numbers, err := ocrTexts(ctx, img, [4]int{owned.Box[0] - 5, owned.Box[1] - 32, 95, 32})
		if err != nil {
			return nil, false, err
		}
		values := map[int64]bool{}
		for _, text := range numbers {
			if text.Score >= .6 {
				if value, e := core.Amount(text.Text); e == nil {
					values[value] = true
				}
			}
		}
		if len(values) != 1 {
			return nil, false, fmt.Errorf("ambiguous material quantity: %s", item.ID)
		}
		var need int64
		for n := range values {
			need = n
		}
		have, err := core.Amount(strings.TrimPrefix(strings.ReplaceAll(owned.Text, " ", ""), "拥有"))
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, core.Material{ID: item.ID, Name: item.Name, Need: need, Owned: have, Unknown: item.Unknown})
	}
	empty := false
	for _, text := range texts {
		for _, s := range []string{"当前注入基质", "暂无养成需求", "暂无材料需求"} {
			if strings.Contains(text.Text, s) {
				empty = true
			}
		}
	}
	if len(rows) == 0 && !empty {
		return nil, false, errors.New("empty recognition is not proof of no demand")
	}
	return rows, empty, nil
}

func readWeapon(ctx *maa.Context, img image.Image) (core.Weapon, error) {
	texts, err := ocrTexts(ctx, img, [4]int{485, 295, 290, 65})
	if err != nil {
		return core.Weapon{}, err
	}
	var catalog map[string]struct {
		Name         string `json:"name"`
		CategoryType string `json:"categoryType"`
	}
	if err = resource.ReadJsonResource("data/IconRecognition/recognition_items.json", &catalog); err != nil {
		return core.Weapon{}, err
	}
	hits := map[string]map[string]bool{}
	for _, text := range texts {
		if text.Score < .6 {
			continue
		}
		for id, item := range catalog {
			if item.CategoryType == "Weapon" && item.Name == strings.ReplaceAll(text.Text, " ", "") {
				if hits[item.Name] == nil {
					hits[item.Name] = map[string]bool{}
				}
				hits[item.Name][id] = true
			}
		}
	}
	if len(hits) != 1 {
		return core.Weapon{}, errors.New("weapon name not uniquely confirmed")
	}
	for name, ids := range hits {
		weapon := core.Weapon{Name: name, EssenceStatus: "unverified_manual"}
		for id := range ids {
			weapon.CandidateIDs = append(weapon.CandidateIDs, id)
		}
		sort.Strings(weapon.CandidateIDs)
		if len(weapon.CandidateIDs) == 1 {
			weapon.ID = weapon.CandidateIDs[0]
		}
		return weapon, nil
	}
	return core.Weapon{}, errors.New("weapon missing")
}

// identifyIcon maps one IconRecognition result to a material identity. An icon
// absent from the catalog (no_match) becomes an unknown material so a new game
// item does not abort the whole scan; any other failure still aborts it.
func identifyIcon(img image.Image, roi maa.Rect, detail *maa.RecognitionDetail) (core.Material, error) {
	icons, _, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		return core.Material{}, err
	}
	if icons.Matched && len(icons.Matches) == 1 {
		item := icons.Matches[0]
		if len(item.Aliases) > 0 {
			return core.Material{}, fmt.Errorf("shared icon identity requires manual verification: %s", item.ItemID)
		}
		return core.Material{ID: item.ItemID, Name: item.Name}, nil
	}
	if icons.Matched || (icons.Error != nil && icons.Error.Code != iconrecognition.ErrorCodeNoMatch) {
		return core.Material{}, fmt.Errorf("material icon recognition failed at %v: %+v", roi, icons.Error)
	}
	cell := image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3])
	id := core.UnknownID(core.IconHash(img, cell))
	saveUnknownIcon(img, cell, id)
	log.Warn().Str("component", "Cultivation").Str("id", id).Ints("roi", roi[:]).
		Msg("material icon missing from IconRecognition catalog")
	return core.Material{ID: id, Unknown: true}, nil
}

// saveUnknownIcon keeps the crop under debug/ so the item can be reported upstream.
func saveUnknownIcon(img image.Image, cell image.Rectangle, id string) {
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, sub.SubImage(cell)); err != nil {
		return
	}
	path := fsutil.OutputPath("debug", "cultivation", id+".png")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Warn().Err(err).Str("component", "Cultivation").Str("path", path).Msg("failed to create unknown icon directory")
		return
	}
	if err := fsutil.WriteFileAtomic(path, buf.Bytes(), 0644); err != nil {
		log.Warn().Err(err).Str("component", "Cultivation").Str("path", path).Msg("failed to save unknown icon")
	}
}
