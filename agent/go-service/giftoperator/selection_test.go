package giftoperator

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"reflect"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestGiftTierRequiresKnownCompleteLabel(t *testing.T) {
	for text, rings := range map[string]int{"高": 0, "中": 1, "低": 2, " HIGH ": 0, "Medium": 1, "Low": 2, "高い": 0, "보통": 1} {
		if value, err := parseGiftTier(text); err != nil || value != rings {
			t.Fatalf("wrong completed rings: text=%q rings=%d error=%v", text, value, err)
		}
	}
	for _, text := range []string{"", "赖", "高低", "今日赠礼可提升的信赖", "3", "mediumlow"} {
		if _, err := parseGiftTier(text); err == nil {
			t.Fatalf("unreadable tier became progress: %q", text)
		}
	}
}

func TestActualGiftStatusRejectsSelectedPreview(t *testing.T) {
	runner := newGiftStatusRunner("高", "101%")
	runner.labels = []maa.Rect{{132, 522, 68, 38}}
	status, _, err := readGiftStatus(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), session{}, false, false)
	if err != nil || status != nil || runner.readTrust {
		t.Fatalf("preview passed actual-status guard: status=%+v read_trust=%v error=%v", status, runner.readTrust, err)
	}
}

func TestDailyFullTakesPrecedenceOverUnreadableTier(t *testing.T) {
	runner := newGiftStatusRunner("赖", "101%")
	runner.limited = true
	status, _, err := readGiftStatus(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), session{}, false, false)
	if err != nil || status == nil || status.CompletedRings == nil || *status.CompletedRings != 3 || runner.readTier {
		t.Fatalf("daily full was not definitive: status=%+v read_tier=%v error=%v", status, runner.readTier, err)
	}
}

func TestUnknownTierIsNotAssumedToBeZero(t *testing.T) {
	for _, trust := range []string{"101%", "200%"} {
		runner := newGiftStatusRunner("赖", trust)
		status, _, err := readGiftStatus(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), session{}, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if trust == "101%" && status != nil {
			t.Fatal("unknown non-max tier was accepted")
		}
		if trust == "200%" && (status == nil || status.CompletedRings != nil) {
			t.Fatal("max-trust exception invented daily rings")
		}
	}
}

func TestSelectedCountSumsCategoriesAndWaitsForGrowth(t *testing.T) {
	runner := newGiftStatusRunner("中", "101%")
	runner.labels = []maa.Rect{{132, 522, 68, 38}, {236, 522, 68, 38}}
	runner.quantities = map[maa.Rect]string{{163, 522, 37, 20}: "28", {267, 522, 37, 20}: "1"}
	s := session{TargetRings: 2, Remaining: 1, Pending: "Perlica", Prepared: true, SelectedCount: 28}
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	status, _, err := recognizeGiftSelection(runner, img, s)
	if err != nil || status == nil || status.SelectedCount == nil || *status.SelectedCount != 29 || !status.Preview {
		t.Fatalf("new category reset total: status=%+v error=%v", status, err)
	}
	s.SelectedCount = 29
	if status, _, err := recognizeGiftSelection(runner, img, s); err != nil || status != nil {
		t.Fatalf("unchanged frame allowed another click: status=%+v error=%v", status, err)
	}
}

func TestSelectedQuantityFailureInvalidatesWholeFrame(t *testing.T) {
	for _, invalid := range []string{"", "0", "01", "1O", "-1", "28+", "999999999999999999999999999"} {
		runner := newGiftStatusRunner("低", "101%")
		runner.labels = []maa.Rect{{132, 522, 68, 38}, {236, 522, 68, 38}}
		runner.quantities = map[maa.Rect]string{{163, 522, 37, 20}: "28", {267, 522, 37, 20}: invalid}
		if count, hit, err := readSelectedCount(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720))); err != nil || hit || count != 0 {
			t.Fatalf("unreadable category yielded partial total: input=%q count=%d hit=%v error=%v", invalid, count, hit, err)
		}
	}
}

func TestSelectedTextROIExcludesIconAndFrame(t *testing.T) {
	if roi, ok := selectedTextROI(maa.Rect{132, 522, 68, 38}); !ok || roi != (maa.Rect{163, 522, 37, 20}) {
		t.Fatalf("wrong verified numeric ROI: %v %v", roi, ok)
	}
	for _, box := range []maa.Rect{{132, 522, 31, 38}, {132, 522, 0, 38}, {132, 522, 68, 0}} {
		if _, ok := selectedTextROI(box); ok {
			t.Fatalf("incomplete label accepted: %v", box)
		}
	}
	for _, text := range []string{"28", "29", "59", "74", "1", "123"} {
		if _, err := parseSelectedCount(text); err != nil {
			t.Fatalf("positive decimal rejected: text=%q error=%v", text, err)
		}
	}
}

func TestGiftStatusPreservesRuntimePortraitAndNameGuard(t *testing.T) {
	s := genericGiftStatusSession(t)
	for _, test := range []struct {
		name  string
		match bool
	}{{"另一人", true}, {"新干员", false}} {
		runner := newGiftStatusRunner("中", "101%")
		runner.name, runner.portraitMatch = test.name, test.match
		if status, _, err := readGiftStatus(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), s, true, true); err != nil || status != nil {
			t.Fatalf("different recipient passed shared preview verification: test=%+v status=%+v error=%v", test, status, err)
		}
	}
}

func TestGiftStatusReadsCombinedNameWithoutChildHit(t *testing.T) {
	for _, test := range []struct {
		name   string
		verify bool
	}{{"before", false}, {"after", true}} {
		t.Run(test.name, func(t *testing.T) {
			runner := newGiftStatusRunner("高", "118%")
			status, _, err := readGiftStatus(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), genericGiftStatusSession(t), test.verify, false)
			if err != nil || status == nil || !status.IdentityMatch || status.Name != "新干员" ||
				status.Trust == nil || *status.Trust != 118 || status.CompletedRings == nil || *status.CompletedRings != 0 {
				t.Fatalf("native combined name was rejected: status=%+v error=%v", status, err)
			}
		})
	}
}

func TestGiftSelectionReadsCombinedNameWithoutChildHit(t *testing.T) {
	runner := newGiftStatusRunner("中", "118%")
	runner.labels = []maa.Rect{{132, 522, 68, 38}}
	runner.quantities = map[maa.Rect]string{{163, 522, 37, 20}: "29"}
	status, _, err := recognizeGiftSelection(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), genericGiftStatusSession(t))
	if err != nil || status == nil || !status.Preview || !status.IdentityMatch || status.Name != "新干员" ||
		status.SelectedCount == nil || *status.SelectedCount != 29 || status.CompletedRings == nil || *status.CompletedRings != 1 {
		t.Fatalf("native combined name blocked preview: status=%+v error=%v", status, err)
	}
}

func TestGiftStatusRejectsInvalidCombinedName(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*maa.RecognitionDetail)
	}{
		{"parent miss", func(detail *maa.RecognitionDetail) { detail.Hit = false }},
		{"missing child", func(detail *maa.RecognitionDetail) { detail.CombinedResult[1] = nil }},
		{"missing best", func(detail *maa.RecognitionDetail) { detail.CombinedResult[1].DetailJson = `{"best":null}` }},
		{"wrong algorithm", func(detail *maa.RecognitionDetail) { detail.CombinedResult[1].Algorithm = "TemplateMatch" }},
		{"invalid payload", func(detail *maa.RecognitionDetail) { detail.CombinedResult[1].DetailJson = `invalid` }},
		{"empty name", func(detail *maa.RecognitionDetail) { detail.CombinedResult[1].DetailJson = ocrDetail(" ").DetailJson }},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := newGiftStatusRunner("高", "118%")
			runner.nameResult = combinedGiftNameDetail("新干员")
			test.mutate(runner.nameResult)
			status, _, err := readGiftStatus(runner, image.NewRGBA(image.Rect(0, 0, 1280, 720)), genericGiftStatusSession(t), false, false)
			if err != nil || status != nil || runner.readTrust || runner.readTier {
				t.Fatalf("invalid combined name passed: status=%+v read_trust=%v read_tier=%v error=%v", status, runner.readTrust, runner.readTier, err)
			}
		})
	}
}

func TestBestOCRTextRejectsStandaloneMiss(t *testing.T) {
	detail := ocrDetail("118%")
	detail.Hit = false
	if text, ok := bestOCRText(detail); ok || text != "" {
		t.Fatalf("standalone OCR miss was accepted: text=%q hit=%v", text, ok)
	}
}

func genericGiftStatusSession(t *testing.T) session {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 56, 48))); err != nil {
		t.Fatal(err)
	}
	return session{Generic: true, Pending: "portrait-1", Prepared: true, Remaining: 1, TargetRings: 3,
		Portraits: []portraitEntry{{Operator: "portrait-1", Name: "新干员", Template: "runtime.png", Image: base64.StdEncoding.EncodeToString(buffer.Bytes())}}}
}

// CombinedResult children in maa-framework-go beta.19 do not populate Hit.
// A successful parent and the child's OCR best result establish the name hit.
func combinedGiftNameDetail(name string) *maa.RecognitionDetail {
	child := ocrDetail(name)
	child.Hit = false
	return &maa.RecognitionDetail{Hit: true, Algorithm: "And", CombinedResult: []*maa.RecognitionDetail{{Algorithm: "And"}, child}}
}

type giftStatusRunner struct {
	tier, trust, name                           string
	limited, portraitMatch, readTrust, readTier bool
	labels                                      []maa.Rect
	quantities                                  map[maa.Rect]string
	nameResult                                  *maa.RecognitionDetail
}

func newGiftStatusRunner(tier, trust string) *giftStatusRunner {
	return &giftStatusRunner{tier: tier, trust: trust, name: "新干员", portraitMatch: true}
}

func (r *giftStatusRunner) RunRecognition(entry string, _ image.Image, overrides ...any) (*maa.RecognitionDetail, error) {
	switch entry {
	case "GiftOperatorGiftUI":
		return &maa.RecognitionDetail{Hit: true}, nil
	case "GiftOperatorGiftSelectedLabelColor":
		filtered := make([]maa.ColorMatchResult, 0, len(r.labels))
		for _, box := range r.labels {
			filtered = append(filtered, maa.ColorMatchResult{Box: box, Count: 1})
		}
		raw, _ := json.Marshal(map[string]any{"filtered": filtered})
		return &maa.RecognitionDetail{Hit: len(filtered) > 0, Algorithm: "ColorMatch", DetailJson: string(raw)}, nil
	case "GiftOperatorGiftTrust":
		r.readTrust = true
		return ocrDetail(r.trust), nil
	case "GiftOperatorDailyLimitText":
		return &maa.RecognitionDetail{Hit: r.limited}, nil
	case "GiftOperatorGiftTier":
		r.readTier = true
		return ocrDetail(r.tier), nil
	case "GiftOperatorGiftName":
		if r.nameResult != nil {
			return r.nameResult, nil
		}
		return combinedGiftNameDetail(r.name), nil
	case "GiftOperatorGiftSelectedText":
		if len(overrides) != 1 {
			return nil, fmt.Errorf("expected explicit numeric ROI")
		}
		raw, _ := json.Marshal(overrides[0])
		var patch map[string]struct {
			ROI      maa.Rect `json:"roi"`
			Offset   maa.Rect `json:"roi_offset"`
			OnlyRec  bool     `json:"only_rec"`
			Expected []string `json:"expected"`
		}
		if err := json.Unmarshal(raw, &patch); err != nil {
			return nil, err
		}
		param := patch[entry]
		if param.Offset != (maa.Rect{}) || !param.OnlyRec || !reflect.DeepEqual(param.Expected, []string{`^[1-9][0-9]*$`}) {
			return nil, fmt.Errorf("numeric OCR lacks explicit constraints")
		}
		text, ok := r.quantities[param.ROI]
		if !ok {
			return nil, fmt.Errorf("OCR read outside numeric tag: %v", param.ROI)
		}
		return ocrDetail(text), nil
	default:
		return nil, fmt.Errorf("unexpected recognition %q", entry)
	}
}

func (r *giftStatusRunner) RunRecognitionDirect(maa.RecognitionType, maa.RecognitionParam, image.Image) (*maa.RecognitionDetail, error) {
	return &maa.RecognitionDetail{Hit: r.portraitMatch}, nil
}

func (r *giftStatusRunner) OverrideImage(string, image.Image) error { return nil }

func ocrDetail(text string) *maa.RecognitionDetail {
	raw, _ := json.Marshal(map[string]any{"best": maa.OCRResult{Text: text}})
	return &maa.RecognitionDetail{Hit: text != "", Algorithm: "OCR", DetailJson: string(raw)}
}
