package executor

import (
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestAntigravityRewritesNarrativeTextAndPreservesTools(t *testing.T) {
	cfg := &config.Config{Antigravity: config.AntigravityConfig{SensitiveWordsReplace: map[string]string{"harnessmark": "agent"}}}
	payload := []byte(`{"request":{"systemInstruction":{"parts":[{"text":"harnessmark system"}]},"contents":[{"role":"user","parts":[{"text":"harnessmark user"}]},{"role":"model","parts":[{"functionCall":{"name":"harnessmark_read","args":{"harnessmark_arg":"harnessmark value"}}}]},{"role":"user","parts":[{"functionResponse":{"name":"harnessmark_read","response":{"harnessmark_key":"harnessmark result"}}}]},{"role":"model","parts":[{"text":"harnessmark assistant"}]}],"tools":[{"functionDeclarations":[{"name":"harnessmark_read","description":"harnessmark description","parameters":{"type":"object","properties":{"harnessmark_arg":{"type":"string"}},"required":["harnessmark_arg"]}}]}]}}`)
	wire := captureAntigravityWireBody(t, cfg, "gemini-4-carbon-high", payload)
	for path, want := range map[string]string{
		"request.systemInstruction.parts.0.text":                               "agent system",
		"request.contents.0.parts.0.text":                                      "agent user",
		"request.contents.3.parts.0.text":                                      "agent assistant",
		"request.contents.1.parts.0.functionCall.name":                         "harnessmark_read",
		"request.contents.1.parts.0.functionCall.args.harnessmark_arg":         "harnessmark value",
		"request.contents.2.parts.0.functionResponse.name":                     "harnessmark_read",
		"request.contents.2.parts.0.functionResponse.response.harnessmark_key": "harnessmark result",
		"request.tools.0.functionDeclarations.0.name":                          "harnessmark_read",
		"request.tools.0.functionDeclarations.0.description":                   "harnessmark description",
		"request.tools.0.functionDeclarations.0.parameters.required.0":         "harnessmark_arg",
	} {
		if got := ojson.GetBytes(wire, path).String(); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if !ojson.GetBytes(wire, "request.tools.0.functionDeclarations.0.parameters.properties.harnessmark_arg").Exists() {
		t.Error("tool schema key was changed")
	}
}
