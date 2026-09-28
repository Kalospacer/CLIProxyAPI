package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func intPointer(value int) *int {
	return &value
}

func TestConfigSynthesizer_TypeSafeKeys_BundledAPIKeyEntries(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			TypeSafeKey: []config.TypeSafeKey{
				{
					APIKey:  "parent-key",
					BaseURL: "https://api.typesafe.ai",
					Models: []config.TypeSafeModel{
						{Name: "jev-latest", Alias: "jev"},
					},
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "bundled-1"},
						{APIKey: "bundled-2", Weight: intPointer(3)},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 3 {
		t.Fatalf("expected 3 auths (parent + 2 bundled), got %d", len(auths))
	}
	wantKeys := []string{"parent-key", "bundled-1", "bundled-2"}
	for i, want := range wantKeys {
		if auths[i].Provider != "typesafe" {
			t.Fatalf("auth[%d].Provider = %q, want typesafe", i, auths[i].Provider)
		}
		if got := auths[i].Attributes["api_key"]; got != want {
			t.Fatalf("auth[%d].api_key = %q, want %q", i, got, want)
		}
		if got := auths[i].Attributes["config_index"]; got != "0" {
			t.Fatalf("auth[%d].config_index = %q, want 0", i, got)
		}
		if _, ok := auths[i].Attributes["models_hash"]; !ok {
			t.Fatalf("auth[%d] missing models_hash", i)
		}
	}
	// Bundled weight overrides the entry-level weight.
	if got := auths[2].Attributes["weight"]; got != "3" {
		t.Fatalf("auth[2].weight = %q, want 3", got)
	}
	// Distinct auth IDs per key so cooldown state is tracked per credential.
	if auths[0].ID == auths[1].ID || auths[1].ID == auths[2].ID {
		t.Fatalf("expected distinct auth IDs, got %q / %q / %q", auths[0].ID, auths[1].ID, auths[2].ID)
	}
}

func TestConfigSynthesizer_TypeSafeKeys_BundledOnlyEntry(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			TypeSafeKey: []config.TypeSafeKey{
				{
					BaseURL: "https://api.typesafe.ai",
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "bundled-only"},
					},
				},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}

	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth for bundled-only entry, got %d", len(auths))
	}
	if got := auths[0].Attributes["api_key"]; got != "bundled-only" {
		t.Fatalf("api_key = %q, want bundled-only", got)
	}
}
