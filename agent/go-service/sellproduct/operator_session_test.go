package sellproduct

import "testing"

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
