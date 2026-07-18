package sellproduct

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestOperatorSessionExcludesSelectedOperators 验证派驻冲突后会按用途排除刚选中的干员并清理待确认状态。
func TestOperatorSessionExcludesSelectedOperators(t *testing.T) {
	tests := []struct {
		name      string
		usage     string
		prepare   func(location string, candidate operatorCandidate)
		remaining func(operatorSessionState, string) bool
	}{
		{
			name:  "售卖干员",
			usage: operatorActionUsageTarget,
			prepare: func(location string, candidate operatorCandidate) {
				operatorSessionSetTargetAssignment(location, candidate)
			},
			remaining: func(session operatorSessionState, location string) bool {
				_, ok := session.TargetAssignments[location]
				return ok
			},
		},
		{
			name:  "恢复干员",
			usage: operatorActionUsageRestore,
			prepare: func(location string, candidate operatorCandidate) {
				operatorSessionSetPlannedRestore(location, candidate, true)
			},
			remaining: func(session operatorSessionState, location string) bool {
				_, ok := session.PlannedRestoreAssignments[location]
				return ok
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetOperatorSessionForTest(t, operatorCacheModeCache)
			location := "RefugeeCamp"
			candidate := operatorCandidate{Name: "Perlica", CacheName: "佩丽卡"}
			test.prepare(location, candidate)

			excluded, ok := operatorSessionExcludeSelected(test.usage, location)
			if !ok || excluded.Name != candidate.Name {
				t.Fatalf("排除结果 = %+v，成功状态 = %v", excluded, ok)
			}
			session := operatorSessionSnapshot()
			if _, ok := session.ExcludedOperators[candidate.CacheName]; !ok {
				t.Fatalf("临时排除集合中缺少 %q", candidate.CacheName)
			}
			if test.remaining(session, location) {
				t.Fatal("派驻冲突后仍残留待确认的干员分配")
			}
		})
	}
}

// TestParseOperatorSessionExcludeSelectedParam 验证临时排除操作必须提供合法用途和据点。
func TestParseOperatorSessionExcludeSelectedParam(t *testing.T) {
	p, err := parseOperatorSessionActionParam(&maa.CustomActionArg{CustomActionParam: `{
        "operation": "exclude_selected",
        "usage": "target",
        "location": "RefugeeCamp"
    }`})
	if err != nil || p.Usage != operatorActionUsageTarget || p.Location != "RefugeeCamp" {
		t.Fatalf("解析结果 = %+v，错误 = %v", p, err)
	}
	if _, err := parseOperatorSessionActionParam(&maa.CustomActionArg{CustomActionParam: `{
        "operation": "exclude_selected",
        "usage": "unknown",
        "location": "RefugeeCamp"
    }`}); err == nil {
		t.Fatal("临时排除操作使用未知用途时应校验失败")
	}
}

func TestOperatorSessionRegistersActiveLocations(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	operatorSessionRegisterLocation("RefugeeCamp")
	operatorSessionRegisterLocation("ReconstructionCommand")

	session := operatorSessionSnapshot()
	if len(session.ActiveLocations) != 2 {
		t.Fatalf("active locations = %#v, want 2 entries", session.ActiveLocations)
	}
	if _, ok := session.ActiveLocations["RefugeeCamp"]; !ok {
		t.Fatal("RefugeeCamp should be active")
	}
	if _, ok := session.ActiveLocations["ReconstructionCommand"]; !ok {
		t.Fatal("ReconstructionCommand should be active")
	}
}

func TestOperatorSessionLocksCompletedRestoreAssignment(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	candidate := operatorCandidate{Name: "Perlica", CacheName: "佩丽卡"}
	operatorSessionSetPlannedRestore("ReconstructionCommand", candidate, true)
	if !operatorSessionCompleteRestore("ReconstructionCommand") {
		t.Fatal("planned restore should be completable")
	}

	session := operatorSessionSnapshot()
	if got := session.LockedRestoreAssignments["ReconstructionCommand"].Name; got != "Perlica" {
		t.Fatalf("locked assignment = %q, want Perlica", got)
	}
	if _, ok := session.PlannedRestoreAssignments["ReconstructionCommand"]; ok {
		t.Fatal("completed assignment should be removed from the planned set")
	}
	if _, ok := session.CompletedRestoreLocations["ReconstructionCommand"]; !ok {
		t.Fatal("completed restore should mark the location as handled")
	}
}

func TestOperatorSessionSkipsRestoreLocation(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	operatorSessionSetPlannedRestore("RefugeeCamp", operatorCandidate{Name: "Shared"}, true)
	operatorSessionSkipRestore("RefugeeCamp")

	session := operatorSessionSnapshot()
	if _, ok := session.CompletedRestoreLocations["RefugeeCamp"]; !ok {
		t.Fatal("skipped restore should mark the location as handled")
	}
	if _, ok := session.PlannedRestoreAssignments["RefugeeCamp"]; ok {
		t.Fatal("skipped restore should remove the stale planned assignment")
	}
}

func TestOperatorSessionAllowsOneRetryPerSelection(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	if !operatorSessionClaimRetry(operatorActionUsageTarget, "RefugeeCamp") {
		t.Fatal("first retry should be allowed")
	}
	if operatorSessionClaimRetry(operatorActionUsageTarget, "RefugeeCamp") {
		t.Fatal("second retry for the same selection should be rejected")
	}
	if !operatorSessionClaimRetry(operatorActionUsageRestore, "RefugeeCamp") {
		t.Fatal("target and restore retries should be tracked separately")
	}
}
