package common

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// codexReasoningLevelOrder is the canonical low-to-high ordering of discrete
// reasoning effort levels.
var codexReasoningLevelOrder = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// DefaultCodexReasoningEffort returns the reasoning effort to inject when the
// source request carries no explicit effort. The historical hardcoded default
// is "medium", but a model whose registered thinking levels form a discrete
// subset that excludes "medium" (e.g. [low high max]) must receive the closest
// supported level instead; otherwise downstream thinking validation (and the
// upstream provider) rejects the request for a value the user never asked for.
//
// Models without registered level metadata keep the "medium" default.
func DefaultCodexReasoningEffort(modelName string) string {
	const fallback = "medium"
	info := registry.LookupModelInfo(modelName)
	if info == nil || info.Thinking == nil || len(info.Thinking.Levels) == 0 {
		return fallback
	}
	target := reasoningLevelIndex(fallback)
	bestIdx, bestDist, best := -1, len(codexReasoningLevelOrder)+1, ""
	for _, raw := range info.Thinking.Levels {
		level := strings.ToLower(strings.TrimSpace(raw))
		if level == fallback {
			return fallback
		}
		idx := reasoningLevelIndex(level)
		if idx < 0 {
			continue
		}
		dist := idx - target
		if dist < 0 {
			dist = -dist
		}
		// Tie-break towards the lower level, matching thinking.clampLevel.
		if dist < bestDist || (dist == bestDist && idx < bestIdx) {
			best, bestDist, bestIdx = level, dist, idx
		}
	}
	if best == "" {
		return fallback
	}
	return best
}

func reasoningLevelIndex(level string) int {
	for i, v := range codexReasoningLevelOrder {
		if v == level {
			return i
		}
	}
	return -1
}
