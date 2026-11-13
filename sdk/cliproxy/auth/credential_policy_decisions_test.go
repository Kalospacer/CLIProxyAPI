package auth

import "testing"

func TestDecisionCredentialPolicyUsesNativeAPICredentials(t *testing.T) {
	for _, test := range []struct {
		name     string
		policy   string
		auth     *Auth
		expected bool
	}{
		{
			name:   "OpenAI API key with versioned base URL",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth: &Auth{Provider: "codex", Attributes: map[string]string{
				AttributeAPIKey: "test-key", "base_url": "https://api.openai.com/v1",
			}},
			expected: true,
		},
		{
			name:   "OpenAI API key without base URL uses official upstream",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth:   &Auth{Provider: "codex", Attributes: map[string]string{AttributeAPIKey: "test-key"}},
			expected: true,
		},
		{
			name:   "third-party Codex key is excluded",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth: &Auth{Provider: "codex", Attributes: map[string]string{
				AttributeAPIKey: "other-key", "base_url": "https://proxy.muice.moe/snow-bread-soft/api.mistral.ai/v1",
			}},
		},
		{
			name:   "OpenAI OAuth is excluded",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth:   &Auth{Provider: "codex", Metadata: map[string]any{"access_token": "token"}},
		},
		{
			name:   "AI Studio key with versioned base URL",
			policy: CredentialPolicyGeminiDecisionsV1,
			auth: &Auth{Provider: "gemini", Attributes: map[string]string{
				AttributeAPIKey: "test-key", "base_url": "https://generativelanguage.googleapis.com/v1beta",
			}},
			expected: true,
		},
		{
			name:   "AI Studio key without base URL uses official upstream",
			policy: CredentialPolicyGeminiDecisionsV1,
			auth:   &Auth{Provider: "gemini", Attributes: map[string]string{AttributeAPIKey: "test-key"}},
			expected: true,
		},
		{
			name:   "Gemini OpenAI-compatible endpoint is excluded",
			policy: CredentialPolicyGeminiDecisionsV1,
			auth: &Auth{Provider: "gemini", Attributes: map[string]string{
				AttributeAPIKey: "test-key", "base_url": "https://generativelanguage.googleapis.com/v1beta/openai",
			}},
		},
		{
			name:   "Gemini OAuth is excluded",
			policy: CredentialPolicyGeminiDecisionsV1,
			auth:   &Auth{Provider: "gemini", Metadata: map[string]any{"access_token": "token"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if actual := credentialPolicyAllows(test.policy, test.auth); actual != test.expected {
				t.Fatalf("credentialPolicyAllows() = %v, want %v", actual, test.expected)
			}
			if normalizeCredentialPolicy(test.policy) != test.policy {
				t.Fatalf("normalizeCredentialPolicy(%q) rejected registered policy", test.policy)
			}
		})
	}
}
