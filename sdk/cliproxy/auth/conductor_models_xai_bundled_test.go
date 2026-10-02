package auth

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// Entry proxy overrides for TypeSafe, Codex, and xAI must match the proxy
// used when the credentials were synthesized.
func TestBundledCodexStyleConfigMatchesProxyOverride(t *testing.T) {
	for _, provider := range []string{"codex", "typesafe", "xai"} {
		t.Run(provider, func(t *testing.T) {
			entries := []config.CodexKey{{APIKey: "fixture-parent", BaseURL: "https://upstream.example.invalid", ProxyURL: "http://parent.proxy.invalid:8080", Prefix: "fixture", Models: []config.CodexModel{{Name: "upstream", Alias: "client"}}, APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "fixture-bundled", ProxyURL: "direct"}}}}
			cfg := &config.Config{}
			switch provider {
			case "codex":
				cfg.CodexKey = entries
			case "typesafe":
				cfg.TypeSafeKey = entries
			case "xai":
				cfg.XAIKey = entries
			}
			a := &Auth{ID: provider + ":apikey:fixture", Provider: provider, Prefix: "fixture", ProxyURL: "direct", Attributes: map[string]string{AttributeAPIKey: "fixture-bundled", AttributeConfigIndex: "0", AttributeSource: "config:" + provider + "[fixture]", AttributeAuthKind: AuthKindAPIKey, "base_url": "https://upstream.example.invalid"}}
			resolve := func() *config.CodexKey {
				switch provider {
				case "codex":
					return resolveCodexAPIKeyConfig(cfg, a)
				case "typesafe":
					return resolveTypeSafeAPIKeyConfig(cfg, a)
				default:
					return resolveXAIAPIKeyConfig(cfg, a)
				}
			}
			entry := resolve()
			if entry == nil || len(entry.Models) != 1 || entry.Models[0].Alias != "client" {
				t.Fatal("bundled credential lost its parent model mapping with a proxy override")
			}
			a.ProxyURL = "http://wrong.proxy.invalid:8080"
			if resolve() != nil {
				t.Fatal("bundled credential with the wrong proxy matched a parent config")
			}
		})
	}
}

// A key reused across different xAI entries is located by its persisted
// config index and base URL.
func TestResolveXAIAPIKeyConfigBundledIndexAndUnknownKey(t *testing.T) {
	cfg := &config.Config{XAIKey: []config.XAIKey{
		{BaseURL: "https://one.example.invalid", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "shared"}}},
		{BaseURL: "https://two.example.invalid", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "shared"}}},
	}}
	a := &Auth{Provider: "xai", Attributes: map[string]string{AttributeAPIKey: "shared", AttributeConfigIndex: "1", AttributeSource: "config:xai[fixture]", AttributeAuthKind: AuthKindAPIKey, "base_url": "https://two.example.invalid"}}
	found := resolveXAIAPIKeyConfig(cfg, a)
	if found != &cfg.XAIKey[1] {
		t.Fatal("xAI bundled key resolved to the wrong entry")
	}
	a.Attributes[AttributeAPIKey] = "unknown"
	if resolveXAIAPIKeyConfig(cfg, a) != nil {
		t.Fatal("unknown bundled key matched an xAI entry")
	}
}
