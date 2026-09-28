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

var _ cliproxyauth.ProviderExecutor = (*TypeSafeExecutor)(nil)

// TypeSafeDefaultBaseURL is the default upstream base URL for TypeSafe AI.
const TypeSafeDefaultBaseURL = "https://api.typesafe.ai"

// TypeSafeExecutor implements cliproxyauth.ProviderExecutor for TypeSafe AI's
// jev evaluation protocol (POST {base}/v1/systemone). The protocol is not a chat
// API, so only the HTTP passthrough path is meaningful; Execute/ExecuteStream/
// CountTokens report not-supported.
type TypeSafeExecutor struct {
	cfg *config.Config
}

// NewTypeSafeExecutor constructs a new TypeSafe executor.
func NewTypeSafeExecutor(cfg *config.Config) *TypeSafeExecutor {
	return &TypeSafeExecutor{cfg: cfg}
}

// Identifier returns the provider identifier "typesafe".
func (e *TypeSafeExecutor) Identifier() string {
	return "typesafe"
}

// PrepareRequest injects the TypeSafe Bearer credential into the outgoing request.
func (e *TypeSafeExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	token := typeSafeAPIKey(auth)
	if token != "" {
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
func (e *TypeSafeExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("typesafe executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	if typeSafeAPIKey(auth) == "" {
		return nil, &cliproxyauth.Error{
			Code:       "unauthorized",
			Message:    "typesafe executor: missing API key",
			HTTPStatus: http.StatusUnauthorized,
		}
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute is not supported: the jev evaluation protocol is served through the
// dedicated /v1/systemone passthrough handler, not the chat execution pipeline.
func (e *TypeSafeExecutor) Execute(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &cliproxyauth.Error{
		Code:    "not_supported",
		Message: "typesafe executor: use POST /v1/systemone for the jev evaluation protocol",
	}
}

// ExecuteStream is not supported: the jev evaluation protocol has no streaming mode.
func (e *TypeSafeExecutor) ExecuteStream(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, &cliproxyauth.Error{
		Code:    "not_supported",
		Message: "typesafe executor: streaming is not supported by the jev evaluation protocol",
	}
}

// CountTokens is not supported: the jev evaluation protocol reports usage in its response.
func (e *TypeSafeExecutor) CountTokens(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &cliproxyauth.Error{
		Code:    "not_supported",
		Message: "typesafe executor: token counting is not supported by the jev evaluation protocol",
	}
}

// Refresh is a no-op: TypeSafe API keys are static bearer tokens.
func (e *TypeSafeExecutor) Refresh(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

func typeSafeAPIKey(a *cliproxyauth.Auth) string {
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
