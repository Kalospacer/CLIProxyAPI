package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type decisionTestRoundTripper struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
}

func (rt *decisionTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, errRead := io.ReadAll(req.Body)
	if errRead != nil {
		return nil, errRead
	}
	rt.mu.Lock()
	rt.requests = append(rt.requests, req.Clone(req.Context()))
	rt.bodies = append(rt.bodies, body)
	call := len(rt.requests)
	rt.mu.Unlock()
	status := http.StatusOK
	responseBody := `{"answers":{"urgent":{"type":"noul","noul":0.96,"confidence":0.96}},"usage":{"total_output_tokens":0}}`
	if call == 1 {
		status = http.StatusUnauthorized
		responseBody = `{"error":{"message":"expired test key"}}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
		Request:    req,
	}, nil
}

func TestDecisionRoutesPreserveNativePayloadAndRotateAPIKeys(t *testing.T) {
	for _, test := range []struct {
		name         string
		route        string
		upstreamPath string
		provider     string
		model        string
	}{
		{name: "OpenAI", route: "/v1/decisions", upstreamPath: "/v1/decisions", provider: "codex", model: "gpt-6-luna"},
		{name: "OpenAI direct alias", route: "/openai/v1/decisions", upstreamPath: "/v1/decisions", provider: "codex", model: "gpt-6-luna"},
		{name: "Gemini", route: "/v1beta/decisions", upstreamPath: "/v1beta/decisions", provider: "gemini", model: "gemini-decision-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t)
			rt := &decisionTestRoundTripper{}
			manager := server.handlers.AuthManager
			var providerExecutor auth.ProviderExecutor
			if test.provider == "gemini" {
				providerExecutor = executor.NewGeminiExecutor(server.cfg)
			} else {
				providerExecutor = executor.NewOpenAICompatExecutor("codex", server.cfg)
			}
			manager.RegisterExecutor(providerExecutor)
			for _, suffix := range []string{"expired", "working"} {
				if _, errRegister := manager.Register(context.Background(), &auth.Auth{
					ID: test.provider + "-" + suffix, Provider: test.provider, Status: auth.StatusActive,
					Attributes: map[string]string{auth.AttributeAPIKey: "decision-test-" + suffix},
				}); errRegister != nil {
					t.Fatalf("register test credential: %v", errRegister)
				}
			}
			payload := []byte(`{"model":"` + test.model + `","input":"same input","questions":{"urgent":{"type":"noul","instructions":" urgent? "}},"unknown_native_field":{"keep":true}}`)
			recorder := httptest.NewRecorder()
			ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(rt))
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, test.route, bytes.NewReader(payload))
			request.Header.Set("Authorization", "Bearer test-key")
			request.Header.Set("Content-Type", "application/json")
			server.engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if len(rt.requests) != 2 {
				t.Fatalf("upstream calls = %d, want 2 (rotate expired key)", len(rt.requests))
			}
			for index, req := range rt.requests {
				if req.URL.Path != test.upstreamPath {
					t.Errorf("upstream path = %q, want %q", req.URL.Path, test.upstreamPath)
				}
				if !bytes.Equal(rt.bodies[index], payload) {
					t.Errorf("request body changed: got %s, want %s", rt.bodies[index], payload)
				}
			}
			if test.provider == "gemini" {
				if got := rt.requests[0].Header.Get("x-goog-api-key"); got != "decision-test-expired" {
					t.Errorf("Gemini auth = %q, want first key", got)
				}
				if got := rt.requests[1].Header.Get("x-goog-api-key"); got != "decision-test-working" {
					t.Errorf("rotated Gemini auth = %q, want second key", got)
				}
			} else {
				if got := rt.requests[0].Header.Get("Authorization"); got != "Bearer decision-test-expired" {
					t.Errorf("OpenAI auth = %q, want first key", got)
				}
				if got := rt.requests[1].Header.Get("Authorization"); got != "Bearer decision-test-working" {
					t.Errorf("rotated OpenAI auth = %q, want second key", got)
				}
			}
			if !strings.Contains(recorder.Body.String(), `"total_output_tokens":0`) {
				t.Errorf("upstream response was not preserved: %s", recorder.Body.String())
			}
		})
	}
}

func TestDecisionClassifyStatusDoesNotCoolInvalidRequests(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity} {
		if got := decisionsClassifyStatus(status, []byte(`{"error":{"code":"invalid_api_key"}}`)); got != systemOneOutcomeRequestScoped {
			t.Errorf("status %d classified as %v, want request-scoped", status, got)
		}
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		if got := decisionsClassifyStatus(status, nil); got != systemOneOutcomeRotate {
			t.Errorf("status %d classified as %v, want credential rotation", status, got)
		}
	}
}

func TestDecisionUpstreamURLAcceptsNativeBaseURLVersions(t *testing.T) {
	for _, test := range []struct {
		protocol decisionProtocol
		base     string
		want     string
	}{
		{openAIDecisionsProtocol, "", "https://api.openai.com/v1/decisions"},
		{openAIDecisionsProtocol, "https://api.openai.com/v1", "https://api.openai.com/v1/decisions"},
		{openAIDecisionsProtocol, "https://api.openai.com/v1/decisions", "https://api.openai.com/v1/decisions"},
		{geminiDecisionsProtocol, "", "https://generativelanguage.googleapis.com/v1beta/decisions"},
		{geminiDecisionsProtocol, "https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com/v1beta/decisions"},
	} {
		if got := decisionUpstreamURL(test.protocol, &auth.Auth{Attributes: map[string]string{"base_url": test.base}}); got != test.want {
			t.Errorf("decisionUpstreamURL(%q) = %q, want %q", test.base, got, test.want)
		}
	}
}
