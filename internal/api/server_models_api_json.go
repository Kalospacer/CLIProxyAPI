package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// kimiModelOverride describes per-model display overrides for the Kimi BYOK
// discovery document. Fields left at their zero value keep the registry
// derived default; Reasoning and ToolCall use pointers so an explicit false
// can override a truthy default. ReasoningOptions, when non-empty, replaces
// the generated reasoning_options list outright (use [] to clear it).
type kimiModelOverride struct {
	Name            string          `json:"name"`
	Context         int             `json:"context"`
	Output          int             `json:"output"`
	Input           int             `json:"input"`
	Reasoning       *bool           `json:"reasoning"`
	ToolCall        *bool           `json:"tool_call"`
	Modalities      []string        `json:"modalities"`
	ReasoningOptions json.RawMessage `json:"reasoning_options"`
}

// kimiModelsOverridesFile is the on-disk layout of kimi-apijson.json.
type kimiModelsOverridesFile struct {
	Models map[string]kimiModelOverride `json:"models"`
}

// kimiModelsOverridesPath returns the overrides file location: a
// "kimi-apijson.json" file next to the main YAML config file.
func (s *Server) kimiModelsOverridesPath() string {
	configPath := strings.TrimSpace(s.configFilePath)
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "kimi-apijson.json")
}

// loadKimiModelsOverrides reads the overrides file. A missing or unreadable
// file is not an error; it simply yields no overrides.
func (s *Server) loadKimiModelsOverrides() map[string]kimiModelOverride {
	path := s.kimiModelsOverridesPath()
	if path == "" {
		return nil
	}
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		return nil
	}
	var file kimiModelsOverridesFile
	if errUnmarshal := json.Unmarshal(raw, &file); errUnmarshal != nil {
		return nil
	}
	return file.Models
}

// defaultKimiReasoningOptions derives the reasoning_options list from the
// registry thinking metadata: discrete levels become an effort option, and a
// zero-allowed budget appends the null off-tier. It returns nil when the
// model has no level based reasoning.
func defaultKimiReasoningOptions(info *registry.ModelInfo) []any {
	if info == nil || info.Thinking == nil || len(info.Thinking.Levels) == 0 {
		return nil
	}
	values := make([]any, 0, len(info.Thinking.Levels)+1)
	for _, level := range info.Thinking.Levels {
		trimmed := strings.TrimSpace(level)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return nil
	}
	if info.Thinking.ZeroAllowed {
		values = append(values, nil)
	}
	return []any{
		map[string]any{"type": "effort", "values": values},
	}
}

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
//
// The document follows the kosong catalog model schema: name, limit
// (context/output/input), tool_call, reasoning, reasoning_options (effort
// levels / off tier / toggle) and modalities.input (image/video/audio become
// capability badges in the client).
//
// Per-model display values can be overridden via a "kimi-apijson.json" file
// next to the main config file. The file is re-read on every request, so edits
// made by the management UI or by hand take effect immediately.
func (s *Server) kimiModelsAPIDocHandler(c *gin.Context) {
	overrides := s.loadKimiModelsOverrides()
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
		toolCall := true
		reasoning := info.Thinking != nil
		reasoningOptions := defaultKimiReasoningOptions(info)

		if override, okOverride := overrides[info.ID]; okOverride {
			if trimmed := strings.TrimSpace(override.Name); trimmed != "" {
				name = trimmed
			}
			if override.Context > 0 {
				contextLength = override.Context
			}
			if override.Output > 0 {
				outputLength = override.Output
			}
			if override.Reasoning != nil {
				reasoning = *override.Reasoning
			}
			if override.ToolCall != nil {
				toolCall = *override.ToolCall
			}
			if len(override.Modalities) > 0 {
				cleaned := make([]string, 0, len(override.Modalities))
				for _, modality := range override.Modalities {
					m := strings.ToLower(strings.TrimSpace(modality))
					if m != "" {
						cleaned = append(cleaned, m)
					}
				}
				if len(cleaned) > 0 {
					inputModalities = cleaned
				}
			}
			if len(override.ReasoningOptions) > 0 {
				var parsed any
				if errUnmarshal := json.Unmarshal(override.ReasoningOptions, &parsed); errUnmarshal == nil {
					if list, isList := parsed.([]any); isList {
						reasoningOptions = list
					}
				}
			}
		}

		limit := map[string]any{
			"context": contextLength,
			"output":  outputLength,
		}
		if override, okOverride := overrides[info.ID]; okOverride && override.Input > 0 {
			inputLimit := override.Input
			if inputLimit > contextLength {
				inputLimit = contextLength
			}
			limit["input"] = inputLimit
		}

		entry := map[string]any{
			"name":      name,
			"limit":     limit,
			"tool_call": toolCall,
			"reasoning": reasoning,
			"modalities": map[string]any{
				"input": inputModalities,
			},
		}
		if len(reasoningOptions) > 0 {
			entry["reasoning_options"] = reasoningOptions
		}
		models[info.ID] = entry
	}
	c.JSON(http.StatusOK, gin.H{
		"cpa": gin.H{
			"type":   "kimi",
			"models": models,
		},
	})
}
