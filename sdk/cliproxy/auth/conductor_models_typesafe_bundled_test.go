package auth

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func typesafeBundledAuth(key, baseURL, configIndex string) *Auth {
	return &Auth{
		ID:       "typesafe:apikey:test000000000",
		Provider: "typesafe",
		Attributes: map[string]string{
			AttributeAPIKey:      key,
			"base_url":           baseURL,
			AttributeConfigIndex: configIndex,
			AttributeSource:      "config:typesafe[abc]",
			AttributeAuthKind:    AuthKindAPIKey,
		},
	}
}

// The same bundled key lives in two entries; the auth's config_index must pin
// the lookup to entry B instead of the first bare-key match.
func TestResolveTypeSafeAPIKeyConfigBundledKeyUsesConfigIndex(t *testing.T) {
	cfg := &config.Config{
		TypeSafeKey: []config.TypeSafeKey{
			{
				APIKey:  "parent-a",
				BaseURL: "https://a.typesafe.example.com",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
			{
				APIKey:  "parent-b",
				BaseURL: "https://b.typesafe.example.com",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
		},
	}
	auth := typesafeBundledAuth("shared-key", "https://b.typesafe.example.com", "1")
	entry := resolveTypeSafeAPIKeyConfig(cfg, auth)
	if entry == nil {
		t.Fatalf("resolveTypeSafeAPIKeyConfig() = nil, want entry B")
	}
	if entry.BaseURL != "https://b.typesafe.example.com" {
		t.Fatalf("resolved base-url = %q, want entry B base-url", entry.BaseURL)
	}
}

// A bundled-only entry (no top-level api-key) resolves via the bundled
// fallback when the flat lookup misses.
func TestResolveTypeSafeAPIKeyConfigBundledOnlyEntry(t *testing.T) {
	cfg := &config.Config{
		TypeSafeKey: []config.TypeSafeKey{
			{
				BaseURL: "https://api.typesafe.ai",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "bundled-only-key"},
				},
			},
		},
	}
	auth := typesafeBundledAuth("bundled-only-key", "https://api.typesafe.ai", "0")
	entry := resolveTypeSafeAPIKeyConfig(cfg, auth)
	if entry == nil {
		t.Fatalf("resolveTypeSafeAPIKeyConfig() = nil, want entry")
	}
	if entry.BaseURL != "https://api.typesafe.ai" {
		t.Fatalf("resolved base-url = %q, want %q", entry.BaseURL, "https://api.typesafe.ai")
	}
}

// An unknown key must not resolve through the bundled fallback.
func TestResolveTypeSafeAPIKeyConfigUnknownKeyMisses(t *testing.T) {
	cfg := &config.Config{
		TypeSafeKey: []config.TypeSafeKey{
			{
				APIKey:  "parent-a",
				BaseURL: "https://api.typesafe.ai",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "bundled-1"},
				},
			},
		},
	}
	auth := typesafeBundledAuth("unknown-key", "https://api.typesafe.ai", "0")
	if entry := resolveTypeSafeAPIKeyConfig(cfg, auth); entry != nil {
		t.Fatalf("resolveTypeSafeAPIKeyConfig() = %v, want nil for unknown key", entry)
	}
}
