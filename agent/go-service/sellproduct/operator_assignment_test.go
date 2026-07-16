package sellproduct

import "testing"

func TestBuildRestoreAssignmentPlanUniqueOperators(t *testing.T) {
	groups := []operatorCandidateGroup{
		{
			Location: "A",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "AOnly", Priority: 1},
			},
		},
		{
			Location: "B",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "BOnly", Priority: 1},
			},
		},
	}
	owned := operatorNameSet([]string{"Shared", "AOnly", "BOnly"})

	plan := buildRestoreAssignmentPlan(groups, owned)
	if plan.Assigned != 2 {
		t.Fatalf("assigned = %d, want 2", plan.Assigned)
	}
	a := plan.Assignments["A"].Name
	b := plan.Assignments["B"].Name
	if a == "" || b == "" {
		t.Fatalf("missing assignment: %#v", plan.Assignments)
	}
	if a == b {
		t.Fatalf("same operator assigned to two locations: A=%s B=%s", a, b)
	}
	if a != "Shared" && b != "Shared" {
		t.Fatalf("shared best operator should be used by one location, got A=%s B=%s", a, b)
	}
}

func TestBuildRestoreAssignmentPlanMaximizesAssignedLocations(t *testing.T) {
	groups := []operatorCandidateGroup{
		{
			Location: "A",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "AOnly", Priority: 9},
			},
		},
		{
			Location: "B",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
			},
		},
	}
	owned := operatorNameSet([]string{"Shared", "AOnly"})

	plan := buildRestoreAssignmentPlan(groups, owned)
	if plan.Assigned != 2 {
		t.Fatalf("assigned = %d, want 2", plan.Assigned)
	}
	if got := plan.Assignments["B"].Name; got != "Shared" {
		t.Fatalf("B should receive the only operator it can use, got %q", got)
	}
	if got := plan.Assignments["A"].Name; got != "AOnly" {
		t.Fatalf("A should fall back to AOnly, got %q", got)
	}
}

func TestCandidatesForCurrentSelectionUsesGlobalRestorePlan(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "B",
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "A",
				Candidates: []operatorCandidate{
					{Name: "Shared", Priority: 0},
					{Name: "AOnly", Priority: 9},
				},
			},
			{
				Location: "B",
				Candidates: []operatorCandidate{
					{Name: "Shared", Priority: 0},
				},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"Shared", "AOnly"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("candidates = %#v, want Shared only", candidates)
	}
}

func TestCandidatesForCurrentSelectionRejectsIncompleteRestorePlan(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "A",
		Candidates: []operatorCandidate{
			{Name: "LocalOnly", Priority: 0},
		},
	}

	if got := candidatesForCurrentSelection(p, operatorNameSet([]string{"LocalOnly"})); got != nil {
		t.Fatalf("incomplete restore plan should not fall back to local candidates, got %#v", got)
	}
}

func TestCandidatesForCurrentSelectionReturnsOnlyGlobalBestTarget(t *testing.T) {
	p := &operatorSelectionParam{
		Usage: operatorActionUsageTarget,
		Candidates: []operatorCandidate{
			{Name: "Best", CacheName: "最优", Priority: 0},
			{Name: "Fallback", CacheName: "备选", Priority: 1},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"最优", "备选"}))
	if len(candidates) != 1 || candidates[0].Name != "Best" {
		t.Fatalf("candidates = %#v, want Best only", candidates)
	}
}

func TestCandidatesForCurrentSelectionIgnoresInactiveRestoreLocations(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "Active",
		ActiveLocations: map[string]struct{}{
			"Active": {},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Active",
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
			{
				Location:   "Inactive",
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"共享"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("candidates = %#v, active location should receive Shared", candidates)
	}
}

func TestCandidatesForCurrentSelectionKeepsLockedRestoreAssignments(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "Pending",
		ActiveLocations: map[string]struct{}{
			"Done":    {},
			"Pending": {},
		},
		LockedRestoreAssignments: map[string]operatorCandidate{
			"Done": {Name: "Shared", CacheName: "共享", Priority: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "Done",
				Candidates: []operatorCandidate{
					{Name: "Shared", CacheName: "共享", Priority: 0},
				},
			},
			{
				Location: "Pending",
				Candidates: []operatorCandidate{
					{Name: "Shared", CacheName: "共享", Priority: 0},
					{Name: "PendingOnly", CacheName: "待处理专属", Priority: 1},
				},
			},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"共享", "待处理专属"}))
	if len(candidates) != 1 || candidates[0].Name != "PendingOnly" {
		t.Fatalf("candidates = %#v, want PendingOnly", candidates)
	}
}

func TestCandidatesForCurrentSelectionIgnoresSkippedRestoreLocations(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "Pending",
		ActiveLocations: map[string]struct{}{
			"Skipped": {},
			"Pending": {},
		},
		CompletedRestoreLocations: map[string]struct{}{
			"Skipped": {},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Skipped",
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
			{
				Location:   "Pending",
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"共享"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("candidates = %#v, pending location should receive Shared", candidates)
	}
}
