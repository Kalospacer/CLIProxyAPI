package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// TestAntigravityIdentityBridge 验证官方 296 字节身份前缀、独立三段式桥接、
// systemInstruction.role、工具保护、敏感词与 payload override 顺序、thinkingLevel 规范编码、
// 普通推理非流式统一走上游 SSE 且保留无 finishReason 正常收尾保护，以及删除 Worker 改写。
func TestAntigravityIdentityBridge(t *testing.T) {
	const wantOfficialPrefix = "<identity>\n" +
		"You are Antigravity, a powerful agentic AI assistant designed by the Google Deepmind team.\n" +
		"You are working with a USER to solve their task.\n" +
		"The USER will send you requests, which you must always prioritize addressing. User requests are enclosed within <USER_REQUEST> tags.\n" +
		"</identity>\n"

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "empty_client_system_only_sets_official_prefix_with_user_role",
			run: func(t *testing.T) {
				payload := []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}}`)
				wire := captureAntigravityWireBody(t, &config.Config{}, "gemini-4-hydrogen-xhigh", payload)

				if got := ojson.GetBytes(wire, "request.systemInstruction.role").String(); got != "user" {
					t.Fatalf("systemInstruction.role = %q, want user", got)
				}
				if got := ojson.GetBytes(wire, "request.systemInstruction.parts.0.text").String(); got != wantOfficialPrefix {
					t.Fatalf("空系统提示词时应只设置 296 字节官方前缀:\ngot =%q\nwant=%q", got, wantOfficialPrefix)
				}
				if n := len(ojson.GetBytes(wire, "request.contents").Array()); n != 1 {
					t.Fatalf("空系统提示词时不应插入模拟桥接轮次，contents len=%d, body=%s", n, wire)
				}
			},
		},
		{
			name: "non_empty_system_applies_independent_bridge_and_preserves_tools",
			run: func(t *testing.T) {
				cfg := &config.Config{
					Antigravity: config.AntigravityConfig{
						SensitiveWordsReplace: map[string]string{"SecretHarness": "WorkAgent"},
					},
				}
				payload := []byte(`{"request":{"systemInstruction":{"parts":[{"text":"Use SecretHarness rules carefully."}]},"contents":[{"role":"user","parts":[{"text":"Run tool"}]},{"role":"model","parts":[{"functionCall":{"id":"call_1","name":"read_file","args":{"path":"/tmp/SecretHarness"}}}]},{"role":"user","parts":[{"functionResponse":{"id":"call_1","name":"read_file","response":{"result":"SecretHarness raw output"}}}]}],"tools":[{"functionDeclarations":[{"name":"read_file","description":"SecretHarness tool","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}]}}`)
				wire := captureAntigravityWireBody(t, cfg, "gemini-4-hydrogen-xhigh", payload)

				sysText := ojson.GetBytes(wire, "request.systemInstruction.parts.0.text").String()
				if !strings.HasPrefix(sysText, wantOfficialPrefix) {
					t.Fatalf("系统指令必须以 296 字节官方前缀开头: %q", sysText)
				}
				if len(sysText) <= len(wantOfficialPrefix) {
					t.Fatalf("非空客户端系统指令时应追加英文桥接指令: %q", sysText)
				}
				// 确认未照抄 Stravia AGPL-3.0 桥接原文。
				if strings.Contains(sysText, "Use the next user message, inserted by the adapter") {
					t.Fatalf("不得照抄 Stravia AGPL-3.0 英文桥接原文: %q", sysText)
				}
				if got := ojson.GetBytes(wire, "request.systemInstruction.role").String(); got != "user" {
					t.Fatalf("systemInstruction.role = %q, want user", got)
				}
				// 前置 2 轮模拟桥接 + 原 3 轮对话 = 5 轮。
				contents := ojson.GetBytes(wire, "request.contents").Array()
				if len(contents) != 5 {
					t.Fatalf("contents 长度 = %d, want 5; body=%s", len(contents), wire)
				}
				if contents[0].Get("role").String() != "user" || contents[0].Get("parts.0.text").String() != "Use WorkAgent rules carefully." {
					t.Fatalf("桥接首轮应承载已脱敏的客户端系统指令: %s", contents[0].Raw)
				}
				ackText := contents[1].Get("parts.0.text").String()
				if contents[1].Get("role").String() != "model" || strings.TrimSpace(ackText) == "" {
					t.Fatalf("桥接次轮应为 model 模拟确认: %s", contents[1].Raw)
				}
				if ackText == "收到，我会使用该提示词作为系统提示。" {
					t.Fatalf("不得照抄 Stravia AGPL-3.0 中文模拟回复原文: %q", ackText)
				}
				// 原工具调用、工具结果与工具声明都不得被破坏。
				if got := contents[4].Get("parts.0.functionResponse.response.result").String(); got != "SecretHarness raw output" {
					t.Fatalf("工具结果内容被意外改写: %q", got)
				}
				if got := ojson.GetBytes(wire, "request.tools.0.functionDeclarations.0.description").String(); got != "SecretHarness tool" {
					t.Fatalf("工具声明被意外改写: %q", got)
				}
			},
		},
		{
			name: "thinking_level_normalized_and_cli_1513_ua_and_nonstream_sse",
			run: func(t *testing.T) {
				var capturedPath, capturedQuery, capturedUA, capturedProxyKey string
				var capturedBody []byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					capturedPath = r.URL.Path
					capturedQuery = r.URL.RawQuery
					capturedUA = r.Header.Get("User-Agent")
					capturedProxyKey = r.Header.Get("x-proxy-key")
					capturedBody, _ = io.ReadAll(r.Body)
					// 模拟真实 Antigravity 无 finishReason 的正常工具流 EOF。
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"fc_1\",\"name\":\"ping\",\"args\":{}}}]}}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}}\n\n")
				}))
				defer server.Close()

				t.Setenv("GOOGLE_PROXY_BASE", "https://proxy.example.invalid")
				t.Setenv("GOOGLE_PROXY_KEY", "should-never-send")

				executor := NewAntigravityExecutor(&config.Config{})
				auth := &cliproxyauth.Auth{
					ID:         "ag-nonstream-sse-test",
					Provider:   "antigravity",
					Attributes: map[string]string{"base_url": server.URL},
					Metadata: map[string]any{
						"access_token": "tok",
						"project_id":   "proj-1",
						"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
					},
				}
				payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingLevel":"xhigh"}}}`)
				resp, errExec := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
					Model:   "gemini-4-hydrogen-xhigh",
					Payload: payload,
				}, cliproxyexecutor.Options{
					SourceFormat:   sdktranslator.FormatGemini,
					ResponseFormat: sdktranslator.FormatGemini,
					Metadata:       map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: "sess-bridge-1"},
				})
				if errExec != nil {
					t.Fatalf("非流式 Execute 走上游 SSE 无 finishReason 工具流时不应失败: %v", errExec)
				}
				if len(resp.Payload) == 0 {
					t.Fatal("非流式 Execute 响应为空")
				}
				if capturedPath != antigravityStreamPath || capturedQuery != "alt=sse" {
					t.Fatalf("普通文本非流式推理应走上游 SSE: got=%s?%s", capturedPath, capturedQuery)
				}
				wantUA := "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)"
				if capturedUA != wantUA || misc.AntigravityUserAgent() != wantUA {
					t.Fatalf("UA 未对齐官方 1.5.13 consumer: req=%q misc=%q", capturedUA, misc.AntigravityUserAgent())
				}
				if capturedProxyKey != "" {
					t.Fatalf("Worker 改写头 x-proxy-key 不应出现: %q", capturedProxyKey)
				}
				if got := ojson.GetBytes(capturedBody, "request.generationConfig.thinkingConfig.thinkingLevel").String(); got != "EXTRA_HIGH" {
					t.Fatalf("thinkingLevel 应规范为大写 EXTRA_HIGH，got=%q body=%s", got, capturedBody)
				}
				if got := ojson.GetBytes(capturedBody, "request.sessionId").String(); got == "" || got == "-3750763034362895579" {
					t.Fatalf("有会话时应发送分会话稳定 sessionId，got=%q", got)
				}
				if !ojson.GetBytes(capturedBody, "request.labels.cascade_id").Exists() || !ojson.GetBytes(capturedBody, "request.labels.trajectory_id").Exists() {
					t.Fatalf("缺少 request.labels 轨迹字段: %s", capturedBody)
				}
			},
		},
		{
			name: "canonical_session_id_is_resolved_and_caller_isolated",
			run: func(t *testing.T) {
				exec := &AntigravityExecutor{}
				optsA := cliproxyexecutor.Options{
					Metadata: map[string]any{
						cliproxyexecutor.CanonicalSessionIDMetadataKey: "codex:0196a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
						cliproxyexecutor.CallerScopeMetadataKey:        "caller-a",
					},
				}
				optsB := cliproxyexecutor.Options{
					Metadata: map[string]any{
						cliproxyexecutor.CanonicalSessionIDMetadataKey: "codex:0196a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
						cliproxyexecutor.CallerScopeMetadataKey:        "caller-b",
					},
				}
				kA1 := exec.resolveLifecycleSessionKey(context.Background(), optsA, cliproxyexecutor.Request{})
				kA2 := exec.resolveLifecycleSessionKey(context.Background(), optsA, cliproxyexecutor.Request{})
				kB := exec.resolveLifecycleSessionKey(context.Background(), optsB, cliproxyexecutor.Request{})
				codexOpts := cliproxyexecutor.Options{
					Headers:  http.Header{"Session_id": {"codex-sess-001"}},
					Metadata: map[string]any{cliproxyexecutor.CallerScopeMetadataKey: "caller-a"},
				}
				if kc := exec.resolveLifecycleSessionKey(context.Background(), codexOpts, cliproxyexecutor.Request{}); kc == "" {
					t.Fatal("Codex 显式 Session_id 头应解析出稳定生命周期会话键")
				}
				if kA1 == "" || kA1 != kA2 {
					t.Fatalf("同调用方显式会话键应稳定非空: kA1=%q kA2=%q", kA1, kA2)
				}
				if kB == "" || kB == kA1 {
					t.Fatalf("不同调用方显式会话键必须隔离: kA1=%q kB=%q", kA1, kB)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
