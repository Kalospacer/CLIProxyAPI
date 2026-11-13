package auth

import "strings"

const (
	// CredentialPolicyCodexAlphaSearchV1 selects credentials supported by Codex Alpha Search.
	CredentialPolicyCodexAlphaSearchV1 = "codex_alpha_search_v1"

	// CredentialPolicyTypeSafeSystemOneV1 selects credentials supported by the TypeSafe
	// jev evaluation protocol (/v1/systemone).
	CredentialPolicyTypeSafeSystemOneV1 = "typesafe_systemone_v1"

	// CredentialPolicyOpenAIDecisionsV1 selects credentials of the dedicated OpenAI
	// Decisions provider group. Decision models are not chat models, so ordinary
	// Codex credentials must never serve them.
	CredentialPolicyOpenAIDecisionsV1 = "openai_decisions_v1"

	// CredentialPolicyGeminiDecisionsV1 selects credentials of the dedicated Gemini
	// Decisions provider group.
	CredentialPolicyGeminiDecisionsV1 = "gemini_decisions_v1"
)

// Decision provider identifiers. These are duplicated from the executor package
// to keep this portable policy helper free of internal imports.
const (
	decisionOpenAIProvider = "decision-openai"
	decisionGeminiProvider = "decision-gemini"
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
		// Decision credentials live in their own provider group, so group
		// membership already excludes ordinary Codex chat credentials.
		return strings.EqualFold(strings.TrimSpace(auth.Provider), decisionOpenAIProvider) &&
			auth.AuthKind() == AuthKindAPIKey &&
			!authWildcardExcluded(auth)
	case CredentialPolicyGeminiDecisionsV1:
		return strings.EqualFold(strings.TrimSpace(auth.Provider), decisionGeminiProvider) &&
			auth.AuthKind() == AuthKindAPIKey &&
			!authWildcardExcluded(auth)
	default:
		return false
	}
}

func authWildcardExcluded(auth *Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	raw := strings.TrimSpace(auth.Attributes["excluded_models"])
	if raw == "" {
		return false
	}
	for _, entry := range strings.Split(raw, ",") {
		if strings.TrimSpace(entry) == "*" {
			return true
		}
	}
	return false
}
