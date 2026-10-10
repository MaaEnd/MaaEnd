package giftoperator

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// The resource is V2 while GetNodeJSON may return its normalized flat form.
// Both must retain the same operator ID, avatar and multilingual identity.
func TestReadCandidateSupportsResourceAndNormalizedNode(t *testing.T) {
	path := filepath.Join("..", "..", "..", "assets", "resource", "pipeline", "GiftOperator", "Operator", "Operator.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var resources nodeJSONStore
	if err := json.Unmarshal(jsonclean.Clean(raw), &resources); err != nil {
		t.Fatal(err)
	}
	if len(resources) == 0 {
		t.Fatal("operator resource is empty")
	}
	for node, raw := range resources {
		t.Run(node, func(t *testing.T) {
			original, template, err := readCandidate(resources, node)
			if err != nil {
				t.Fatal(err)
			}
			if original.Operator != strings.TrimPrefix(node, "GiftOperatorSelect_") || len(original.Names) == 0 {
				t.Fatalf("operator metadata does not match resource node: %+v", original)
			}
			var v2 struct {
				Recognition struct {
					Param map[string]json.RawMessage `json:"param"`
				} `json:"recognition"`
				Action struct {
					Param map[string]json.RawMessage `json:"param"`
				} `json:"action"`
			}
			if err := json.Unmarshal(raw, &v2); err != nil {
				t.Fatal(err)
			}
			flat := map[string]json.RawMessage{
				"recognition": json.RawMessage(`"TemplateMatch"`),
				"action":      json.RawMessage(`"Custom"`),
			}
			for key, value := range v2.Recognition.Param {
				flat[key] = value
			}
			for key, value := range v2.Action.Param {
				flat[key] = value
			}
			normalized, err := json.Marshal(flat)
			if err != nil {
				t.Fatal(err)
			}
			actual, actualTemplate, err := readCandidate(nodeJSONStore{node: normalized}, node)
			if err != nil || actualTemplate != template || !reflect.DeepEqual(actual, original) {
				t.Fatalf("flat node changed identity: candidate=%+v template=%q error=%v", actual, actualTemplate, err)
			}
		})
	}
}

func TestParseTrustRequiresValidPercentage(t *testing.T) {
	for text, want := range map[string]int{"0%": 0, "101%": 101, "199%": 199, "200%": 200, " 101 ％ ": 101} {
		value, err := parseTrust(text)
		if err != nil || value != want {
			t.Fatalf("parseTrust(%q) = %d, %v; want %d", text, value, err, want)
		}
	}
	for _, text := range []string{"", "101", "201%", "-1%", "1O1%", "101.5%"} {
		if _, err := parseTrust(text); err == nil {
			t.Fatalf("accepted invalid trust %q", text)
		}
	}
}

func TestGiftCandidateUsesCardOrderAndTrust(t *testing.T) {
	// 用户日志：噗切娜头像比同排秋栗头像低 2px，不能因此先选择秋栗。
	purrchena := candidate{Operator: "Purrchena", Box: maa.Rect{551, 187, 80, 72}}
	akekuri := candidate{Operator: "Akekuri", Box: maa.Rect{642, 185, 81, 73}}
	secondRow := candidate{Operator: "Perlica", Box: maa.Rect{551, 315, 80, 72}}
	for _, tc := range []struct {
		name       string
		trust      map[string]int
		unknown    string
		want       *candidate
		wantChecks []string
	}{
		{"reported failure", map[string]int{"Purrchena": 101, "Akekuri": 200, "Perlica": 200}, "", &purrchena, []string{"Purrchena"}},
		{"same row jitter", map[string]int{"Purrchena": 101, "Akekuri": 101, "Perlica": 101}, "", &purrchena, []string{"Purrchena"}},
		{"maximum trust on left", map[string]int{"Purrchena": 200, "Akekuri": 101, "Perlica": 101}, "", &akekuri, []string{"Purrchena", "Akekuri"}},
		{"next row", map[string]int{"Purrchena": 200, "Akekuri": 200, "Perlica": 199}, "", &secondRow, []string{"Purrchena", "Akekuri", "Perlica"}},
		{"all maximum", map[string]int{"Purrchena": 200, "Akekuri": 200, "Perlica": 200}, "", nil, []string{"Purrchena", "Akekuri", "Perlica"}},
		{"unreadable trust", map[string]int{"Purrchena": 0, "Akekuri": 101, "Perlica": 200}, "Purrchena", &akekuri, []string{"Purrchena", "Akekuri"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, input := range [][]candidate{{akekuri, secondRow, purrchena}, {purrchena, secondRow, akekuri}} {
				var checked []string
				got, trust, err := selectGiftCandidate(input, func(info candidate) (int, bool, error) {
					checked = append(checked, info.Operator)
					return tc.trust[info.Operator], info.Operator != tc.unknown, nil
				})
				if err != nil || !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(checked, tc.wantChecks) {
					t.Fatalf("selected=%+v checked=%v error=%v; want=%+v checked=%v", got, checked, err, tc.want, tc.wantChecks)
				}
				if got != nil && trust != tc.trust[got.Operator] {
					t.Fatalf("selected trust=%d; want=%d", trust, tc.trust[got.Operator])
				}
			}
		})
	}
}

func TestGiftCandidateDoesNotSelectAfterRecognitionError(t *testing.T) {
	got, _, err := selectGiftCandidate([]candidate{{Operator: "Purrchena", Box: maa.Rect{551, 187, 80, 72}}},
		func(candidate) (int, bool, error) { return 0, false, fmt.Errorf("agent disconnected") })
	if err == nil || got != nil {
		t.Fatalf("recognition failure selected a recipient: candidate=%+v error=%v", got, err)
	}
}

func TestCandidateTrustReadsOnlyMatchingCard(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for _, tc := range []struct {
		name string
		box  maa.Rect
		roi  maa.Rect
	}{
		{"reported Win32 card", maa.Rect{551, 187, 80, 72}, maa.Rect{581, 271, 40, 18}},
		{"scaled ADB card", maa.Rect{551, 187, 100, 90}, maa.Rect{588, 292, 50, 22}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &trustRecognitionRunner{detail: &maa.RecognitionDetail{Hit: true, Algorithm: "OCR", DetailJson: `{"best":{"text":"101%"}}`}}
			trust, hit, err := recognizeCandidateTrust(runner, img, tc.box)
			if err != nil || !hit || trust != 101 || runner.entry != "GiftOperatorTrustValue" {
				t.Fatalf("trust=%d hit=%v error=%v entry=%q", trust, hit, err, runner.entry)
			}
			var override map[string]struct {
				ROI       maa.Rect `json:"roi"`
				ROIOffset maa.Rect `json:"roi_offset"`
			}
			if err := json.Unmarshal(runner.override, &override); err != nil {
				t.Fatal(err)
			}
			params := override["GiftOperatorTrustValue"]
			if params.ROI != tc.roi || params.ROIOffset != (maa.Rect{}) {
				t.Fatalf("trust ROI=%v offset=%v; want ROI=%v and no previous relative offset", params.ROI, params.ROIOffset, tc.roi)
			}
		})
	}
}

func TestCandidateTrustRejectsUnconfirmedValues(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for _, text := range []string{"", "1O1%", "201%", "-1%", "+101%", "101.5%", "101%%"} {
		raw, err := json.Marshal(map[string]any{"best": map[string]string{"text": text}})
		if err != nil {
			t.Fatal(err)
		}
		runner := &trustRecognitionRunner{detail: &maa.RecognitionDetail{Hit: true, Algorithm: "OCR", DetailJson: string(raw)}}
		if trust, hit, err := recognizeCandidateTrust(runner, img, maa.Rect{551, 187, 80, 72}); err != nil || hit {
			t.Fatalf("unconfirmed trust %q accepted: trust=%d hit=%v error=%v", text, trust, hit, err)
		}
	}
	for _, text := range []string{"0", "101", "199%", "200", "200％"} {
		raw, err := json.Marshal(map[string]any{"best": map[string]string{"text": text}})
		if err != nil {
			t.Fatal(err)
		}
		runner := &trustRecognitionRunner{detail: &maa.RecognitionDetail{Hit: true, Algorithm: "OCR", DetailJson: string(raw)}}
		if _, hit, err := recognizeCandidateTrust(runner, img, maa.Rect{551, 187, 80, 72}); err != nil || !hit {
			t.Fatalf("valid trust %q rejected: hit=%v error=%v", text, hit, err)
		}
	}
	for _, box := range []maa.Rect{{551, 650, 80, 72}, {1240, 187, 80, 72}, {551, 187, 0, 72}} {
		runner := &trustRecognitionRunner{}
		if _, hit, err := recognizeCandidateTrust(runner, img, box); err != nil || hit || runner.entry != "" {
			t.Fatalf("incomplete card reached OCR: box=%v hit=%v error=%v entry=%q", box, hit, err, runner.entry)
		}
	}
}

type trustRecognitionRunner struct {
	detail   *maa.RecognitionDetail
	entry    string
	override json.RawMessage
}

func (r *trustRecognitionRunner) RunRecognition(entry string, _ image.Image, override ...any) (*maa.RecognitionDetail, error) {
	r.entry = entry
	if len(override) != 1 {
		return nil, fmt.Errorf("expected one override")
	}
	raw, err := json.Marshal(override[0])
	if err != nil {
		return nil, err
	}
	r.override = raw
	return r.detail, nil
}

type nodeJSONStore map[string]json.RawMessage

func (s nodeJSONStore) GetNodeJSON(name string) (string, error) {
	raw, ok := s[name]
	if !ok {
		return "", fmt.Errorf("unknown node %q", name)
	}
	return string(raw), nil
}

func (s nodeJSONStore) OverridePipeline(any) error {
	return fmt.Errorf("readCandidate must not modify the pipeline")
}
