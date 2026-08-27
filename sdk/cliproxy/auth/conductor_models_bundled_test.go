package auth

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func bundledCodexAuth(key, baseURL, proxyURL, prefix, configIndex string) *Auth {
	return &Auth{
		ID:       "codex:apikey:test000000000",
		Provider: "codex",
		Prefix:   prefix,
		ProxyURL: proxyURL,
		Attributes: map[string]string{
			AttributeAPIKey:      key,
			"base_url":           baseURL,
			AttributeConfigIndex: configIndex,
			AttributeSource:      "config:codex[abc]",
			AttributeAuthKind:    AuthKindAPIKey,
		},
	}
}

func TestResolveCodexAPIKeyConfigBundledKeyUsesConfigIndex(t *testing.T) {
	cfg := &config.Config{
		CodexKey: []config.CodexKey{
			{
				APIKey:  "parent-a",
				BaseURL: "https://entry-a.example.com/v1",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
			{
				APIKey:  "parent-b",
				BaseURL: "https://entry-b.example.com/v1",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
		},
	}
	// The same bundled key exists in both entries; the auth's config_index must
	// pin the lookup to entry B instead of the first bare-key match.
	auth := bundledCodexAuth("shared-key", "https://entry-b.example.com/v1", "", "", "1")
	entry := resolveCodexAPIKeyConfig(cfg, auth)
	if entry == nil {
		t.Fatalf("resolveCodexAPIKeyConfig() = nil, want entry B")
	}
	if entry.BaseURL != "https://entry-b.example.com/v1" {
		t.Fatalf("resolved base-url = %q, want entry B base-url", entry.BaseURL)
	}
}

func TestResolveCodexAPIKeyConfigBundledKeyFallsBackToCredentialMatch(t *testing.T) {
	cfg := &config.Config{
		CodexKey: []config.CodexKey{
			{
				APIKey:  "parent-a",
				BaseURL: "https://entry-a.example.com/v1",
				Prefix:  "teamA",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
			{
				APIKey:  "parent-b",
				BaseURL: "https://entry-b.example.com/v1",
				Prefix:  "teamB",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
		},
	}
	// Without a usable config_index the prefix/proxy/base-url checks must still
	// disambiguate the two entries.
	auth := bundledCodexAuth("shared-key", "https://entry-b.example.com/v1", "", "teamB", "99")
	entry := resolveCodexAPIKeyConfig(cfg, auth)
	if entry == nil {
		t.Fatalf("resolveCodexAPIKeyConfig() = nil, want entry B")
	}
	if entry.BaseURL != "https://entry-b.example.com/v1" {
		t.Fatalf("resolved base-url = %q, want entry B base-url", entry.BaseURL)
	}
}

func TestResolveCodexAPIKeyConfigBundledKeyRejectsMismatchedPrefix(t *testing.T) {
	cfg := &config.Config{
		CodexKey: []config.CodexKey{
			{
				APIKey:  "parent-a",
				BaseURL: "https://entry-a.example.com/v1",
				Prefix:  "teamA",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
		},
	}
	auth := bundledCodexAuth("shared-key", "https://entry-a.example.com/v1", "", "teamB", "0")
	if entry := resolveCodexAPIKeyConfig(cfg, auth); entry != nil {
		t.Fatalf("resolveCodexAPIKeyConfig() = %+v, want nil for mismatched prefix", entry)
	}
}

func TestResolveCodexAPIKeyConfigTopLevelKeyStillWins(t *testing.T) {
	cfg := &config.Config{
		CodexKey: []config.CodexKey{
			{
				APIKey:  "parent-a",
				BaseURL: "https://entry-a.example.com/v1",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "shared-key"},
				},
			},
		},
	}
	auth := bundledCodexAuth("parent-a", "https://entry-a.example.com/v1", "", "", "0")
	entry := resolveCodexAPIKeyConfig(cfg, auth)
	if entry == nil {
		t.Fatalf("resolveCodexAPIKeyConfig() = nil, want entry A")
	}
	if entry.APIKey != "parent-a" {
		t.Fatalf("resolved api-key = %q, want parent-a", entry.APIKey)
	}
}
