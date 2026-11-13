package executor

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// Decision provider identifiers re-exported from the auth package, which owns
// the canonical spelling. Each native decision protocol keeps its own provider
// so decision credentials never serve chat requests built from the ordinary
// Gemini or Codex credential groups.
const (
	DecisionOpenAIProvider = cliproxyauth.DecisionOpenAIProvider
	DecisionGeminiProvider = cliproxyauth.DecisionGeminiProvider
)

var _ cliproxyauth.ProviderExecutor = (*DecisionExecutor)(nil)

// DecisionExecutor sends native decision requests to the upstream selected by
// its own credential group. These endpoints are not chat APIs, so only the HTTP
// passthrough path is meaningful here.
type DecisionExecutor struct {
	cfg      *config.Config
	provider string
}

// NewDecisionExecutor constructs a decision executor bound to one protocol.
func NewDecisionExecutor(cfg *config.Config, provider string) *DecisionExecutor {
	return &DecisionExecutor{cfg: cfg, provider: provider}
}

// Identifier returns the provider identifier of this decision protocol.
func (e *DecisionExecutor) Identifier() string {
	if e == nil {
		return ""
	}
	return e.provider
}

// PrepareRequest injects the protocol-specific credential header.
func (e *DecisionExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	token := decisionAPIKey(auth)
	if e != nil && e.provider == DecisionGeminiProvider {
		if token != "" {
			req.Header.Set("x-goog-api-key", token)
		} else {
			req.Header.Del("x-goog-api-key")
		}
		req.Header.Del("Authorization")
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Del("Authorization")
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}

// HttpRequest injects the credential and executes the request via a proxy-aware client.
func (e *DecisionExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("decision executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	if decisionAPIKey(auth) == "" {
		return nil, &cliproxyauth.Error{
			Code:       "unauthorized",
			Message:    "decision executor: missing API key",
			HTTPStatus: http.StatusUnauthorized,
		}
	}
	httpReq := req.WithContext(ctx)
	if errPrepare := e.PrepareRequest(httpReq, auth); errPrepare != nil {
		return nil, errPrepare
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute is not supported: decision protocols are served by the dedicated
// passthrough handlers, not the chat execution pipeline.
func (e *DecisionExecutor) Execute(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &cliproxyauth.Error{
		Code:    "not_supported",
		Message: "decision executor: use the native /decisions endpoint for this protocol",
	}
}

// ExecuteStream is not supported: decision protocols have no streaming mode.
func (e *DecisionExecutor) ExecuteStream(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, &cliproxyauth.Error{
		Code:    "not_supported",
		Message: "decision executor: streaming is not supported by the decision protocols",
	}
}

// CountTokens is not supported: decision protocols report usage in the response.
func (e *DecisionExecutor) CountTokens(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &cliproxyauth.Error{
		Code:    "not_supported",
		Message: "decision executor: token counting is not supported by the decision protocols",
	}
}

// Refresh is a no-op: decision API keys are static bearer tokens.
func (e *DecisionExecutor) Refresh(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

func decisionAPIKey(a *cliproxyauth.Auth) string {
	if a == nil {
		return ""
	}
	if a.Attributes != nil {
		if k := strings.TrimSpace(a.Attributes["api_key"]); k != "" {
			return k
		}
	}
	if a.Metadata != nil {
		if k, ok := a.Metadata["api_key"].(string); ok {
			return strings.TrimSpace(k)
		}
	}
	return ""
}
