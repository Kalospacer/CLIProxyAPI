package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestAntigravityLiteralsPreserveExecutableContext(t *testing.T) {
	matcher := BuildAntigravitySensitiveWordMatcherWithPatterns(nil, map[string]string{"Codex": "agent", "Read /tmp/codex file.": "Read the supplied file."}, nil)
	for _, preserved := range []string{
		"/tmp/codex/file", "`/tmp/Codex space/file`", `"C:\Program Files\Codex\file"`,
		"$CODEX_HOME/skills", "${CODEX_HOME}/skills", "~/tools/codex/file", "(/tmp/codex/file)",
	} {
		input := "Codex uses " + preserved + " with Codex."
		want := "agent uses " + preserved + " with agent."
		if got := matcher.ObfuscateText(input); got != want {
			t.Errorf("executable context changed: %q, want %q", got, want)
		}
	}
	if got := matcher.ObfuscateText("Read /tmp/codex file."); got != "Read the supplied file." {
		t.Fatalf("explicit sentence replacement was blocked: %q", got)
	}
	explicit := BuildAntigravitySensitiveWordMatcherWithPatterns(nil, nil, []config.SensitivePatternRule{{Pattern: "Codex uses /tmp/codex", Replacement: "Use the configured file"}})
	if got := explicit.ObfuscateText("Codex uses /tmp/codex"); got != "Use the configured file" {
		t.Fatalf("explicit regex was blocked: %q", got)
	}
	other := BuildSensitiveWordMatcherWithReplacements(nil, map[string]string{"Codex": "agent"})
	if got := other.ObfuscateText("/tmp/codex"); got != "/tmp/agent" {
		t.Fatalf("non-Antigravity behavior changed: %q", got)
	}
}
