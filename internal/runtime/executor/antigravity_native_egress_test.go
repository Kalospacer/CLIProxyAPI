package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type nativeEgressCapture struct {
	Body    []byte
	Headers http.Header
	Host    string
	Proto   string
}

func captureNativeEgress(t *testing.T, cfg *config.Config, format sdktranslator.Format, kind, model string, payload []byte, inbound http.Header) nativeEgressCapture {
	t.Helper()
	captured := make(chan nativeEgressCapture, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Error(errRead)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		captured <- nativeEgressCapture{Body: body, Headers: r.Header.Clone(), Host: r.Host, Proto: r.Proto}
		response := `{"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}}`
		switch kind {
		case "stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", response)
		case "count":
			_, _ = io.WriteString(w, `{"totalTokens":1}`)
		default:
			_, _ = io.WriteString(w, response)
		}
	}))
	defer server.Close()
	executor := NewAntigravityExecutor(cfg)
	auth := &cliproxyauth.Auth{ID: t.Name(), Provider: "antigravity", Attributes: map[string]string{"base_url": server.URL}, Metadata: map[string]any{
		"access_token": "offline-upstream", "project_id": "test-project", "expired": time.Now().Add(time.Hour).Format(time.RFC3339),
	}}
	req := cliproxyexecutor.Request{Model: model, Payload: payload}
	opts := cliproxyexecutor.Options{SourceFormat: format, Headers: inbound}
	switch kind {
	case "stream":
		result, errStream := executor.ExecuteStream(context.Background(), auth, req, opts)
		if errStream != nil {
			t.Fatal(errStream)
		}
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				t.Fatal(chunk.Err)
			}
		}
	case "count":
		if _, errCount := executor.CountTokens(context.Background(), auth, req, opts); errCount != nil {
			t.Fatal(errCount)
		}
	case "execute":
		if _, errExecute := executor.Execute(context.Background(), auth, req, opts); errExecute != nil {
			t.Fatal(errExecute)
		}
	}
	select {
	case wire := <-captured:
		return wire
	default:
		t.Fatal("upstream received no request")
		return nativeEgressCapture{}
	}
}

func TestAntigravityNativeEgressUnknownHarnessProtocols(t *testing.T) {
	const functionA = `{"name":"inspect_bytes","description":"Inspect a fixture","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}`
	const functionB = `{"name":"inspect_state","description":"Read current state","parameters":{"type":"object","properties":{}}}`
	responses := `{"model":"gemini-3.1-pro-preview","instructions":"Follow the workspace rules from this unknown client.","input":[{"role":"user","content":"Inspect the fixture."}],"max_output_tokens":2048,"tools":[{"type":"function",` + functionA[1:] + `,{"type":"function",` + functionB[1:] + `]}`
	chat := `{"model":"gemini-3.1-pro-preview","messages":[{"role":"system","content":"Follow the workspace rules from this unknown client."},{"role":"user","content":"Inspect the fixture."}],"max_completion_tokens":2048,"tools":[{"type":"function","function":` + functionA + `},{"type":"function","function":` + functionB + `}]}`
	inbound := http.Header{"User-Agent": {"never-seen-sdk/9"}, "X-New-Client": {"private"}, "X-Stainless-Retry-Count": {"4"}, "Authorization": {"Bearer downstream"}, "Connection": {"close"}}
	var outputs [][]byte
	for _, tc := range []struct {
		name   string
		format sdktranslator.Format
		body   string
	}{{"responses", sdktranslator.FormatOpenAIResponse, responses}, {"chat", sdktranslator.FormatOpenAI, chat}} {
		t.Run(tc.name, func(t *testing.T) {
			wire := captureNativeEgress(t, nil, tc.format, "stream", "gemini-3.1-pro-preview", []byte(tc.body), inbound)
			assertNativeEgressHeaders(t, wire, inbound)
			doc := ojson.NewDoc(wire.Body)
			if doc.Get("request.systemInstruction.role").String() != "user" || !strings.Contains(doc.Get("request.systemInstruction.parts.0.text").String(), "unknown client") {
				t.Fatalf("system instruction lost: %s", wire.Body)
			}
			if doc.Get("request.generationConfig.maxOutputTokens").Int() != 2048 {
				t.Fatalf("explicit output limit lost: %s", wire.Body)
			}
			tools := doc.Get("request.tools").Array()
			if len(tools) != 2 {
				t.Fatalf("tool wrapper count = %d, want 2: %s", len(tools), wire.Body)
			}
			for _, tool := range tools {
				declarations := tool.Get("functionDeclarations").Array()
				if len(declarations) != 1 {
					t.Fatalf("native wrapper contains %d declarations", len(declarations))
				}
				for _, path := range []string{"parameters.required", "parameters.properties.reason", "parameters.properties._"} {
					if declarations[0].Get(path).Exists() {
						t.Fatalf("Gemini tool gained %s: %s", path, declarations[0].Raw)
					}
				}
			}
			outputs = append(outputs, wire.Body)
		})
	}
	if len(outputs) == 2 {
		for _, path := range []string{"request.contents", "request.systemInstruction", "request.tools", "request.generationConfig"} {
			var a, b any
			a = ojson.GetBytes(outputs[0], path).Value()
			b = ojson.GetBytes(outputs[1], path).Value()
			if !reflect.DeepEqual(a, b) {
				t.Errorf("equivalent protocols emitted different %s: %v vs %v", path, a, b)
			}
		}
	}
}

func assertNativeEgressHeaders(t *testing.T, wire nativeEgressCapture, inbound http.Header) {
	t.Helper()
	if wire.Headers.Get("Authorization") != "Bearer offline-upstream" || wire.Headers.Get("User-Agent") != resolveUserAgent(nil) || wire.Headers.Get("Content-Type") != "application/json" {
		t.Fatal("upstream authentication or native headers differ")
	}
	for name := range inbound {
		if name != "User-Agent" && name != "Authorization" && wire.Headers.Get(name) != "" {
			t.Errorf("downstream header leaked: %s", name)
		}
	}
	if wire.Proto != "HTTP/1.1" {
		t.Errorf("wire protocol = %s, want HTTP/1.1", wire.Proto)
	}
}

func TestAntigravityNativeEgressExecuteAndCountHeaders(t *testing.T) {
	for _, kind := range []string{"execute", "count"} {
		t.Run(kind, func(t *testing.T) {
			inbound := http.Header{"User-Agent": {"unknown"}, "X-Unlisted-Sdk": {"private"}}
			wire := captureNativeEgress(t, nil, sdktranslator.FormatOpenAIResponse, kind, "gemini-3.1-pro-preview", []byte(`{"input":"hello"}`), inbound)
			assertNativeEgressHeaders(t, wire, inbound)
		})
	}
}

func TestAntigravityNativeEgressRawRequestDoesNotMutateCaller(t *testing.T) {
	var captured nativeEgressCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = nativeEgressCapture{Headers: r.Header.Clone(), Host: r.Host, Proto: r.Proto}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/generate", strings.NewReader(`{}`))
	req.Header = http.Header{"User-Agent": {"unknown-sdk"}, "Content-Type": {"application/json"}, "X-Unlisted-Sdk": {"private"}, "Connection": {"close"}}
	req.Host = "downstream.example"
	req.RequestURI = "/v1/chat/completions"
	req.Close = true
	req.Trailer = http.Header{"X-Unlisted-Sdk-Trailer": {"private"}}
	originalHeaders := req.Header.Clone()
	auth := &cliproxyauth.Auth{ID: t.Name(), Provider: "antigravity", Metadata: map[string]any{"access_token": "offline-upstream", "expired": time.Now().Add(time.Hour).Format(time.RFC3339)}}
	resp, errDo := NewAntigravityExecutor(nil).HttpRequest(context.Background(), auth, req)
	if errDo != nil {
		t.Fatal(errDo)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	assertNativeEgressHeaders(t, captured, http.Header{"X-Unlisted-Sdk": {"private"}, "Connection": {"close"}, "Trailer": {"X-Unlisted-Sdk-Trailer"}})
	if !reflect.DeepEqual(req.Header, originalHeaders) || req.Host != "downstream.example" || !req.Close || req.RequestURI != "/v1/chat/completions" || req.Trailer.Get("X-Unlisted-Sdk-Trailer") != "private" {
		t.Fatal("forwarding mutated the caller's request")
	}
	if captured.Host != strings.TrimPrefix(server.URL, "http://") {
		t.Errorf("inbound Host survived: %s", captured.Host)
	}
}

func TestAntigravityNativeEgressPayloadOverridesRemainFinal(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "gemini-*"}}, Params: map[string]any{
		"systemInstruction.role": "system", "generationConfig.maxOutputTokens": 7,
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "operator_a"}, map[string]any{"name": "operator_b"}}}},
	}}}}}
	wire := captureNativeEgress(t, cfg, sdktranslator.FormatOpenAIResponse, "stream", "gemini-3.1-pro-preview", []byte(`{"instructions":"keep","input":"hello"}`), nil)
	doc := ojson.NewDoc(wire.Body)
	if doc.Get("request.systemInstruction.role").String() != "system" || doc.Get("request.generationConfig.maxOutputTokens").Int() != 7 || len(doc.Get("request.tools").Array()) != 1 || len(doc.Get("request.tools.0.functionDeclarations").Array()) != 2 {
		t.Fatalf("native normalization overrode explicit payload rules: %s", wire.Body)
	}
}

func TestAntigravityNativeDefaultsDoNotGuessOtherModels(t *testing.T) {
	for _, model := range []string{"gemini-2.5-pro", "gemini-3.1-pro-low", "gemini-pro-orchestrator", "unknown-model"} {
		body := buildRequestBodyFromRawPayload(t, model, []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}}`))
		request := body["request"].(map[string]any)
		if _, exists := request["generationConfig"]; exists {
			t.Errorf("model %s received an unmeasured native default: %v", model, request["generationConfig"])
		}
	}
}
