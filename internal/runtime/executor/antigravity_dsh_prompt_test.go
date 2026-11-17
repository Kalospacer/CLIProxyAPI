package executor

import (
	"os"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestAntigravityDSHSystemPromptLeavesNoBrand(t *testing.T) {
	rulesRaw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(rulesRaw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	raw, errPrompt := os.ReadFile("testdata/antigravity_dsh_system.txt")
	if errPrompt != nil {
		t.Fatal(errPrompt)
	}
	out := matcher.ObfuscateText(string(raw))
	if out == string(raw) {
		t.Fatal("DSH system prompt was not rewritten at all")
	}
	for _, marker := range []string{"DeepSeek", "deepseek", "Harness", "DSH", "dsh"} {
		if strings.Contains(out, marker) {
			t.Errorf("residual harness marker %q", marker)
		}
	}
}
