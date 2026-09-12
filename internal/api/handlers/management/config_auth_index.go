package management

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
)

type geminiKeyWithAuthIndex struct {
	config.GeminiKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type claudeKeyWithAuthIndex struct {
	config.ClaudeKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type codexKeyWithAuthIndex struct {
	config.CodexKey
	// APIKeyEntries shadows the embedded field so each bundled credential can
	// carry its own live auth index, mirroring the openai-compatibility view.
	APIKeyEntries []openAICompatibilityAPIKeyWithAuthIndex `json:"api-key-entries,omitempty"`
	AuthIndex     string                                   `json:"auth-index,omitempty"`
}

type xaiKeyWithAuthIndex struct {
	config.XAIKey
	// APIKeyEntries shadows the embedded field so each bundled credential can
	// carry its own live auth index, mirroring the openai-compatibility view.
	APIKeyEntries []openAICompatibilityAPIKeyWithAuthIndex `json:"api-key-entries,omitempty"`
	AuthIndex     string                                   `json:"auth-index,omitempty"`
}

type vertexCompatKeyWithAuthIndex struct {
	config.VertexCompatKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type openAICompatibilityAPIKeyWithAuthIndex struct {
	config.OpenAICompatibilityAPIKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type openAICompatibilityWithAuthIndex struct {
	Name                  string                                   `json:"name"`
	Priority              int                                      `json:"priority,omitempty"`
	Disabled              bool                                     `json:"disabled"`
	Prefix                string                                   `json:"prefix,omitempty"`
	BaseURL               string                                   `json:"base-url"`
	APIKeyEntries         []openAICompatibilityAPIKeyWithAuthIndex `json:"api-key-entries,omitempty"`
	Models                []config.OpenAICompatibilityModel        `json:"models,omitempty"`
	Headers               map[string]string                        `json:"headers,omitempty"`
	SupportPromptCacheKey bool                                     `json:"support-prompt-cache-key,omitempty"`
	DisableCooling        *bool                                    `json:"disable-cooling,omitempty"`
	RequestRetry          *int                                     `json:"request-retry,omitempty"`
	RequestScopedErrors   []config.RequestScopedErrorRule          `json:"request-scoped-errors,omitempty"`
	AuthIndex             string                                   `json:"auth-index,omitempty"`
}

func (h *Handler) liveAuthIndexByID() map[string]string {
	out := map[string]string{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return out
	}
	// authManager.List() returns clones, so EnsureIndex only affects these copies.
	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		id := strings.TrimSpace(auth.ID)
		if id == "" {
			continue
		}
		idx := strings.TrimSpace(auth.Index)
		if idx == "" {
			idx = auth.EnsureIndex()
		}
		if idx == "" {
			continue
		}
		out[id] = idx
	}
	return out
}

func (h *Handler) geminiKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]geminiKeyWithAuthIndex, len(h.cfg.GeminiKey))
	for i := range h.cfg.GeminiKey {
		entry := h.cfg.GeminiKey[i]
		authIndex := ""
		key := strings.TrimSpace(entry.APIKey)
		base := strings.TrimSpace(entry.BaseURL)
		proxyURL := strings.TrimSpace(entry.ProxyURL)
		prefix := strings.TrimSpace(entry.Prefix)
		if key != "" || base != "" {
			id, _ := idGen.Next("gemini:apikey", key, base, proxyURL, prefix, config.FormatSortedHeaders(entry.Headers))
			authIndex = liveIndexByID[id]
		}
		out[i] = geminiKeyWithAuthIndex{
			GeminiKey: entry,
			AuthIndex: authIndex,
		}
	}
	return out
}

func (h *Handler) interactionsKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]geminiKeyWithAuthIndex, len(h.cfg.InteractionsKey))
	for i := range h.cfg.InteractionsKey {
		entry := h.cfg.InteractionsKey[i]
		authIndex := ""
		key := strings.TrimSpace(entry.APIKey)
		base := strings.TrimSpace(entry.BaseURL)
		proxyURL := strings.TrimSpace(entry.ProxyURL)
		prefix := strings.TrimSpace(entry.Prefix)
		if key != "" || base != "" {
			id, _ := idGen.Next("gemini-interactions:apikey", key, base, proxyURL, prefix, config.FormatSortedHeaders(entry.Headers))
			authIndex = liveIndexByID[id]
		}
		out[i] = geminiKeyWithAuthIndex{
			GeminiKey: entry,
			AuthIndex: authIndex,
		}
	}
	return out
}

func (h *Handler) claudeKeysWithAuthIndex() []claudeKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]claudeKeyWithAuthIndex, len(h.cfg.ClaudeKey))
	for i := range h.cfg.ClaudeKey {
		entry := h.cfg.ClaudeKey[i]
		authIndex := ""
		key := strings.TrimSpace(entry.APIKey)
		base := strings.TrimSpace(entry.BaseURL)
		proxyURL := strings.TrimSpace(entry.ProxyURL)
		prefix := strings.TrimSpace(entry.Prefix)
		if key != "" || base != "" {
			id, _ := idGen.Next("claude:apikey", key, base, proxyURL, prefix, config.FormatSortedHeaders(entry.Headers))
			authIndex = liveIndexByID[id]
		}
		out[i] = claudeKeyWithAuthIndex{
			ClaudeKey: entry,
			AuthIndex: authIndex,
		}
	}
	return out
}

func (h *Handler) codexKeysWithAuthIndex() []codexKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]codexKeyWithAuthIndex, len(h.cfg.CodexKey))
	for i := range h.cfg.CodexKey {
		entry := h.cfg.CodexKey[i]
		authIndex := ""
		key := strings.TrimSpace(entry.APIKey)
		base := strings.TrimSpace(entry.BaseURL)
		proxyURL := strings.TrimSpace(entry.ProxyURL)
		prefix := strings.TrimSpace(entry.Prefix)
		if key != "" || base != "" {
			id, _ := idGen.Next("codex:apikey", key, base, proxyURL, prefix, config.FormatSortedHeaders(entry.Headers))
			authIndex = liveIndexByID[id]
		}
		// Bundled api-key-entries synthesize one auth per key; expose each
		// bundled credential's live auth index like the openai-compatibility
		// view does.
		bundledEntries := make([]openAICompatibilityAPIKeyWithAuthIndex, len(entry.APIKeyEntries))
		for j := range entry.APIKeyEntries {
			bundled := entry.APIKeyEntries[j]
			bundledIndex := ""
			bundledKey := strings.TrimSpace(bundled.APIKey)
			bundledProxy := strings.TrimSpace(bundled.ProxyURL)
			if bundledKey != "" {
				credProxy := proxyURL
				if bundledProxy != "" {
					credProxy = bundledProxy
				}
				id, _ := idGen.Next("codex:apikey", bundledKey, base, credProxy, prefix, config.FormatSortedHeaders(entry.Headers))
				bundledIndex = liveIndexByID[id]
			}
			bundledEntries[j] = openAICompatibilityAPIKeyWithAuthIndex{
				OpenAICompatibilityAPIKey: bundled,
				AuthIndex:                 bundledIndex,
			}
		}
		out[i] = codexKeyWithAuthIndex{
			CodexKey:      entry,
			AuthIndex:     authIndex,
			APIKeyEntries: bundledEntries,
		}
	}
	return out
}

func (h *Handler) xaiKeysWithAuthIndex() []xaiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]xaiKeyWithAuthIndex, len(h.cfg.XAIKey))
	for i := range h.cfg.XAIKey {
		entry := h.cfg.XAIKey[i]
		authIndex := ""
		key := strings.TrimSpace(entry.APIKey)
		base := strings.TrimSpace(entry.BaseURL)
		proxyURL := strings.TrimSpace(entry.ProxyURL)
		prefix := strings.TrimSpace(entry.Prefix)
		if key != "" || base != "" {
			id, _ := idGen.Next("xai:apikey", key, base, proxyURL, prefix, config.FormatSortedHeaders(entry.Headers))
			authIndex = liveIndexByID[id]
		}
		// Bundled xai api-key-entries are not synthesized into auths (the xAI
		// resolver only matches the top-level APIKey), so they carry no auth
		// index; keep them visible so the view still reflects the config.
		bundledEntries := make([]openAICompatibilityAPIKeyWithAuthIndex, len(entry.APIKeyEntries))
		for j := range entry.APIKeyEntries {
			bundledEntries[j] = openAICompatibilityAPIKeyWithAuthIndex{
				OpenAICompatibilityAPIKey: entry.APIKeyEntries[j],
			}
		}
		out[i] = xaiKeyWithAuthIndex{
			XAIKey:        entry,
			AuthIndex:     authIndex,
			APIKeyEntries: bundledEntries,
		}
	}
	return out
}

func (h *Handler) vertexCompatKeysWithAuthIndex() []vertexCompatKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	idGen := synthesizer.NewStableIDGenerator()
	out := make([]vertexCompatKeyWithAuthIndex, len(h.cfg.VertexCompatAPIKey))
	for i := range h.cfg.VertexCompatAPIKey {
		entry := h.cfg.VertexCompatAPIKey[i]
		id, _ := idGen.Next("vertex:apikey", entry.APIKey, entry.BaseURL, entry.ProxyURL)
		authIndex := liveIndexByID[id]
		out[i] = vertexCompatKeyWithAuthIndex{
			VertexCompatKey: entry,
			AuthIndex:       authIndex,
		}
	}
	return out
}

func (h *Handler) openAICompatibilityWithAuthIndex() []openAICompatibilityWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	normalized := normalizedOpenAICompatibilityEntries(h.cfg.OpenAICompatibility)
	out := make([]openAICompatibilityWithAuthIndex, len(normalized))
	idGen := synthesizer.NewStableIDGenerator()
	for i := range normalized {
		entry := normalized[i]
		providerName := strings.ToLower(strings.TrimSpace(entry.Name))
		if providerName == "" {
			providerName = "openai-compatibility"
		}
		idKind := fmt.Sprintf("openai-compatibility:%s", providerName)

		response := openAICompatibilityWithAuthIndex{
			Name:                  entry.Name,
			Priority:              entry.Priority,
			Disabled:              entry.Disabled,
			Prefix:                entry.Prefix,
			BaseURL:               entry.BaseURL,
			Models:                entry.Models,
			Headers:               entry.Headers,
			SupportPromptCacheKey: entry.SupportPromptCacheKey,
			DisableCooling:        entry.DisableCooling,
			RequestRetry:          entry.RequestRetry,
			RequestScopedErrors:   entry.RequestScopedErrors,
			AuthIndex:             "",
		}
		if len(entry.APIKeyEntries) == 0 {
			id, _ := idGen.Next(idKind, entry.BaseURL)
			response.AuthIndex = liveIndexByID[id]
		} else {
			response.APIKeyEntries = make([]openAICompatibilityAPIKeyWithAuthIndex, len(entry.APIKeyEntries))
			for j := range entry.APIKeyEntries {
				apiKeyEntry := entry.APIKeyEntries[j]
				id, _ := idGen.Next(idKind, apiKeyEntry.APIKey, entry.BaseURL, apiKeyEntry.ProxyURL)
				response.APIKeyEntries[j] = openAICompatibilityAPIKeyWithAuthIndex{
					OpenAICompatibilityAPIKey: apiKeyEntry,
					AuthIndex:                 liveIndexByID[id],
				}
			}
		}
		out[i] = response
	}
	return out
}
