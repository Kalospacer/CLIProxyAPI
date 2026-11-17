package executor

import (
	"os"
	"testing"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

type antigravityPromptSample struct {
	Name  string `json:"name"`
	Input string `json:"input"`
	Want  string `json:"want"`
}

type antigravityPromptCorpus struct {
	Cases   []antigravityPromptSample `json:"cases"`
	Samples []antigravityPromptSample `json:"samples"`
}

func loadAntigravityPromptCorpus(t *testing.T, path string) antigravityPromptCorpus {
	t.Helper()
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatal(errRead)
	}
	var data antigravityPromptCorpus
	if errDecode := json.Unmarshal(raw, &data); errDecode != nil {
		t.Fatal(errDecode)
	}
	return data
}

func TestAntigravityHarnessRulesRewritePromptCorpora(t *testing.T) {
	rulesRaw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(rulesRaw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	matcher := NewAntigravityExecutor(cfg).getSensitiveWordMatcher()
	if matcher == nil {
		t.Fatal("missing harness matcher")
	}
	for _, corpus := range []struct {
		name, path     string
		cases, samples int
	}{
		{"codex", "testdata/antigravity_codex_samples.json", 46, 5},
	} {
		t.Run(corpus.name, func(t *testing.T) {
			data := loadAntigravityPromptCorpus(t, corpus.path)
			if len(data.Cases) != corpus.cases || len(data.Samples) != corpus.samples {
				t.Fatalf("unexpected sample count: %d %d", len(data.Cases), len(data.Samples))
			}
			for i, tc := range data.Cases {
				if got := matcher.ObfuscateText(tc.Input); got != tc.Want {
					t.Errorf("source section %d did not receive its expected neutral rewrite", i)
				}
			}
			for _, tc := range data.Samples {
				if got := matcher.ObfuscateText(tc.Input); got != tc.Want {
					t.Errorf("full template %s did not receive its expected neutral rewrite", tc.Name)
				}
			}
		})
	}
}

func TestAntigravityPromptCorporaAreRewrittenOnWire(t *testing.T) {
	rulesRaw, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(rulesRaw)
	if errParse != nil {
		t.Fatal(errParse)
	}
	data := loadAntigravityPromptCorpus(t, "testdata/antigravity_codex_samples.json")
	for _, sample := range data.Samples {
		t.Run(sample.Name, func(t *testing.T) {
			doc := ojson.NewDocString(`{"request":{"systemInstruction":{"parts":[{"text":""}]},"contents":[{"role":"user","parts":[{"text":""}]}]}}`)
			if err := doc.Set("request.systemInstruction.parts.0.text", sample.Input); err != nil {
				t.Fatal(err)
			}
			if err := doc.Set("request.contents.0.parts.0.text", sample.Input); err != nil {
				t.Fatal(err)
			}
			wire := captureAntigravityWireBody(t, cfg, "gemini-4-carbon-high", doc.Bytes())
			for _, path := range []string{"request.systemInstruction.parts.0.text", "request.contents.0.parts.0.text"} {
				if got := ojson.GetBytes(wire, path).String(); got != sample.Want {
					t.Errorf("%s: rewritten output differs", path)
				}
			}
		})
	}
}
