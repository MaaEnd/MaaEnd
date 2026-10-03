package visitfriends

import "testing"

func TestCountPatchStopsAtLimit(t *testing.T) {
	for _, test := range []struct {
		visits int64
		limit  int64
		stop   bool
	}{
		{visits: 1, limit: 1, stop: true},
		{visits: 1, limit: 3, stop: false},
		{visits: 2, limit: 3, stop: false},
		{visits: 3, limit: 3, stop: true},
	} {
		patch, stop := countPatch("VisitFriendsEnterFriendsListSuccess", test.visits, test.limit)
		if stop != test.stop {
			t.Fatalf("visits=%d limit=%d: stop=%v", test.visits, test.limit, stop)
		}
		state := patch["VisitFriendsEnterFriendsListSuccess"].(map[string]any)["attach"].(map[string]any)
		if state["visits"] != test.visits {
			t.Fatalf("visits=%d: saved state=%v", test.visits, state)
		}
		_, hasStop := patch["VisitFriendsMenuScan"]
		if hasStop != test.stop {
			t.Fatalf("visits=%d: stop patch=%v", test.visits, hasStop)
		}
	}
}
