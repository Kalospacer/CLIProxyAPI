package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// kimiModelsAPIDocHandler serves GET /v1/models/api.json.
//
// It renders the live model registry as a Kimi desktop BYOK discovery document
// (the format Kimi's "third-party model gateway" importer probes after
// /v1/models). The document is generated per request, so config hot-reloads and
// model availability changes are reflected immediately without any caching.
//
// Kimi decides the wire dialect per model id: ids prefixed with "kimi-" use the
// kimi dialect (OpenAI chat completions), ids containing "gpt-"/"codex-" use the
// OpenAI Responses API; all other ids are skipped by the client. Aliases are
// managed through the existing per-channel models[].alias config, so exposing a
// model to Kimi is just adding a "kimi-"-prefixed alias for it.
func (s *Server) kimiModelsAPIDocHandler(c *gin.Context) {
	infos := registry.GetGlobalRegistry().GetAvailableModelInfos()
	models := make(map[string]any, len(infos))
	for _, info := range infos {
		if info == nil || info.ID == "" {
			continue
		}
		name := strings.TrimSpace(info.DisplayName)
		if name == "" {
			name = info.ID
		}
		contextLength := info.ContextLength
		if contextLength <= 0 {
			contextLength = info.MaxContextLength
		}
		if contextLength <= 0 {
			contextLength = info.InputTokenLimit
		}
		if contextLength <= 0 {
			contextLength = 131072
		}
		outputLength := info.MaxCompletionTokens
		if outputLength <= 0 {
			outputLength = info.OutputTokenLimit
		}
		if outputLength <= 0 {
			outputLength = 32768
		}
		inputModalities := make([]string, 0, len(info.SupportedInputModalities))
		seen := make(map[string]struct{}, len(info.SupportedInputModalities))
		for _, modality := range info.SupportedInputModalities {
			m := strings.ToLower(strings.TrimSpace(modality))
			if m == "" {
				continue
			}
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			inputModalities = append(inputModalities, m)
		}
		if len(inputModalities) == 0 {
			inputModalities = []string{"text"}
		}
		models[info.ID] = map[string]any{
			"name": name,
			"limit": map[string]any{
				"context": contextLength,
				"output":  outputLength,
			},
			"tool_call": true,
			"reasoning": info.Thinking != nil,
			"modalities": map[string]any{
				"input": inputModalities,
			},
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"cpa": gin.H{
			"type":   "kimi",
			"models": models,
		},
	})
}
