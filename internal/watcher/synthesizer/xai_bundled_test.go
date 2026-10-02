package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// The xAI primary key and its bundled keys must produce independent
// credentials that keep their per-entry proxy and weight.
func TestConfigSynthesizer_XAIBundledKeys(t *testing.T) {
	parentWeight, overrideWeight := 2, 7
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{Config: &config.Config{XAIKey: []config.XAIKey{{APIKey: "fixture-parent", BaseURL: "https://api.x.ai/v1", ProxyURL: "http://parent.proxy.invalid:8080", Weight: &parentWeight, Models: []config.XAIModel{{Name: "grok-fixture", Alias: "client-fixture"}}, APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "fixture-one", ProxyURL: "direct", Weight: &overrideWeight}, {APIKey: "fixture-two"}}}}}, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(auths) != 3 {
		t.Fatalf("expected parent and two bundled credentials, got %d", len(auths))
	}
	wantKeys := []string{"fixture-parent", "fixture-one", "fixture-two"}
	seen := map[string]bool{}
	for i, a := range auths {
		if a.Provider != "xai" || a.Attributes["api_key"] != wantKeys[i] || a.Attributes["config_index"] != "0" {
			t.Fatalf("incorrect credential %d: %#v", i, a)
		}
		if a.Attributes["models_hash"] == "" || seen[a.ID] {
			t.Fatal("missing model mapping hash or non-distinct credential ID")
		}
		seen[a.ID] = true
	}
	if auths[1].ProxyURL != "direct" || auths[1].Attributes["weight"] != "7" {
		t.Fatal("bundled proxy/weight override lost")
	}
	if auths[2].ProxyURL != "http://parent.proxy.invalid:8080" || auths[2].Attributes["weight"] != "2" {
		t.Fatal("bundled parent proxy/weight inheritance lost")
	}
}

func TestConfigSynthesizer_XAIBundledOnlyKeys(t *testing.T) {
	ctx := &SynthesisContext{Config: &config.Config{XAIKey: []config.XAIKey{{BaseURL: "https://api.x.ai/v1", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "fixture-only"}}}}}, Now: time.Now(), IDGenerator: NewStableIDGenerator()}
	auths, err := NewConfigSynthesizer().Synthesize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(auths) != 1 || auths[0].Attributes["api_key"] != "fixture-only" {
		t.Fatal("xAI bundled-only entry did not synthesize its credential")
	}
}
