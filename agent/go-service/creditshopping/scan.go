package creditshopping

import (
	"image"
	"sort"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Pipeline 节点名：CreditIcon 即货架上的物品图标，一次模板命中对应一个物品槽位。
const pipelineNodeShelfItemIcon = "CreditIcon"

// 自每个 CreditIcon 命中框推算名称/折扣 OCR 区域用的 roi_offset 链（与 Item.json 中
// CreditIcon→NotSoldOut→CanAfford 及名称/折扣相对关系一致；不做颜色状态识别）。
var (
	notSoldOutOffset = [4]int{-94, -135, 140, 152}
	canAffordOffset  = [4]int{94, 133, -94, -147}
	nameSearchOffset = [4]int{-110, -185, 230, 80}
	discountOffset   = [4]int{44, -170, 4, 4}
)

type SlotRecord struct {
	Index       int    `json:"index"`
	NameRaw     string `json:"name_raw"`
	DiscountRaw string `json:"discount_raw"`
}

// ScanShelfSlots 流程：
//  1) 对整图跑 CreditIcon 模板匹配，每个命中框对应一个物品槽位；
//  2) 对每个命中框用固定偏移链得到名称 OCR 区与（基于名称字框的）折扣 OCR 区；
//  3) 结果由调用方写入快照文件（见 storage / action_record）。
func ScanShelfSlots(ctx *maa.Context, img image.Image) []SlotRecord {
	icons := collectCreditIconBoxes(ctx, img)
	if len(icons) == 0 {
		return nil
	}
	out := make([]SlotRecord, 0, len(icons))
	for i, box := range icons {
		out = append(out, slotFromCreditIcon(ctx, img, i, box))
	}
	return out
}

func collectCreditIconBoxes(ctx *maa.Context, img image.Image) []maa.Rect {
	detail, err := ctx.RunRecognition(pipelineNodeShelfItemIcon, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		log.Info().Str("component", component).Msg("shelf scan: no CreditIcon")
		return nil
	}
	boxes := templateMatchBoxes(detail)
	sort.Slice(boxes, func(i, j int) bool {
		if boxes[i][1] != boxes[j][1] {
			return boxes[i][1] < boxes[j][1]
		}
		return boxes[i][0] < boxes[j][0]
	})
	return boxes
}

func templateMatchBoxes(detail *maa.RecognitionDetail) []maa.Rect {
	if detail == nil || detail.Results == nil {
		return nil
	}
	var out []maa.Rect
	src := detail.Results.Filtered
	if len(src) == 0 {
		src = detail.Results.All
	}
	for _, r := range src {
		if r == nil {
			continue
		}
		tm, ok := r.AsTemplateMatch()
		if ok {
			out = append(out, tm.Box)
		}
	}
	if len(out) == 0 && detail.Results.Best != nil {
		if tm, ok := detail.Results.Best.AsTemplateMatch(); ok {
			out = append(out, tm.Box)
		}
	}
	return out
}

func slotFromCreditIcon(ctx *maa.Context, img image.Image, index int, icon maa.Rect) SlotRecord {
	rec := SlotRecord{Index: index}
	nameROI := nameSearchROIFromCreditIcon(icon)
	if !rectValid(nameROI) {
		return rec
	}
	rec.NameRaw = nameOCR(ctx, img, nameROI)
	nameBox := textBoxForName(ctx, img, nameROI, rec.NameRaw)
	if !rectValid(nameBox) {
		nameBox = nameROI
	}
	discountROI := applyROIEdge(nameBox, discountOffset)
	if rectValid(discountROI) {
		rec.DiscountRaw = discountOCR(ctx, img, discountROI)
	}
	return rec
}

func nameSearchROIFromCreditIcon(icon maa.Rect) maa.Rect {
	ns := applyROIEdge(icon, notSoldOutOffset)
	if !rectValid(ns) {
		return maa.Rect{}
	}
	ar := applyROIEdge(ns, canAffordOffset)
	if !rectValid(ar) {
		return maa.Rect{}
	}
	return applyROIEdge(ar, nameSearchOffset)
}

func nameOCR(ctx *maa.Context, img image.Image, roi maa.Rect) string {
	param := maa.OCRParam{
		ROI:      targetRect(roi),
		Expected: []string{".*"},
		OnlyRec:  true,
		OrderBy:  maa.OCROrderByVertical,
	}
	d, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param, img)
	if err != nil || d == nil || !d.Hit {
		return ""
	}
	return strings.TrimSpace(bestOCRText(d))
}

func textBoxForName(ctx *maa.Context, img image.Image, roi maa.Rect, want string) maa.Rect {
	if strings.TrimSpace(want) == "" {
		return maa.Rect{}
	}
	param := maa.OCRParam{
		ROI:      targetRect(roi),
		Expected: []string{".*"},
		OnlyRec:  true,
		OrderBy:  maa.OCROrderByVertical,
	}
	d, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param, img)
	if err != nil || d == nil || !d.Hit {
		return maa.Rect{}
	}
	want = strings.TrimSpace(want)
	if d.Results != nil {
		for _, r := range d.Results.Filtered {
			if r == nil {
				continue
			}
			if o, ok := r.AsOCR(); ok && strings.TrimSpace(o.Text) == want {
				return o.Box
			}
		}
		if d.Results.Best != nil {
			if o, ok := d.Results.Best.AsOCR(); ok && strings.TrimSpace(o.Text) == want {
				return o.Box
			}
		}
	}
	return maa.Rect{}
}

func discountOCR(ctx *maa.Context, img image.Image, roi maa.Rect) string {
	param := maa.OCRParam{
		ROI:      targetRect(roi),
		Expected: []string{"99", "95", "75", "50", "25"},
		OnlyRec:  true,
		OrderBy:  maa.OCROrderByExpected,
	}
	d, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param, img)
	if err == nil && d != nil && d.Hit {
		return strings.TrimSpace(bestOCRText(d))
	}
	param2 := maa.OCRParam{
		ROI:      targetRect(roi),
		Expected: []string{".*"},
		OnlyRec:  true,
	}
	d2, err2 := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, &param2, img)
	if err2 != nil || d2 == nil || !d2.Hit {
		return ""
	}
	t := strings.TrimSpace(bestOCRText(d2))
	if _, err := strconv.Atoi(t); err == nil {
		return t
	}
	return t
}
