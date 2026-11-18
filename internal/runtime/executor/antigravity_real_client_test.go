package executor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestAntigravityRealClientCaptures(t *testing.T) {
	rules, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(rules)
	if errParse != nil {
		t.Fatal(errParse)
	}
	paths, errGlob := filepath.Glob("testdata/antigravity_real_client_*.json")
	if errGlob != nil || len(paths) == 0 {
		t.Fatalf("missing real client captures: %v", errGlob)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, errFixture := os.ReadFile(path)
			if errFixture != nil {
				t.Fatal(errFixture)
			}
			var fixture struct {
				Format   string            `json:"format"`
				Requests []json.RawMessage `json:"requests"`
				Rewrites []struct {
					Source string `json:"source"`
					Want   string `json:"want"`
				} `json:"rewrites"`
				Preserve []string `json:"preserve"`
				Absent   []string `json:"absent"`
			}
			if errDecode := json.Unmarshal(raw, &fixture); errDecode != nil {
				t.Fatal(errDecode)
			}
			format := sdktranslator.Format(fixture.Format)
			matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
			for _, rule := range fixture.Rewrites {
				if got := matcher.ObfuscateText(rule.Source); got != rule.Want {
					t.Errorf("captured guidance not rewritten: %.100s", rule.Source)
				}
			}
			preservationSeen := make(map[string]bool)
			for _, payload := range fixture.Requests {
				options := cliproxyexecutor.Options{SourceFormat: format, OriginalRequest: payload}
				before := ojson.NewDoc(captureAntigravityWireBodyWithOptions(t, &config.Config{}, "gemini-3.1-pro-preview", payload, options))
				after := ojson.NewDoc(captureAntigravityWireBodyWithOptions(t, cfg, "gemini-3.1-pro-preview", payload, options))
				for _, key := range []string{"request.tools", "request.toolConfig", "request.generationConfig"} {
					if !reflect.DeepEqual(before.Get(key).Value(), after.Get(key).Value()) {
						t.Errorf("text rewriting changed %s", key)
					}
				}
				beforeContents := before.Get("request.contents").Array()
				afterContents := after.Get("request.contents").Array()
				if len(beforeContents) != len(afterContents) {
					t.Fatal("text rewriting changed message count")
				}
				for i, content := range beforeContents {
					parts := content.Get("parts").Array()
					other := afterContents[i].Get("parts").Array()
					if len(parts) != len(other) {
						t.Fatal("text rewriting changed part count")
					}
					for j, part := range parts {
						if part.Get("functionCall").Exists() || part.Get("functionResponse").Exists() {
							if !reflect.DeepEqual(part.Value(), other[j].Value()) {
								t.Error("text rewriting changed a tool call or result")
							}
						}
					}
				}
				text := ""
				for _, part := range after.Get("request.systemInstruction.parts").Array() {
					text += part.Get("text").String() + "\n"
				}
				for _, content := range afterContents {
					for _, part := range content.Get("parts").Array() {
						text += part.Get("text").String()
					}
				}
				for _, source := range fixture.Absent {
					if strings.Contains(text, source) {
						t.Errorf("captured identity or guidance remains: %.100s", source)
					}
				}
				for _, preserved := range fixture.Preserve {
					if strings.Contains(before.Get("request").String(), preserved) {
						preservationSeen[preserved] = true
						if !strings.Contains(text, preserved) {
							t.Errorf("executable context lost: %s", preserved)
						}
					}
				}
			}
			for _, preserved := range fixture.Preserve {
				if !preservationSeen[preserved] {
					t.Errorf("capture lacks required context: %s", preserved)
				}
			}
		})
	}
}
