package matchapi

import (
	"path/filepath"
	"regexp"
	"testing"
)

func TestSkillPoolTermIDs(t *testing.T) {
	dir, err := filepath.Abs("../../../../assets/data/EssenceFilter")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngineFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	seen := map[string]bool{}
	for pool := 1; pool <= 3; pool++ {
		skills := poolBySlot(engine.SkillPools(), pool)
		if len(skills) == 0 {
			t.Errorf("empty skill pool %d", pool)
		}
		for _, skill := range skills {
			term := skill.TermID
			if !pattern.MatchString(term) || seen[term] {
				t.Errorf("pool %d, skill %d (%s): invalid or duplicate term_id %q", pool, skill.ID, skill.Chinese, term)
			}
			seen[term] = true
			gameID, found := engine.TermID(pool, skill.ID)
			gotPool, gotID, ok := engine.ResolveTermID(term)
			if !found || !ok || gameID != term || gotPool != pool || gotID != skill.ID {
				t.Errorf("could not round-trip pool %d, skill %d, term %q", pool, skill.ID, term)
			}
		}
	}
}
