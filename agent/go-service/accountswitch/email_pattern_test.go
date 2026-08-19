package accountswitch

import (
	"regexp"
	"strings"
	"testing"
)

func TestMaskEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		email string
		want  string
	}{
		{name: "normal", email: "abcdef@gmail.com", want: "ab*****ef@gmail.com"},
		{name: "numeric suffix", email: "ar1234500@gmail.com", want: "ar*****00@gmail.com"},
		{name: "one character", email: "m@example.com", want: "m*****@example.com"},
		{name: "two characters", email: "m1@example.com", want: "m*****1@example.com"},
		{name: "three characters", email: "m00@example.com", want: "m*****0@example.com"},
		{name: "four characters", email: "m123@example.com", want: "m1*****3@example.com"},
		{name: "five characters", email: "m1234@example.com", want: "m1*****34@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := maskEmail(tt.email)
			if err != nil {
				t.Fatalf("maskEmail() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("maskEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaskEmailRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, email := range []string{
		"",
		"missing-at.example.com",
		"@example.com",
		"local@",
		"a@b@example.com",
		"a b@example.com",
		"ab@example. com",
		"ab\t@example.com",
		"\u7528\u6237@example.com",
	} {
		email := email
		t.Run(email, func(t *testing.T) {
			t.Parallel()
			if _, err := maskEmail(email); err == nil {
				t.Fatalf("maskEmail(%q) error = nil, want error", email)
			}
		})
	}
}

func TestEmailTargetPatternEscapesRegexMetaCharacters(t *testing.T) {
	t.Parallel()

	email := `.*hidden?@example+?()[]\.com`
	masked, err := maskEmail(email)
	if err != nil {
		t.Fatalf("maskEmail() error = %v", err)
	}
	override, err := buildEmailPatternOverride(email)
	if err != nil {
		t.Fatalf("buildEmailPatternOverride() error = %v", err)
	}

	pattern := overridePattern(t, override, selectAccountNode)
	re := regexp.MustCompile(pattern)
	if !re.MatchString(masked) {
		t.Fatalf("pattern %q does not match literal masked email %q", pattern, masked)
	}
	if !re.MatchString(strings.ToUpper(masked)) {
		t.Fatalf("pattern %q is not case-insensitive for %q", pattern, masked)
	}
	if re.MatchString("zz*****zz@exampleZcom") {
		t.Fatalf("pattern %q treated email punctuation as regex syntax", pattern)
	}
}

func TestBuildEmailPatternOverrideTargetsOnlyAccountOCRNodes(t *testing.T) {
	t.Parallel()

	override, err := buildEmailPatternOverride("abcdef@gmail.com")
	if err != nil {
		t.Fatalf("buildEmailPatternOverride() error = %v", err)
	}
	if len(override) != 4 {
		t.Fatalf("override node count = %d, want 4", len(override))
	}

	for _, node := range []string{
		clickDropdownNode,
		selectAccountNode,
		checkAccountNode,
		verifySelectedNode,
	} {
		if _, ok := override[node]; !ok {
			t.Errorf("override missing node %q", node)
		}
	}
	if got := overridePattern(t, override, clickDropdownNode); got != maskedEmailPattern {
		t.Errorf("click dropdown pattern = %q, want %q", got, maskedEmailPattern)
	}
	if got := overridePattern(t, override, checkAccountNode); got != maskedEmailPattern {
		t.Errorf("check account pattern = %q, want %q", got, maskedEmailPattern)
	}
	selectPattern := overridePattern(t, override, selectAccountNode)
	if got := overridePattern(t, override, verifySelectedNode); got != selectPattern {
		t.Errorf("verify selected pattern = %q, want %q", got, selectPattern)
	}
}

func TestMaskedEmailPatternCoversShortUsernames(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(maskedEmailPattern)
	for _, masked := range []string{
		"m*****@example.com",
		"m*****1@example.com",
		"m*****0@example.com",
		"m1*****3@example.com",
		"m1*****34@example.com",
	} {
		if !re.MatchString(masked) {
			t.Errorf("generic masked email pattern does not match %q", masked)
		}
	}
}

func overridePattern(t *testing.T, override map[string]any, node string) string {
	t.Helper()

	nodeOverride, ok := override[node].(map[string]any)
	if !ok {
		t.Fatalf("override[%q] has type %T, want map[string]any", node, override[node])
	}
	recognition, ok := nodeOverride["recognition"].(map[string]any)
	if !ok {
		t.Fatalf("override[%q].recognition has type %T, want map[string]any", node, nodeOverride["recognition"])
	}
	param, ok := recognition["param"].(map[string]any)
	if !ok {
		t.Fatalf("override[%q].recognition.param has type %T, want map[string]any", node, recognition["param"])
	}
	expected, ok := param["expected"].([]string)
	if !ok || len(expected) != 1 {
		t.Fatalf("override[%q] expected has type %T and value %v, want one string", node, param["expected"], param["expected"])
	}
	return expected[0]
}
