package auth

import "testing"

func TestDecisionCredentialPolicyRequiresDedicatedProvider(t *testing.T) {
	for _, test := range []struct {
		name     string
		policy   string
		auth     *Auth
		expected bool
	}{
		{
			name:     "OpenAI decision key in its own group",
			policy:   CredentialPolicyOpenAIDecisionsV1,
			auth:     &Auth{Provider: "decision-openai", Attributes: map[string]string{AttributeAPIKey: "key"}},
			expected: true,
		},
		{
			name:   "ordinary Codex chat key is excluded",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth: &Auth{Provider: "codex", Attributes: map[string]string{
				AttributeAPIKey: "key", "base_url": "https://api.openai.com/v1",
			}},
		},
		{
			name:   "Codex OAuth is excluded",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth:   &Auth{Provider: "codex", Metadata: map[string]any{"access_token": "token"}},
		},
		{
			name:     "Gemini decision key in its own group",
			policy:   CredentialPolicyGeminiDecisionsV1,
			auth:     &Auth{Provider: "decision-gemini", Attributes: map[string]string{AttributeAPIKey: "key"}},
			expected: true,
		},
		{
			name:   "ordinary Gemini chat key is excluded",
			policy: CredentialPolicyGeminiDecisionsV1,
			auth: &Auth{Provider: "gemini", Attributes: map[string]string{
				AttributeAPIKey: "key", "base_url": "https://generativelanguage.googleapis.com/v1beta",
			}},
		},
		{
			name:   "Gemini OAuth is excluded",
			policy: CredentialPolicyGeminiDecisionsV1,
			auth:   &Auth{Provider: "gemini", Metadata: map[string]any{"access_token": "token"}},
		},
		{
			name:   "OpenAI decision policy rejects the Gemini decision group",
			policy: CredentialPolicyOpenAIDecisionsV1,
			auth:   &Auth{Provider: "decision-gemini", Attributes: map[string]string{AttributeAPIKey: "key"}},
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
