package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// captureAntigravityWireBody 走完整 ExecuteStream 链路，返回真实出站请求体。
func captureAntigravityWireBody(t *testing.T, cfg *config.Config, model string, payload []byte, sessionMetadata ...map[string]any) []byte {
	t.Helper()
	captured := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Error(errRead)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		captured <- body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"{}\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}}\n\n")
	}))
	t.Cleanup(server.Close)

	executor := NewAntigravityExecutor(cfg)
	auth := &cliproxyauth.Auth{
		ID:         t.Name(),
		Provider:   "antigravity",
		Attributes: map[string]string{"base_url": server.URL},
		Metadata: map[string]any{
			"access_token": "token",
			"project_id":   "project-test",
			"expired":      time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	options := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatAntigravity}
	if len(sessionMetadata) > 0 {
		options.Metadata = sessionMetadata[0]
	}
	result, errStream := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{Model: model, Payload: payload}, options)
	if errStream != nil {
		t.Fatal(errStream)
	}
	for range result.Chunks {
	}
	select {
	case body := <-captured:
		return body
	default:
		t.Fatal("upstream request was not captured")
		return nil
	}
}
