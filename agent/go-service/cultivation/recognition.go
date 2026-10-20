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

// identifyCell recognizes one icon cell. Overlapping pages show the same cells
// again, so known identities are reused within a pass by icon hash; each pass
// keeps its own cache so the second pass still verifies the first.
func identifyCell(ctx *maa.Context, img image.Image, roi maa.Rect, cell image.Rectangle, cache map[uint64]iconIdentity) (core.Material, bool, error) {
	hash := core.IconHash(img, cell)
	if known, ok := cache[hash]; ok {
		return known.Material, known.Box, nil
	}
	// Reuse upstream single_roi rather than importing SRC's icon matcher/runtime.
	filters := iconrecognition.StorageFilter()
	// Cultivation materials are all 培养素材 or 折金票; boxes are kept so they can be
	// skipped. A narrow candidate set is far faster than all storage items.
	// Plant icons on this page score 0.81-0.90 for the right item (quantity and
	// rarity bar overlap the art) while the best wrong candidate reached 0.61 on
	// CloudADB, so 0.75 keeps a clear margin where the default 0.85 misses.
	params := iconrecognition.NewParams(iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI), iconrecognition.WithItemFilters(
		filters.ValuableDepot.SpecialItem, filters.Normal.Nurturance, filters.Isolate.Gold, filters.ValuableDepot.CommercialItem),
		iconrecognition.WithThreshold(0.75))
	patch := map[string]any{"CultivationIcon": map[string]any{"recognition": map[string]any{"type": "Custom", "param": map[string]any{"custom_recognition": iconrecognition.CustomRecognitionName, "custom_recognition_param": params, "roi": roi}}}}
	detail, err := ctx.RunRecognition("CultivationIcon", img, patch)
	if err != nil {
		return core.Material{}, false, err
	}
	item, box, err := identifyIcon(img, roi, detail)
	if err != nil {
		return core.Material{}, false, err
	}
	// Unknown results are never cached: they must be confirmed on a fresh frame.
	if !item.Unknown && cache != nil {
		cache[hash] = iconIdentity{Material: item, Box: box}
	}
	return item, box, nil
}

// iconIdentity is a cached icon recognition result within one pass.
type iconIdentity struct {
	Material core.Material
	Box      bool
}

// readMaterialPage reads one viewport. section is the weapon section continuing
// from the previous page ("" for the operator); it returns the section in effect
// at the page bottom and the weapon headers seen.
func readMaterialPage(ctx *maa.Context, img image.Image, section string, catalog map[string][]string, cache map[uint64]iconIdentity) ([]core.Material, bool, string, []string, error) {
	texts, err := ocrTexts(ctx, img, [4]int{420, 245, 820, 420})
	if err != nil {
		return nil, false, "", nil, err
	}
	texts = core.SplitOwned(texts)
	headers := weaponHeaders(texts, catalog)
	var names []string
	for _, h := range headers {
		names = append(names, h.Text)
	}
	sectionAt := func(y int) string {
		current := section
		for _, h := range headers {
			if h.Box[1] < y {
				current = h.Text
			}
		}
		return current
	}
	for _, text := range texts {
		label := strings.ReplaceAll(text.Text, " ", "")
		if strings.HasPrefix(label, "拥有") {
			if _, err := core.Amount(strings.TrimPrefix(label, "拥有")); err != nil || text.Score < .6 {
				return nil, false, "", nil, fmt.Errorf("unreadable owned-material slot %q at %v (score %.2f)", text.Text, text.Box, text.Score)
			}
		}
	}
	var rows []core.Material
	for _, owned := range core.OwnedRows(texts) {
		estimate := image.Rect(owned.Box[0]+owned.Box[2]/2-40, owned.Box[1]-83, owned.Box[0]+owned.Box[2]/2+40, owned.Box[1]-3)
		cell := core.AlignCellX(img, estimate, 12)
		if !cell.In(image.Rect(0, 0, 1280, 720)) {
			return nil, false, "", nil, errors.New("invalid icon ROI")
		}
		roi := maa.Rect{cell.Min.X, cell.Min.Y, 80, 80}
		item, box, err := identifyCell(ctx, img, roi, cell, cache)
		if err != nil {
			return nil, false, "", nil, err
		}
		if box {
			// Boxes listed beside a material are optional substitutes, not a need.
			continue
		}
		// The required amount is printed on the bottom strip of the card itself.
		numbers, err := ocrTexts(ctx, img, [4]int{cell.Min.X, cell.Min.Y + 52, 80, 28})
		if err != nil {
			return nil, false, "", nil, err
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
			return nil, false, "", nil, fmt.Errorf("ambiguous material quantity: %s", item.ID)
		}
		var need int64
		for n := range values {
			need = n
		}
		have, err := core.Amount(strings.TrimPrefix(strings.ReplaceAll(owned.Text, " ", ""), "拥有"))
		if err != nil {
			return nil, false, "", nil, err
		}
		rows = append(rows, core.Material{ID: item.ID, Name: item.Name, Need: need, Owned: have, Unknown: item.Unknown, Weapon: sectionAt(cell.Min.Y)})
	}
	empty := false
	for _, text := range texts {
		for _, s := range []string{"当前注入基质", "暂无养成需求", "暂无材料需求"} {
			if strings.Contains(text.Text, s) {
				empty = true
			}
		}
	}
	if len(rows) == 0 && !empty && len(headers) == 0 {
		return nil, false, "", nil, errors.New("empty recognition is not proof of no demand")
	}
	return rows, empty, sectionAt(720), names, nil
}

// weaponCatalog maps each weapon display name to its catalog IDs; one name can
// cover several IDs (e.g. skins), which stay candidates.
func weaponCatalog() (map[string][]string, error) {
	var catalog map[string]struct {
		Name         string `json:"name"`
		CategoryType string `json:"categoryType"`
	}
	if err := resource.ReadJsonResource("data/IconRecognition/recognition_items.json", &catalog); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for id, item := range catalog {
		if item.CategoryType == "Weapon" {
			out[item.Name] = append(out[item.Name], id)
		}
	}
	for name := range out {
		sort.Strings(out[name])
	}
	return out, nil
}

// weaponHeaders returns the 干员可用武器 section headers on a page, top first:
// a catalogued weapon name in the left part of the header bar.
func weaponHeaders(texts []core.Text, catalog map[string][]string) []core.Text {
	var headers []core.Text
	for _, text := range texts {
		name := strings.ReplaceAll(text.Text, " ", "")
		if text.Score >= .6 && text.Box[0] >= 480 && text.Box[0] <= 760 && catalog[name] != nil {
			text.Text = name
			headers = append(headers, text)
		}
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].Box[1] < headers[j].Box[1] })
	return headers
}

// boxCategory is the IconRecognition category of every selectable/random box.
const boxCategory = "CommercialItem"

// identifyIcon maps one IconRecognition result to a material identity. An icon
// absent from the catalog (no_match) becomes an unknown material so a new game
// item does not abort the whole scan; any other failure still aborts it.
func identifyIcon(img image.Image, roi maa.Rect, detail *maa.RecognitionDetail) (core.Material, bool, error) {
	icons, _, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		return core.Material{}, false, err
	}
	if icons.Matched && len(icons.Matches) == 1 {
		item := icons.Matches[0]
		if len(item.Aliases) > 0 {
			return core.Material{}, false, fmt.Errorf("shared icon identity requires manual verification: %s", item.ItemID)
		}
		return core.Material{ID: item.ItemID, Name: item.Name}, item.CategoryType == boxCategory, nil
	}
	if icons.Matched || (icons.Error != nil && icons.Error.Code != iconrecognition.ErrorCodeNoMatch) {
		return core.Material{}, false, fmt.Errorf("material icon recognition failed at %v: %+v", roi, icons.Error)
	}
	cell := image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3])
	id := core.UnknownID(core.IconHash(img, cell))
	saveUnknownIcon(img, cell, id)
	log.Warn().Str("component", "Cultivation").Str("id", id).Ints("roi", roi[:]).
		Msg("material icon missing from IconRecognition catalog")
	return core.Material{ID: id, Unknown: true}, false, nil
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
