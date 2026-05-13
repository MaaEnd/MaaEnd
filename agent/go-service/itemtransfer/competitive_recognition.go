package itemtransfer

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

const (
	itemSize       = 64
	rarityWidth    = 66
	rarityHeight   = 5
	suppressFactor = 0.75
	itemsImageDir  = "Items"

	positionExtendFactor = 0.1
)

var greenColor = color.RGBA{0, 255, 0, 255}

// compRecogParams holds the parameters parsed from pipeline JSON.
type compRecogParams struct {
	Item            string      `json:"item"`
	Rarity          int         `json:"rarity"`
	RarityThreshold  float64    `json:"rarity_threshold"`
	ItemThreshold    float64    `json:"item_threshold"`
	ItemScale        float64    `json:"item_scale"`
	ItemCropOffset   [4]float64 `json:"item_crop_offset"`
}

// detection is a single detected item with position and confidence.
type detection struct {
	name  string
	x, y  int
	score float64
}

// scaledTemplate is a preprocessed template ready for NCC matching.
type scaledTemplate struct {
	name  string
	img   *image.RGBA
	stats minicv.StatsResult
}

// nccMap holds the full NCC score map for a template over an ROI.
type nccMap struct {
	scores []float64
	w, h   int
	offX   int // ROI origin X in image coordinates
	offY   int // ROI origin Y in image coordinates
}

// CompetitiveRecognition implements competitive template matching for item icons.
type CompetitiveRecognition struct {
	mu          sync.Mutex
	itemCache   map[int][]*scaledTemplate // rarity → item templates
	rarityCache map[int]*scaledTemplate   // rarity → rarity bar template
}

var _ maa.CustomRecognitionRunner = &CompetitiveRecognition{}

// Run is the main entry point for the custom recognition.
func (r *CompetitiveRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}

	var params compRecogParams
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("competitive recognition: failed to parse params")
		return nil, false
	}

	// Step 0: ensure templates are preprocessed
	itemTpls, err := r.ensureItemTemplates(params.Rarity, params.ItemScale, params.ItemCropOffset)
	if err != nil {
		log.Warn().Err(err).Int("rarity", params.Rarity).Msg("competitive recognition: failed to load item templates")
		return nil, false
	}
	rarityTpl, err := r.ensureRarityTemplate(params.Rarity)
	if err != nil {
		log.Warn().Err(err).Int("rarity", params.Rarity).Msg("competitive recognition: failed to load rarity template")
		return nil, false
	}

	// Prepare screenshot
	img := minicv.ImageConvertRGBA(arg.Img)
	imgArr := minicv.GetIntegralArray(img)

	// Step 1: detect rarity bars
	roi := [4]int{arg.Roi[0], arg.Roi[1], arg.Roi[2], arg.Roi[3]}
	rarityHits := findAllMatches(img, imgArr, rarityTpl.img, rarityTpl.stats, roi, params.RarityThreshold)
	if len(rarityHits) == 0 {
		log.Debug().Str("component", componentName).Msg("competitive recognition: no rarity bars detected")
		return nil, false
	}

	// Step 2: competitive matching for each rarity bar
	var allDetections []detection
	for _, rHit := range rarityHits {
		itemROI := computeItemROI(rHit.x, rHit.y)
		dets := competitiveMatch(img, imgArr, itemTpls, itemROI, params.ItemThreshold)
		allDetections = append(allDetections, dets...)
	}

	// Step 3: find target item
	for _, det := range allDetections {
		if det.name == params.Item {
			detailJSON, _ := json.Marshal(map[string]any{
				"name":  det.name,
				"score": det.score,
			})
			return &maa.CustomRecognitionResult{
				Box:    maa.Rect{det.x, det.y, itemSize, itemSize},
				Detail: string(detailJSON),
			}, true
		}
	}

	log.Debug().Str("item", params.Item).Msg("competitive recognition: target item not found")
	return nil, false
}

// --- Template preprocessing ---

func (r *CompetitiveRecognition) ensureItemTemplates(rarity int, scale float64, cropOffset [4]float64) ([]*scaledTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.itemCache == nil {
		r.itemCache = make(map[int][]*scaledTemplate)
	}
	if tpls, ok := r.itemCache[rarity]; ok {
		return tpls, nil
	}

	dir := resource.FindResource(filepath.Join("resource", "image", itemsImageDir, fmt.Sprintf("r%d", rarity)))
	if dir == "" {
		return nil, fmt.Errorf("item template directory not found for rarity %d", rarity)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read item template dir: %w", err)
	}

	var tpls []*scaledTemplate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".png") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		imgPath := filepath.Join(dir, entry.Name())

		tplImg, err := preprocessTemplate(imgPath, scale, itemSize, itemSize, cropOffset)
		if err != nil {
			log.Warn().Err(err).Str("template", name).Msg("competitive recognition: failed to preprocess template")
			continue
		}

		stats := minicv.GetImageStats(tplImg)
		if stats.Std < 1e-6 {
			log.Warn().Str("template", name).Msg("competitive recognition: template has near-zero std, skipping")
			continue
		}

		tpls = append(tpls, &scaledTemplate{
			name:  name,
			img:   tplImg,
			stats: stats,
		})
	}

	if len(tpls) == 0 {
		return nil, fmt.Errorf("no valid item templates for rarity %d", rarity)
	}

	r.itemCache[rarity] = tpls
	log.Info().
		Str("component", componentName).
		Int("rarity", rarity).
		Int("count", len(tpls)).
		Msg("competitive recognition: item templates loaded")
	return tpls, nil
}

func (r *CompetitiveRecognition) ensureRarityTemplate(rarity int) (*scaledTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rarityCache == nil {
		r.rarityCache = make(map[int]*scaledTemplate)
	}
	if tpl, ok := r.rarityCache[rarity]; ok {
		return tpl, nil
	}

	imgPath := resource.FindResource(filepath.Join("resource", "image", itemsImageDir, fmt.Sprintf("rarity_%d.png", rarity)))
	if imgPath == "" {
		return nil, fmt.Errorf("rarity template not found for rarity %d", rarity)
	}

	f, err := os.Open(imgPath)
	if err != nil {
		return nil, fmt.Errorf("open rarity template: %w", err)
	}
	defer f.Close()

	rawImg, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode rarity template: %w", err)
	}

	// Rarity bars are already 66×5 RGB, no scaling needed.
	// Pad/crop to exact rarityWidth × rarityHeight just in case.
	rgba := padOrCrop(minicv.ImageConvertRGBA(rawImg), rarityWidth, rarityHeight)
	stats := minicv.GetImageStats(rgba)

	tpl := &scaledTemplate{
		name:  fmt.Sprintf("rarity_%d", rarity),
		img:   rgba,
		stats: stats,
	}
	r.rarityCache[rarity] = tpl
	return tpl, nil
}

// preprocessTemplate loads an original template image, scales it, pads/crops to
// target size, and applies green masking for crop offset and low-alpha pixels.
func preprocessTemplate(path string, scale float64, targetW, targetH int, cropOffset [4]float64) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open template: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode template: %w", err)
	}

	// Convert to RGBA and extract alpha
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	alpha := image.NewAlpha(bounds)
	hasAlpha := false
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			alphaAlpha := uint8(a >> 8)
			alpha.SetAlpha(x, y, color.Alpha{A: alphaAlpha})
			if alphaAlpha < 196 {
				hasAlpha = true
			}
		}
	}

	// Scale
	if scale != 1.0 && scale > 0 {
		rgba = minicv.ImageScale(rgba, scale)
		if hasAlpha {
			alphaImg := minicv.ImageScale(imageToRGBA(alpha), scale)
			b2 := alphaImg.Bounds()
			alpha = image.NewAlpha(b2)
			draw.Draw(alpha, b2, alphaImg, b2.Min, draw.Src)
		}
	}

	// Pad/crop to target size
	result := padOrCrop(rgba, targetW, targetH)

	// Apply crop offset mask (fill masked areas with green)
	topR, rightR, bottomR, leftR := cropOffset[0], cropOffset[1], cropOffset[2], cropOffset[3]
	if topR > 0 {
		fillRect(result, 0, 0, targetW, int(float64(targetH)*topR), greenColor)
	}
	if bottomR > 0 {
		y0 := targetH - int(float64(targetH)*bottomR)
		fillRect(result, 0, y0, targetW, targetH, greenColor)
	}
	if leftR > 0 {
		fillRect(result, 0, 0, int(float64(targetW)*leftR), targetH, greenColor)
	}
	if rightR > 0 {
		x0 := targetW - int(float64(targetW)*rightR)
		fillRect(result, x0, 0, targetW, targetH, greenColor)
	}

	// Apply alpha mask (fill low-alpha pixels with green)
	if hasAlpha {
		b2 := result.Bounds()
		aBounds := alpha.Bounds()
		for y := 0; y < b2.Dy() && y < aBounds.Dy(); y++ {
			for x := 0; x < b2.Dx() && x < aBounds.Dx(); x++ {
				if alpha.AlphaAt(aBounds.Min.X+x, aBounds.Min.Y+y).A < 196 {
					result.SetRGBA(b2.Min.X+x, b2.Min.Y+y, greenColor)
				}
			}
		}
	}

	return result, nil
}

// --- Image utilities ---

// padOrCrop centers the image in a targetW×targetH canvas, padding with green or cropping.
func padOrCrop(img *image.RGBA, targetW, targetH int) *image.RGBA {
	srcW, srcH := img.Rect.Dx(), img.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))

	// Fill with green
	for i := range dst.Pix {
		if i%4 == 0 {
			dst.Pix[i] = greenColor.R
			dst.Pix[i+1] = greenColor.G
			dst.Pix[i+2] = greenColor.B
			dst.Pix[i+3] = 255
		}
	}

	// Center source into destination
	dx := (targetW - srcW) / 2
	dy := (targetH - srcH) / 2

	srcX0, srcY0 := 0, 0
	dstX0, dstY0 := dx, dy

	// If source is larger, crop from center
	if srcW > targetW {
		cropLeft := (srcW - targetW) / 2
		srcX0 = cropLeft
		dstX0 = 0
		srcW = targetW
	}
	if srcH > targetH {
		cropTop := (srcH - targetH) / 2
		srcY0 = cropTop
		dstY0 = 0
		srcH = targetH
	}

	for y := 0; y < srcH; y++ {
		srcOff := (srcY0 + y) * img.Stride + srcX0*4
		dstOff := (dstY0 + y) * dst.Stride + dstX0*4
		copy(dst.Pix[dstOff:dstOff+srcW*4], img.Pix[srcOff:srcOff+srcW*4])
	}

	return dst
}

func imageToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

func fillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	b := img.Bounds()
	x1 = max(b.Min.X, x1)
	y1 = max(b.Min.Y, y1)
	x2 = min(b.Max.X, x2)
	y2 = min(b.Max.Y, y2)
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// --- NCC map computation ---

func computeNCCMap(img *image.RGBA, imgArr minicv.IntegralArray, tplImg *image.RGBA, tplStats minicv.StatsResult, roi [4]int) *nccMap {
	imgW, imgH := img.Rect.Dx(), img.Rect.Dy()
	tplW, tplH := tplImg.Rect.Dx(), tplImg.Rect.Dy()

	// Compute search bounds for top-left corner
	minX := max(0, roi[0])
	minY := max(0, roi[1])
	maxX := min(imgW-tplW, roi[0]+roi[2])
	maxY := min(imgH-tplH, roi[1]+roi[3])

	if maxX < minX || maxY < minY {
		return nil
	}

	mapW := maxX - minX + 1
	mapH := maxY - minY + 1
	scores := make([]float64, mapW*mapH)

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			scores[(y-minY)*mapW+(x-minX)] = minicv.ComputeNCC(img, imgArr, tplImg, tplStats, x, y)
		}
	}

	return &nccMap{
		scores: scores,
		w:      mapW,
		h:      mapH,
		offX:   minX,
		offY:   minY,
	}
}

func (m *nccMap) max() (int, int, float64) {
	bestIdx := 0
	bestVal := m.scores[0]
	for i, v := range m.scores {
		if v > bestVal {
			bestVal = v
			bestIdx = i
		}
	}
	mx := bestIdx % m.w
	my := bestIdx / m.w
	return mx, my, bestVal
}

func (m *nccMap) suppress(cx, cy, sw, sh int) {
	x1 := max(0, cx)
	y1 := max(0, cy)
	x2 := min(m.w, cx+sw)
	y2 := min(m.h, cy+sh)
	for y := y1; y < y2; y++ {
		for x := x1; x < x2; x++ {
			m.scores[y*m.w+x] = -1.0
		}
	}
}

// --- Detection logic ---

// findAllMatches finds all non-overlapping matches of a template above threshold using NMS.
func findAllMatches(img *image.RGBA, imgArr minicv.IntegralArray, tplImg *image.RGBA, tplStats minicv.StatsResult, roi [4]int, threshold float64) []detection {
	ncc := computeNCCMap(img, imgArr, tplImg, tplStats, roi)
	if ncc == nil {
		return nil
	}

	tw, th := tplImg.Rect.Dx(), tplImg.Rect.Dy()
	supHalfW := int(float64(tw) * suppressFactor)
	supHalfH := int(float64(th) * suppressFactor)

	var hits []detection
	for {
		mx, my, val := ncc.max()
		if val < threshold {
			break
		}
		hits = append(hits, detection{
			x:     ncc.offX + mx,
			y:     ncc.offY + my,
			score: val,
		})
		ncc.suppress(mx-supHalfW, my-supHalfH, supHalfW*2, supHalfH*2)
	}
	return hits
}

// computeItemROI returns the search region above a rarity bar hit.
func computeItemROI(rarityX, rarityY int) [4]int {
	extend := int(math.Round(float64(itemSize) * positionExtendFactor))
	return [4]int{
		rarityX - extend,
		rarityY - itemSize - extend,
		itemSize + 2*extend,
		itemSize + rarityHeight + 2*extend,
	}
}

// competitiveMatch runs the competitive template matching loop for all templates in an ROI.
// Implements the same algorithm as Python CompetitiveTemplateMatcher.match().
func competitiveMatch(img *image.RGBA, imgArr minicv.IntegralArray, templates []*scaledTemplate, roi [4]int, threshold float64) []detection {
	// Step 1: compute NCC map for each template
	maps := make([]*nccMap, len(templates))
	for i, tpl := range templates {
		maps[i] = computeNCCMap(img, imgArr, tpl.img, tpl.stats, roi)
	}

	// Step 2: competitive loop (strictly following Python prototype lines 295-338)
	supHalfW := int(float64(itemSize) * suppressFactor)
	supHalfH := int(float64(itemSize) * suppressFactor)

	var detections []detection
	for {
		// Find global max across all templates
		bestVal := -1.0
		bestIdx := -1
		bestMX, bestMY := 0, 0

		for i, m := range maps {
			if m == nil {
				continue
			}
			mx, my, val := m.max()
			if val > bestVal {
				bestVal = val
				bestIdx = i
				bestMX = mx
				bestMY = my
			}
		}

		if bestVal < threshold || bestIdx < 0 {
			break
		}

		// Record detection (convert map-local coords to image coords)
		detX := maps[bestIdx].offX + bestMX
		detY := maps[bestIdx].offY + bestMY
		detections = append(detections, detection{
			name:  templates[bestIdx].name,
			x:     detX,
			y:     detY,
			score: bestVal,
		})

		// Suppress in all maps
		for _, m := range maps {
			if m != nil {
				// Convert detection position to this map's local coords
				localX := detX - m.offX
				localY := detY - m.offY
				m.suppress(localX-supHalfW, localY-supHalfH, supHalfW*2, supHalfH*2)
			}
		}
	}

	return detections
}
