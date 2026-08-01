package seizedeliveryjobs

import (
	"errors"
	"testing"
)

const seizeDeliveryJobsDepartureBaseParam = `{
    "map_name_regex": "^(map02_lv002|map02_lv005)$",
    "zipline_policy": "Lazy",
    "delete_shared_ziplines": false,
    "is_retry": false
}`

type fakeSeizeDeliveryJobsNodeJSONSource struct {
	raw   string
	err   error
	calls int
}

func (f *fakeSeizeDeliveryJobsNodeJSONSource) GetNodeJSON(_ string) (string, error) {
	f.calls++
	return f.raw, f.err
}

func TestResolveDepartureParamFromAttach(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		param      string
		attach     string
		wantPolicy string
		wantDelete bool
	}{
		{
			name:       "defaults without attach",
			param:      seizeDeliveryJobsDepartureBaseParam,
			attach:     `{}`,
			wantPolicy: "Lazy",
			wantDelete: false,
		},
		{
			name:       "prefer zipline",
			param:      seizeDeliveryJobsDepartureBaseParam,
			attach:     `{"zipline_policy":"Active"}`,
			wantPolicy: "Active",
			wantDelete: false,
		},
		{
			name:       "prefer zipline and delete shared ziplines",
			param:      seizeDeliveryJobsDepartureBaseParam,
			attach:     `{"zipline_policy":"Active","delete_shared_ziplines":true}`,
			wantPolicy: "Active",
			wantDelete: true,
		},
		{
			name:       "explicit false overrides true",
			param:      `{"map_name_regex":"map","zipline_policy":"Active","delete_shared_ziplines":true}`,
			attach:     `{"delete_shared_ziplines":false}`,
			wantPolicy: "Active",
			wantDelete: false,
		},
		{
			name:       "lazy policy disables deletion",
			param:      seizeDeliveryJobsDepartureBaseParam,
			attach:     `{"delete_shared_ziplines":true}`,
			wantPolicy: "Lazy",
			wantDelete: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := &fakeSeizeDeliveryJobsNodeJSONSource{
				raw: `{"attach":` + testCase.attach + `}`,
			}
			param, err := (&SeizeDeliveryJobsDepartureAction{}).resolveParam(
				source,
				"SeizeDeliveryJobsRunDeparture",
				testCase.param,
			)
			if err != nil {
				t.Fatalf("resolveParam() error = %v", err)
			}
			if param.ZiplinePolicy != testCase.wantPolicy {
				t.Errorf("ZiplinePolicy = %q, want %q", param.ZiplinePolicy, testCase.wantPolicy)
			}
			if param.DeleteSharedZiplines != testCase.wantDelete {
				t.Errorf("DeleteSharedZiplines = %t, want %t", param.DeleteSharedZiplines, testCase.wantDelete)
			}
			if source.calls != 1 {
				t.Errorf("GetNodeJSON() calls = %d, want 1", source.calls)
			}
		})
	}
}

func TestResolveDepartureParamRetrySkipsAttach(t *testing.T) {
	t.Parallel()

	source := &fakeSeizeDeliveryJobsNodeJSONSource{
		err: errors.New("attach must not be read during retry"),
	}
	param, err := (&SeizeDeliveryJobsDepartureAction{}).resolveParam(
		source,
		"SeizeDeliveryJobsSubmitFallback",
		`{"map_name_regex":"","zipline_policy":"Never","is_retry":true}`,
	)
	if err != nil {
		t.Fatalf("resolveParam() error = %v", err)
	}
	if param.ZiplinePolicy != "Never" {
		t.Errorf("ZiplinePolicy = %q, want Never", param.ZiplinePolicy)
	}
	if param.DeleteSharedZiplines {
		t.Error("DeleteSharedZiplines = true, want false")
	}
	if source.calls != 0 {
		t.Errorf("GetNodeJSON() calls = %d, want 0", source.calls)
	}
}

func TestResolveDepartureParamAttachErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source *fakeSeizeDeliveryJobsNodeJSONSource
	}{
		{
			name: "get node failure",
			source: &fakeSeizeDeliveryJobsNodeJSONSource{
				err: errors.New("get node failed"),
			},
		},
		{
			name: "null node JSON",
			source: &fakeSeizeDeliveryJobsNodeJSONSource{
				raw: `null`,
			},
		},
		{
			name: "invalid boolean type",
			source: &fakeSeizeDeliveryJobsNodeJSONSource{
				raw: `{"attach":{"delete_shared_ziplines":"yes"}}`,
			},
		},
		{
			name: "null attach",
			source: &fakeSeizeDeliveryJobsNodeJSONSource{
				raw: `{"attach":null}`,
			},
		},
		{
			name: "null zipline policy",
			source: &fakeSeizeDeliveryJobsNodeJSONSource{
				raw: `{"attach":{"zipline_policy":null}}`,
			},
		},
		{
			name: "null deletion switch",
			source: &fakeSeizeDeliveryJobsNodeJSONSource{
				raw: `{"attach":{"delete_shared_ziplines":null}}`,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := (&SeizeDeliveryJobsDepartureAction{}).resolveParam(
				testCase.source,
				"SeizeDeliveryJobsRunDeparture",
				seizeDeliveryJobsDepartureBaseParam,
			); err == nil {
				t.Fatal("resolveParam() error = nil, want error")
			}
		})
	}
}

func TestResolveDepartureParamRejectsInvalidAttachedPolicy(t *testing.T) {
	t.Parallel()

	source := &fakeSeizeDeliveryJobsNodeJSONSource{
		raw: `{"attach":{"zipline_policy":"Sometimes"}}`,
	}
	if _, err := (&SeizeDeliveryJobsDepartureAction{}).resolveParam(
		source,
		"SeizeDeliveryJobsRunDeparture",
		seizeDeliveryJobsDepartureBaseParam,
	); err == nil {
		t.Fatal("resolveParam() error = nil, want error")
	}
}
