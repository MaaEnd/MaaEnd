package pullcount

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestIntroLettersOnlyCountTowardNextPool(t *testing.T) {
	previousSession := currentSession
	t.Cleanup(func() { currentSession = previousSession })

	for _, tc := range []struct {
		name        string
		values      resourceValues
		vouchers    int
		boxes       []maa.Rect
		wantCurrent int
		wantNext    int
	}{
		{
			name:        "issue 6782 with duplicate recognition",
			values:      resourceValues{ConvertedOriginiumOroberyl: 3750, Oroberyl: 12485},
			vouchers:    1,
			boxes:       []maa.Rect{{980, 58, 260, 36}, {980, 58, 260, 36}},
			wantCurrent: 29,
			wantNext:    49,
		},
		{
			name:        "only an intro letter",
			boxes:       []maa.Rect{{980, 58, 260, 36}},
			wantCurrent: 0,
			wantNext:    20,
		},
		{
			name:        "distinct intro letter hits",
			boxes:       []maa.Rect{{980, 58, 260, 36}, {980, 60, 260, 36}},
			wantCurrent: 0,
			wantNext:    30,
		},
		{
			name:        "ordinary vouchers without an intro letter",
			values:      resourceValues{ConvertedOriginiumOroberyl: 3750, Oroberyl: 12485},
			vouchers:    1,
			wantCurrent: 29,
			wantNext:    39,
		},
		{
			name:        "originium below the reserve",
			values:      resourceValues{ConvertedOriginiumOroberyl: 75},
			wantCurrent: 0,
			wantNext:    10,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := newRunSession()
			session.Values = tc.values
			session.Vouchers.CarryToNextPulls = tc.vouchers
			currentSession = session
			for _, box := range tc.boxes {
				if !handleRecordVoucher(nil, &maa.CustomActionArg{Box: box}, stageRecordIntroLetter) {
					t.Fatal("intro letter recording failed")
				}
			}
			result := calculatePullCount(session.Values, session.Vouchers)
			if result.CurrentPoolTotal != tc.wantCurrent || result.NextPoolTotal != tc.wantNext {
				t.Errorf("got current/next %d/%d, want %d/%d", result.CurrentPoolTotal, result.NextPoolTotal, tc.wantCurrent, tc.wantNext)
			}
		})
	}
}

func TestRecordVoucherKeepsCategoriesSeparate(t *testing.T) {
	session := newRunSession()
	for _, hit := range []struct {
		stage string
		pulls int
	}{
		{stageRecordVoucher, 3},
		{stageRecordIntroLetter, 10},
	} {
		if !recordVoucher(session, hit.stage, "same position", hit.pulls) {
			t.Fatalf("first hit for %s was ignored", hit.stage)
		}
		if recordVoucher(session, hit.stage, "same position", hit.pulls) {
			t.Fatalf("duplicate hit for %s was counted", hit.stage)
		}
	}
	if session.Vouchers.CarryToNextPulls != 3 || session.Vouchers.IntroLetterPulls != 10 || len(session.VoucherHits) != 2 {
		t.Fatalf("unexpected voucher summary: %+v, unique hits: %d", session.Vouchers, len(session.VoucherHits))
	}
}

func TestOriginiumReserveIsExcludedFromPulls(t *testing.T) {
	values := resourceValues{ConvertedOriginiumOroberyl: 2925, Oroberyl: 1335}
	result := calculatePullCount(values, voucherSummary{CarryToNextPulls: 1})
	if result.ReservedOriginium != 29 || result.ReservedOriginiumOroberyl != 2175 || result.UsableOriginiumOroberyl != 750 {
		t.Fatalf("unexpected reserve or convertible balance: %+v", result)
	}
	if result.ResourcePulls != 4 || result.CurrentPoolTotal != 5 || result.NextPoolTotal != 15 {
		t.Fatalf("got resource/current/next %d/%d/%d, want 4/5/15", result.ResourcePulls, result.CurrentPoolTotal, result.NextPoolTotal)
	}

	i18n.Init()
	if i18n.Lang() == i18n.LangZhCN {
		output := formatResultFocus(values, result)
		if !strings.Contains(output, "等值嵌晶玉 2175，不计入抽数") || !strings.Contains(output, "剩余可兑换嵌晶玉 750") {
			t.Fatalf("result must distinguish reserved and convertible Oroberyl: %s", output)
		}
	}
}

func TestFormatResultFocus(t *testing.T) {
	i18n.Init()
	dossierNames := map[string]string{
		"zh_cn": "寻访情报书",
		"zh_tw": "尋訪情報書",
		"en_us": "Headhunting Dossier",
		"ja_jp": "スカウト情報ファイル",
		"ko_kr": "헤드헌팅 정보서",
	}
	values := resourceValues{ConvertedOriginiumOroberyl: 404, Oroberyl: 202}
	result := calculationResult{
		ResourcePulls:             101,
		OroberylPulls:             303,
		ReservedOriginium:         505,
		ReservedOriginiumOroberyl: 606,
		UsableOriginiumOroberyl:   707,
		UsableOriginiumPulls:      808,
		CarryToNextPulls:          909,
		IntroLetterPulls:          1010,
		NextPoolShopPulls:         1111,
		NextPoolSigninPulls:       1212,
		CurrentPoolTotal:          1313,
		NextPoolTotal:             1414,
	}
	output := formatResultFocus(values, result)
	if output == "pullcount.result" || strings.Contains(output, "%!") {
		t.Fatalf("invalid localized result: %s", output)
	}
	lines := strings.Split(output, "\n")
	if len(lines) != 9 || !strings.Contains(lines[4], dossierNames[i18n.Lang()]) || !strings.Contains(lines[4], "1010") {
		t.Fatalf("missing separate dossier line for %s: %s", i18n.Lang(), output)
	}
	wantNumbers := []string{"101", "202", "303", "404", "505", "606", "707", "808", "909", "1010", "1111", "1212", "1313", "1414"}
	if numbers := regexp.MustCompile(`[0-9]+`).FindAllString(output, -1); !reflect.DeepEqual(numbers, wantNumbers) {
		t.Fatalf("%s result fields are out of order: got %v, want %v", i18n.Lang(), numbers, wantNumbers)
	}
	t.Logf("verified localized result for %s", i18n.Lang())
}
