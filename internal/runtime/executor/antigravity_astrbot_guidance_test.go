package executor

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestAntigravityAstrBotCapturedGuidancePreservesToolsAndSafety(t *testing.T) {
	rules, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(rules)
	if errParse != nil {
		t.Fatal(errParse)
	}
	// 来自原版 4.28.0 主代理的实际出站记录，仅匿名化本地路径并省略传输信封。
	raw, errFixture := os.ReadFile("testdata/antigravity_astrbot_4_28_request.json")
	if errFixture != nil {
		t.Fatal(errFixture)
	}
	before := ojson.NewDoc(raw)
	wire := captureAntigravityWireBody(t, cfg, "gemini-3.1-pro-preview", raw)
	output := ojson.NewDoc(wire)
	system := output.Get("request.systemInstruction.parts.0.text").String()
	cases := []struct{ source, replacement string }{
		{"You are running in Safe Mode.", "Apply the following safety constraints."},
		{"You have access to the host local environment and can execute shell commands and Python code.", "Use the available shell and Python tools to work in the local environment."},
		{"Local shell commands automatically return a managed session when they outlive the initial wait.", "A command that exceeds the initial wait continues in a managed shell session."},
		{"Use `astrbot_shell_session` to list, poll, write raw text or complete lines to, interrupt, or terminate those sessions.", "Manage continuing commands with `astrbot_shell_session`: list, poll, send raw text or complete lines, interrupt, or terminate."},
		{"Use its `write_line` action for line-oriented programs so the session receives a real line feed.", "For line-oriented input, use `write_line` to send the terminating newline."},
		{"Do not add `&`, `nohup`, or another detachment wrapper for ordinary long-running commands.", "Keep ordinary long-running commands under session management rather than detaching them with `&`, `nohup`, or similar wrappers."},
		{"When using tools: never return an empty response; briefly explain the purpose when starting a new type of task, but not before every tool call; follow the tool schema exactly and do not invent parameters; keep the conversation style consistent.", "Briefly explain the purpose at the start of a new task type, without narrating every tool call. Produce a nonempty response, follow declared tool schemas without inventing parameters, and keep the established conversation style."},
		{"Prefer relative paths within the workspace; do not assume this behavior for other tools.", "Prefer workspace-relative paths for these tools; do not apply that assumption to other tools."},
	}
	for _, tc := range cases {
		if !strings.Contains(before.Get("request.systemInstruction.parts.0.text").String(), tc.source) {
			t.Fatalf("captured fixture lacks expected source: %q", tc.source)
		}
		if strings.Contains(system, tc.source) || !strings.Contains(system, tc.replacement) {
			t.Errorf("measured guidance not rewritten: %q", tc.source)
		}
	}
	for _, path := range []string{"request.tools", "request.contents", "request.generationConfig"} {
		if !reflect.DeepEqual(before.Get(path).Value(), output.Get(path).Value()) {
			t.Errorf("guidance rewrite changed %s", path)
		}
	}
	for _, preserved := range []string{
		"Avoid sexual, violent, extremist, hateful, illegal, or harmful content.",
		"Do NOT comment on or take positions on real-world political and sensitive controversial topics.",
		"The runtime shell is PowerShell 7 (pwsh.exe).", "`astrbot_shell_session`", "`write_line`", "Current workspace:",
	} {
		if !strings.Contains(system, preserved) {
			t.Errorf("runtime or safety instruction lost: %q", preserved)
		}
	}
}

func TestAntigravityAstrBotGuidanceSkipsToolDescriptions(t *testing.T) {
	rules, errRead := os.ReadFile("testdata/antigravity_harness_rules.yaml")
	if errRead != nil {
		t.Fatal(errRead)
	}
	cfg, errParse := config.ParseConfigBytes(rules)
	if errParse != nil {
		t.Fatal(errParse)
	}
	const source = "You are running in Safe Mode."
	doc := ojson.NewDocString(`{"request":{"contents":[{"role":"user","parts":[{"text":"You are running in Safe Mode."}]}],"tools":[{"functionDeclarations":[{"name":"astrbot_tool","description":"You are running in Safe Mode.","parameters":{"type":"object","properties":{"mode":{"type":"string","description":"You are running in Safe Mode."}}}}]}]}}`)
	wire := captureAntigravityWireBody(t, cfg, "gemini-3.1-pro-preview", doc.Bytes())
	out := ojson.NewDoc(wire)
	if out.Get("request.contents.0.parts.0.text").String() != "Apply the following safety constraints." {
		t.Fatal("ordinary conversation text was not rewritten")
	}
	for _, path := range []string{"request.tools.0.functionDeclarations.0.description", "request.tools.0.functionDeclarations.0.parameters.properties.mode.description"} {
		if out.Get(path).String() != source {
			t.Errorf("guidance rewrite changed tool field %s", path)
		}
	}
}
