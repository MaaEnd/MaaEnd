package giftoperator

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/minicv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestGenericCandidateFindsUnlistedPortraitAndSkipsUnconfirmedTrust(t *testing.T) {
	runner := &portraitTestRunner{cards: []portraitTestCard{
		{maa.Rect{552, 186, 80, 72}, "200%", 1},
		{maa.Rect{642, 186, 80, 72}, "1O1%", 2},
		{maa.Rect{732, 186, 80, 72}, "101%", 3},
	}}
	got, trust, err := recognizeGenericCandidate(runner, runner.image(), session{}, portraitGeometry{80, 72, -19, -85}, false)
	if err != nil || got == nil || got.Operator != "portrait-1" || got.Box != runner.cards[2].box || trust != 101 || len(got.Names) != 0 {
		t.Fatalf("unknown recipient = %+v, trust=%d, error=%v", got, trust, err)
	}
	if len(runner.images) != 16 {
		t.Fatalf("saved %d templates; only selected portrait and 15 gift scales are required", len(runner.images))
	}
	if got.Template != "GiftOperator/RuntimePortrait/portrait-1.png" {
		t.Fatalf("unexpected runtime template %q", got.Template)
	}
	if runner.images[got.Template].Bounds().Size() != image.Pt(56, 48) {
		t.Fatal("portrait includes the card border, selection icon, or trust text")
	}
	for i, name := range giftPortraitTemplates(got.Template) {
		want := image.Pt(int(56*(0.75+0.02*float64(i))), int(48*(0.75+0.02*float64(i))))
		if img := runner.images[name]; img == nil || img.Bounds().Size() != want {
			t.Fatalf("gift portrait %d has incorrect scale; want %v", i, want)
		}
	}
}

func TestGenericCandidateReidentifiesPendingAfterCardsMove(t *testing.T) {
	runner := &portraitTestRunner{cards: []portraitTestCard{{maa.Rect{552, 186, 80, 72}, "101%", 7}}}
	geometry := portraitGeometry{80, 72, -19, -85}
	first, _, err := recognizeGenericCandidate(runner, runner.image(), session{}, geometry, false)
	if err != nil || first == nil {
		t.Fatalf("first selection failed: %v", err)
	}
	s := session{Pending: first.Operator, Portraits: []portraitEntry{{Operator: first.Operator, Template: first.Template, Image: first.Portrait}}}
	// 下一个 Custom 回调没有上次的图像覆盖，必须按任务像素恢复。
	runner.images = nil
	// 同一格换成另一张未知头像；真正待送礼的头像已移到下一排。
	runner.cards = []portraitTestCard{{maa.Rect{552, 186, 80, 72}, "101%", 8}, {maa.Rect{642, 314, 80, 72}, "101%", 7}}
	got, _, err := recognizeGenericCandidate(runner, runner.image(), s, geometry, true)
	if err != nil || got == nil || got.Operator != first.Operator || got.Box != runner.cards[1].box {
		t.Fatalf("pending recipient used a stale position: %+v, error=%v", got, err)
	}
	if len(runner.images) != 16 {
		t.Fatal("click recognition replaced the captured identity")
	}
	runner.cards = runner.cards[:1]
	if got, _, err := recognizeGenericCandidate(runner, runner.image(), s, geometry, true); err != nil || got != nil {
		t.Fatalf("missing pending portrait selected someone else: %+v, error=%v", got, err)
	}
}

func TestGenericCandidateExcludesCompletedAndRejectedPortraitsAcrossPositions(t *testing.T) {
	geometry := portraitGeometry{80, 72, -19, -85}
	runner := &portraitTestRunner{cards: []portraitTestCard{{maa.Rect{552, 186, 80, 72}, "101%", 11}}}
	first, _, err := recognizeGenericCandidate(runner, runner.image(), session{}, geometry, false)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	s := session{Completed: []string{first.Operator}, Portraits: []portraitEntry{{Operator: first.Operator, Template: first.Template, Image: first.Portrait}}}
	runner.cards = []portraitTestCard{{maa.Rect{642, 314, 80, 72}, "110%", 11}, {maa.Rect{552, 186, 80, 72}, "101%", 12}}
	second, _, err := recognizeGenericCandidate(runner, runner.image(), s, geometry, false)
	if err != nil || second == nil || second.Operator != "portrait-2" {
		t.Fatalf("second unknown identity failed: %+v, error=%v", second, err)
	}
	s.Portraits = append(s.Portraits, portraitEntry{Operator: second.Operator, Template: second.Template, Image: second.Portrait})
	s.Excluded = []string{second.Operator}
	// 排除对象移到首格，已成功对象移到次格，两者仍须按头像跳过。
	runner.cards = []portraitTestCard{{maa.Rect{552, 186, 80, 72}, "101%", 12}, {maa.Rect{642, 186, 80, 72}, "110%", 11}, {maa.Rect{732, 186, 80, 72}, "199%", 13}}
	third, trust, err := recognizeGenericCandidate(runner, runner.image(), s, geometry, false)
	if err != nil || third == nil || third.Operator != "portrait-3" || third.Box != runner.cards[2].box || trust != 199 {
		t.Fatalf("processed portrait was selected again: %+v, trust=%d, error=%v", third, trust, err)
	}
}

func TestGenericCandidateRequiresRuntimeIdentityBeforeReturningClick(t *testing.T) {
	runner := &portraitTestRunner{cards: []portraitTestCard{{maa.Rect{552, 186, 80, 72}, "101%", 1}}}
	geometry := portraitGeometry{80, 72, -19, -85}
	if got, _, err := recognizeGenericCandidate(runner, runner.image(), session{Pending: "portrait-1"}, geometry, true); err == nil || got != nil {
		t.Fatal("click without a captured portrait was accepted")
	}
	runner.saveError = fmt.Errorf("runtime image storage failed")
	if got, _, err := recognizeGenericCandidate(runner, runner.image(), session{}, geometry, false); err == nil || got != nil {
		t.Fatal("recipient was returned after runtime image capture failed")
	}
}

func TestPortraitGeometryUsesWin32AndADBResources(t *testing.T) {
	for _, tc := range []struct {
		offset int
		want   portraitGeometry
		crop   maa.Rect
	}{
		{-85, portraitGeometry{80, 72, -19, -85}, maa.Rect{564, 204, 56, 48}},
		{-106, portraitGeometry{100, 90, -24, -106}, maa.Rect{567, 208, 70, 60}},
	} {
		for _, raw := range []string{
			fmt.Sprintf(`{"recognition":"ColorMatch","roi_offset":[40,%d,5,5]}`, tc.offset),
			fmt.Sprintf(`{"recognition":{"type":"ColorMatch","param":{"roi_offset":[40,%d,5,5]}}}`, tc.offset),
		} {
			store := nodeJSONStore{"GiftOperatorNotSelect": json.RawMessage(raw)}
			got, err := readPortraitGeometry(store)
			if err != nil || got != tc.want || portraitCropBox(maa.Rect{552, 186, got.width, got.height}) != tc.crop {
				t.Fatalf("geometry=%+v, crop=%v, error=%v", got, portraitCropBox(maa.Rect{552, 186, got.width, got.height}), err)
			}
		}
	}
	if _, err := readPortraitGeometry(nodeJSONStore{"GiftOperatorNotSelect": json.RawMessage(`{"roi_offset":[0,0,0,0]}`)}); err == nil {
		t.Fatal("unknown layout used assumed card coordinates")
	}
}

func TestRestorePortraitInstallsImagesInEachCallback(t *testing.T) {
	first := &portraitTestRunner{cards: []portraitTestCard{{maa.Rect{552, 186, 80, 72}, "101%", 33}}}
	selected, _, err := recognizeGenericCandidate(first, first.image(), session{}, portraitGeometry{80, 72, -19, -85}, false)
	if err != nil || selected == nil || selected.Portrait == "" {
		t.Fatalf("portrait pixel capture failed: %v", err)
	}
	entry := portraitEntry{Operator: selected.Operator, Template: selected.Template, Image: selected.Portrait}
	fresh := &portraitTestRunner{}
	if err := restorePortrait(fresh, entry); err != nil || fresh.installCount != 16 {
		t.Fatalf("fresh callback restored %d images, error=%v", fresh.installCount, err)
	}
	if !reflect.DeepEqual(fresh.images[selected.Template], first.images[selected.Template]) {
		t.Fatal("PNG restoration changed the captured identity pixels")
	}
	fresh.images = nil
	if err := restorePortrait(fresh, entry); err != nil || fresh.installCount != 32 {
		t.Fatalf("another callback reused expired image overrides: count=%d, error=%v", fresh.installCount, err)
	}
}

func TestRestorePortraitRejectsMissingOrInvalidPNG(t *testing.T) {
	var large bytes.Buffer
	if err := png.Encode(&large, image.NewRGBA(image.Rect(0, 0, 101, 48))); err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{"", "%%invalid", base64.StdEncoding.EncodeToString([]byte("not a PNG")),
		base64.StdEncoding.EncodeToString(large.Bytes()), strings.Repeat("a", 128*1024+1)} {
		runner := &portraitTestRunner{}
		err := restorePortrait(runner, portraitEntry{Operator: "portrait-1", Template: "GiftOperator/RuntimePortrait/portrait-1.png", Image: encoded})
		if err == nil || runner.installCount != 0 {
			t.Fatalf("invalid PNG installed runtime images: count=%d, error=%v", runner.installCount, err)
		}
	}
}

type portraitTestCard struct {
	box   maa.Rect
	trust string
	seed  int64
}

type portraitTestRunner struct {
	cards        []portraitTestCard
	images       map[string]image.Image
	saveError    error
	installCount int
}

func (r *portraitTestRunner) image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for _, card := range r.cards {
		crop := portraitCropBox(card.box)
		rng := rand.New(rand.NewSource(card.seed))
		for y := crop[1]; y < crop[1]+crop[3]; y++ {
			for x := crop[0]; x < crop[0]+crop[2]; x++ {
				img.SetRGBA(x, y, color.RGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255})
			}
		}
	}
	return img
}

func (r *portraitTestRunner) RunRecognition(entry string, _ image.Image, overrides ...any) (*maa.RecognitionDetail, error) {
	if entry == "GiftOperatorTrustIcon" {
		var filtered []maa.TemplateMatchResult
		for i := len(r.cards) - 1; i >= 0; i-- {
			box := r.cards[i].box
			filtered = append(filtered, maa.TemplateMatchResult{Box: maa.Rect{box[0] + 19, box[1] + 85, 15, 16}, Score: 1})
		}
		raw, _ := json.Marshal(map[string]any{"filtered": filtered})
		return &maa.RecognitionDetail{Hit: len(filtered) > 0, Algorithm: "TemplateMatch", DetailJson: string(raw)}, nil
	}
	if entry != "GiftOperatorTrustValue" || len(overrides) != 1 {
		return nil, fmt.Errorf("unexpected recognition %q", entry)
	}
	raw, _ := json.Marshal(overrides[0])
	var params map[string]struct {
		ROI maa.Rect `json:"roi"`
	}
	_ = json.Unmarshal(raw, &params)
	for _, card := range r.cards {
		box := card.box
		want := maa.Rect{box[0] + box[2]*30/80, box[1] + box[3]*84/72, box[2] * 40 / 80, box[3] * 18 / 72}
		if params[entry].ROI == want {
			raw, _ := json.Marshal(map[string]any{"best": map[string]string{"text": card.trust}})
			return &maa.RecognitionDetail{Hit: true, Algorithm: "OCR", DetailJson: string(raw)}, nil
		}
	}
	return nil, fmt.Errorf("OCR read a different card: %s", raw)
}

func (r *portraitTestRunner) RunRecognitionDirect(kind maa.RecognitionType, params maa.RecognitionParam, img image.Image) (*maa.RecognitionDetail, error) {
	p, ok := params.(*maa.TemplateMatchParam)
	if !ok || kind != maa.RecognitionTypeTemplateMatch || len(p.Template) != 1 || !reflect.DeepEqual(p.Threshold, []float64{portraitThreshold}) {
		return nil, fmt.Errorf("unexpected portrait matching parameters")
	}
	if r.images[p.Template[0]] == nil {
		return nil, fmt.Errorf("runtime portrait is missing")
	}
	raw, _ := json.Marshal(p)
	var region struct {
		ROI maa.Rect `json:"roi"`
	}
	_ = json.Unmarshal(raw, &region)
	b := region.ROI
	crop := minicv.ImageCropRect(minicv.ImageConvertRGBA(img), image.Rect(b[0], b[1], b[0]+b[2], b[1]+b[3]))
	template := minicv.ImageConvertRGBA(r.images[p.Template[0]])
	x, y, score := minicv.MatchTemplate(crop, minicv.GetIntegralArray(crop), template, minicv.GetImageStats(template))
	return &maa.RecognitionDetail{Hit: score >= p.Threshold[0], Algorithm: "TemplateMatch", Box: maa.Rect{b[0] + int(x), b[1] + int(y), template.Bounds().Dx(), template.Bounds().Dy()}}, nil
}

func (r *portraitTestRunner) OverrideImage(name string, img image.Image) error {
	if r.saveError != nil {
		return r.saveError
	}
	if r.images == nil {
		r.images = make(map[string]image.Image)
	}
	r.images[name] = minicv.ImageCopy(minicv.ImageConvertRGBA(img))
	r.installCount++
	return nil
}
