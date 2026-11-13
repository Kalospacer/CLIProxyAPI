package auth

import (
	"net/url"
	"strings"
)

const (
	// CredentialPolicyCodexAlphaSearchV1 selects credentials supported by Codex Alpha Search.
	CredentialPolicyCodexAlphaSearchV1 = "codex_alpha_search_v1"

	// CredentialPolicyTypeSafeSystemOneV1 selects credentials supported by the TypeSafe
	// jev evaluation protocol (/v1/systemone).
	CredentialPolicyTypeSafeSystemOneV1 = "typesafe_systemone_v1"

	// CredentialPolicyOpenAIDecisionsV1 selects official OpenAI API keys for the
	// native Decisions API, not Codex OAuth or third-party Responses providers.
	CredentialPolicyOpenAIDecisionsV1 = "openai_decisions_v1"

	// CredentialPolicyGeminiDecisionsV1 selects AI Studio API keys for the native
	// Decisions API, not Gemini OAuth or its OpenAI-compatible chat endpoint.
	CredentialPolicyGeminiDecisionsV1 = "gemini_decisions_v1"
)

func normalizeCredentialPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case CredentialPolicyCodexAlphaSearchV1:
		return CredentialPolicyCodexAlphaSearchV1
	case CredentialPolicyTypeSafeSystemOneV1, CredentialPolicyOpenAIDecisionsV1, CredentialPolicyGeminiDecisionsV1:
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return ""
	}
}

func credentialPolicyAllows(policy string, auth *Auth) bool {
	if auth == nil {
		return false
	}
	switch policy {
	case CredentialPolicyCodexAlphaSearchV1:
		if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
			return false
		}
		switch auth.AuthKind() {
		case AuthKindOAuth:
			return true
		case AuthKindAPIKey:
			return strings.EqualFold(authAttribute(auth, AttributeCodexAlphaSearch), "true")
		default:
			return false
		}
	case CredentialPolicyTypeSafeSystemOneV1:
		if !strings.EqualFold(strings.TrimSpace(auth.Provider), "typesafe") {
			return false
		}
		return auth.AuthKind() == AuthKindAPIKey
	case CredentialPolicyOpenAIDecisionsV1:
		return auth.AuthKind() == AuthKindAPIKey && decisionCredentialHostAllowed(auth, "api.openai.com")
	case CredentialPolicyGeminiDecisionsV1:
		return auth.AuthKind() == AuthKindAPIKey && decisionCredentialHostAllowed(auth, "generativelanguage.googleapis.com")
	default:
		return false
	}
}

// decisionCredentialHostAllowed prevents keys for another Codex/Gemini provider
// from being sent to a native Decisions upstream. Empty base URLs use the
// provider's official default and are therefore eligible.
func decisionCredentialHostAllowed(current *Auth, officialHost string) bool {
	provider := "codex"
	version := "/v1"
	if officialHost == "generativelanguage.googleapis.com" {
		provider = "gemini"
		version = "/v1beta"
	}
	if current == nil || !strings.EqualFold(strings.TrimSpace(current.Provider), provider) {
		return false
	}
	baseURL := strings.TrimSpace(authAttribute(current, "base_url"))
	if baseURL == "" {
		return true
	}
	parsed, errParse := url.Parse(baseURL)
	if errParse != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") || !strings.EqualFold(parsed.Hostname(), officialHost) {
		return false
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	return path == "" || path == version || path == version+"/decisions"
}
