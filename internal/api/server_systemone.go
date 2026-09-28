package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// typeSafeDefaultBaseURL is used when the credential does not carry a base_url attribute.
const typeSafeDefaultBaseURL = "https://api.typesafe.ai"

// systemOneMaxAttempts caps the credential-rotation loop for the jev evaluation
// passthrough, independent of the (potentially large) retry configuration.
const systemOneMaxAttempts = 16

// systemOneHandler proxies the TypeSafe AI jev evaluation protocol
// (POST /v1/systemone, body {state, model, questions}) with credential pooling.
//
// Unlike chat endpoints, this payload is already in the upstream's native format
// and is forwarded verbatim. The handler rotates credentials on 401/429/5xx
// (marking conductor cooldowns so the scheduler skips failing keys), while
// request-scoped 4xx errors (e.g. 422 validation failures) are returned to the
// client immediately without cooling the credential.
func (s *Server) systemOneHandler(c *gin.Context) {
	if s == nil || s.handlers == nil || s.handlers.AuthManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TypeSafe auth manager unavailable"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 16<<20))
	if err != nil {
		c.JSON(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadRequest), gin.H{"error": "Failed to read systemone request"})
		return
	}

	var routing struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &routing)
	selectionModel := strings.TrimSpace(routing.Model)
	if selectionModel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	selectionHeaders := c.Request.Header.Clone()
	ctx := context.WithValue(c.Request.Context(), "gin", c)
	ctx = handlers.EnrichContextWithSessionHierarchy(ctx, selectionHeaders, body, nil)

	if s.handlers.AuthManager.HomeEnabled() {
		s.systemOneHomeAttempt(ctx, c, body, selectionHeaders, selectionModel)
		return
	}
	s.systemOneLegacyAttempts(ctx, c, body, selectionHeaders, selectionModel)
}

// systemOneBuildUpstreamURL resolves the upstream systemone endpoint for a credential.
func systemOneBuildUpstreamURL(current *auth.Auth) string {
	baseURL := typeSafeDefaultBaseURL
	if current != nil && current.Attributes != nil {
		if b := strings.TrimSpace(current.Attributes["base_url"]); b != "" {
			baseURL = b
		}
	}
	return strings.TrimRight(baseURL, "/") + "/v1/systemone"
}

// systemOneRetryAfter extracts the upstream Retry-After hint for 429 responses.
func systemOneRetryAfter(resp *http.Response) *time.Duration {
	if resp == nil {
		return nil
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	if secs, errParse := strconv.Atoi(raw); errParse == nil && secs >= 0 {
		d := time.Duration(secs) * time.Second
		return &d
	}
	if at, errParse := http.ParseTime(raw); errParse == nil {
		if d := time.Until(at); d > 0 {
			return &d
		}
	}
	return nil
}

// systemOneAttemptResult classifies one upstream attempt outcome.
type systemOneAttemptResult int

const (
	systemOneOutcomeSuccess       systemOneAttemptResult = iota
	systemOneOutcomeRotate                               // credential-level failure: cool and rotate
	systemOneOutcomeRequestScoped                        // client/request fault: return as-is, no cooldown
)

func systemOneClassifyStatus(status int) systemOneAttemptResult {
	switch {
	case status >= 200 && status < 300:
		return systemOneOutcomeSuccess
	case status == http.StatusUnauthorized:
		return systemOneOutcomeRotate
	case status == http.StatusTooManyRequests:
		return systemOneOutcomeRotate
	case status == 529: // TypeSafe "Overloaded"
		return systemOneOutcomeRotate
	case status >= 500:
		return systemOneOutcomeRotate
	default:
		// 4xx other than 401/429 (notably 422 validation) is request-scoped.
		return systemOneOutcomeRequestScoped
	}
}

// systemOnePerKeyRetry reads the per-credential request-retry override from metadata.
func systemOnePerKeyRetry(current *auth.Auth) (int, bool) {
	if current == nil || current.Metadata == nil {
		return 0, false
	}
	if v, ok := current.Metadata["request_retry"]; ok {
		switch value := v.(type) {
		case int:
			return value, true
		case int64:
			return int(value), true
		case float64:
			return int(value), true
		}
	}
	return 0, false
}

// systemOneLegacyAttempts runs the credential-rotation loop for the legacy
// (non-Home) dispatch mode.
func (s *Server) systemOneLegacyAttempts(ctx context.Context, c *gin.Context, body []byte, selectionHeaders http.Header, selectionModel string) {
	manager := s.handlers.AuthManager
	requestRetry := 3
	maxRetryCredentials := 0
	if s.cfg != nil {
		requestRetry = s.cfg.RequestRetry
		maxRetryCredentials = s.cfg.MaxRetryCredentials
	}
	if requestRetry < 0 {
		requestRetry = 0
	}
	// Attempt budget: rounds after the first, each round may try up to
	// maxRetryCredentials distinct credentials (0 means no explicit cap).
	perRound := maxRetryCredentials
	if perRound <= 0 {
		perRound = 4
	}
	maxAttempts := (requestRetry + 1) * perRound
	if maxAttempts > systemOneMaxAttempts {
		maxAttempts = systemOneMaxAttempts
	}

	selectionOpts := coreexecutor.Options{Headers: selectionHeaders, OriginalRequest: body}
	tried := make(map[string]struct{})
	var lastStatus int
	var lastBody []byte
	var lastHeader http.Header

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if errCtx := ctx.Err(); errCtx != nil {
			c.JSON(clienterror.HTTPStatusFromErrorOr(errCtx, http.StatusRequestTimeout), gin.H{"error": errCtx.Error()})
			return
		}

		selected, errSelect := manager.SelectAuthWithCredentialPolicy(ctx, "typesafe", selectionModel, auth.CredentialPolicyTypeSafeSystemOneV1, selectionOpts)
		if errSelect != nil {
			break
		}
		if selected == nil {
			break
		}
		if _, used := tried[selected.ID]; used {
			break
		}
		tried[selected.ID] = struct{}{}
		logging.SetGinCPATraceID(c, selected.EnsureIndex())

		resp, errDo := s.systemOnePerformRequest(ctx, selected, body, c)
		if errDo != nil {
			// Transport-level failure: transient, no credential cooldown, try next key.
			manager.MarkResult(ctx, auth.Result{
				AuthID:   selected.ID,
				Provider: "typesafe",
				Model:    selectionModel,
				Success:  false,
				Error: &auth.Error{
					Code:      auth.ErrorCodeTransientTransport,
					Message:   errDo.Error(),
					Retryable: true,
				},
			})
			lastStatus = http.StatusBadGateway
			lastBody = nil
			lastHeader = nil
			log.WithError(errDo).Warn("typesafe systemone: transport error, rotating credential")
			continue
		}

		upstreamBody, errRead := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		headerCopy := resp.Header.Clone()
		statusCode := resp.StatusCode
		_ = resp.Body.Close()
		helps.RecordAPIResponseMetadata(ctx, s.cfg, statusCode, headerCopy.Clone())
		helps.AppendAPIResponseChunk(ctx, s.cfg, upstreamBody)
		if errRead != nil {
			manager.MarkResult(ctx, auth.Result{
				AuthID:   selected.ID,
				Provider: "typesafe",
				Model:    selectionModel,
				Success:  false,
				Error: &auth.Error{
					Code:      auth.ErrorCodeTransientTransport,
					Message:   "failed to read upstream response",
					Retryable: true,
				},
			})
			lastStatus = http.StatusBadGateway
			lastBody = nil
			lastHeader = nil
			continue
		}

		outcome := systemOneClassifyStatus(statusCode)
		switch outcome {
		case systemOneOutcomeSuccess:
			manager.MarkResult(ctx, auth.Result{
				AuthID:   selected.ID,
				Provider: "typesafe",
				Model:    selectionModel,
				Success:  true,
			})
			s.systemOneWriteUpstreamResponse(c, statusCode, headerCopy, upstreamBody)
			return
		case systemOneOutcomeRotate:
			manager.MarkResult(ctx, auth.Result{
				AuthID:     selected.ID,
				Provider:   "typesafe",
				Model:      selectionModel,
				Success:    false,
				RetryAfter: systemOneRetryAfter(resp),
				Error: &auth.Error{
					Message:    "typesafe systemone upstream error",
					Retryable:  true,
					HTTPStatus: statusCode,
				},
			})
			log.WithField("status", statusCode).Warnf("typesafe systemone: upstream error, rotating credential: %s", logging.SafeDiagnosticForLog(string(upstreamBody)))
			lastStatus = statusCode
			lastBody = upstreamBody
			lastHeader = headerCopy
			continue
		default: // request-scoped
			manager.MarkResult(ctx, auth.Result{
				AuthID:   selected.ID,
				Provider: "typesafe",
				Model:    selectionModel,
				Success:  false,
				Error:    auth.NewRequestScopedError("typesafe systemone request rejected", statusCode),
			})
			s.systemOneWriteUpstreamResponse(c, statusCode, headerCopy, upstreamBody)
			return
		}
	}

	// All attempts exhausted: replay the last upstream failure if we have one,
	// otherwise surface the selection failure.
	if lastStatus != 0 {
		s.systemOneWriteUpstreamResponse(c, lastStatus, lastHeader, lastBody)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "TypeSafe auth unavailable"})
}

// systemOnePerformRequest builds and executes one upstream systemone call.
func (s *Server) systemOnePerformRequest(ctx context.Context, current *auth.Auth, body []byte, c *gin.Context) (*http.Response, error) {
	upstreamURL := systemOneBuildUpstreamURL(current)
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")

	manager := s.handlers.AuthManager
	req, errRequest := manager.NewHttpRequest(ctx, current, http.MethodPost, upstreamURL, body, headers)
	if errRequest != nil {
		return nil, errRequest
	}
	authType, authValue := current.AccountInfo()
	helps.RecordAPIRequest(ctx, s.cfg, helps.UpstreamRequestLog{
		URL:       upstreamURL,
		Method:    http.MethodPost,
		Headers:   req.Header.Clone(),
		Body:      body,
		Provider:  "typesafe",
		AuthID:    current.ID,
		AuthLabel: current.Label,
		AuthType:  authType,
		AuthValue: authValue,
	})
	return manager.HttpRequest(ctx, current, req)
}

// systemOneWriteUpstreamResponse replays an upstream status/headers/body to the client.
func (s *Server) systemOneWriteUpstreamResponse(c *gin.Context, status int, header http.Header, body []byte) {
	if contentType := header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	} else {
		c.Header("Content-Type", "application/json")
	}
	if retryAfter := header.Get("Retry-After"); retryAfter != "" && status == http.StatusTooManyRequests {
		c.Header("Retry-After", retryAfter)
	}
	if status == 0 {
		status = http.StatusBadGateway
	}
	if len(body) == 0 {
		body = []byte(`{"error":"typesafe systemone upstream unavailable"}`)
	}
	c.Status(status)
	_, _ = c.Writer.Write(body)
}

var errSystemOneNoAuth = errors.New("typesafe auth unavailable")

// systemOneHomeAttempt performs a single Home-dispatch attempt, mirroring the
// codex alpha search endpoint behavior for Home-enabled deployments.
func (s *Server) systemOneHomeAttempt(ctx context.Context, c *gin.Context, body []byte, selectionHeaders http.Header, selectionModel string) {
	manager := s.handlers.AuthManager
	selectionOpts := coreexecutor.Options{Headers: selectionHeaders, OriginalRequest: body}
	selection, err := manager.SelectHomeAuthWithCredentialPolicy(ctx, "typesafe", selectionModel, auth.CredentialPolicyTypeSafeSystemOneV1, selectionOpts)
	if err != nil {
		status := clienterror.HTTPStatusFromErrorOr(err, http.StatusServiceUnavailable)
		for _, value := range auth.SafeResponseHeaders(err).Values("Retry-After") {
			c.Writer.Header().Add("Retry-After", value)
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if selection == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errSystemOneNoAuth.Error()})
		return
	}
	selected := selection.CloneAuth()
	if selected == nil {
		selection.End("missing_auth")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errSystemOneNoAuth.Error()})
		return
	}

	attemptCtx, release, errBind := homeSelectionAttemptContext(ctx, selection)
	if errBind != nil {
		selection.End("attempt_bind_failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errBind.Error()})
		return
	}
	ctx = attemptCtx
	defer release()
	logging.SetGinCPATraceID(c, selected.EnsureIndex())

	resp, err := s.systemOnePerformRequest(ctx, selected, body, c)
	if err != nil {
		selection.End("request_failed")
		helps.RecordAPIResponseError(ctx, s.cfg, err)
		c.JSON(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadGateway), gin.H{"error": err.Error()})
		return
	}
	closeResponseBody := func() error { return resp.Body.Close() }
	if errBind := selection.Bind(closeResponseBody); errBind != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			manager.ReportHomeUnauthorized(ctx, selected, "typesafe", selectionModel)
		}
		selection.End("response_bind_failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errBind.Error()})
		return
	}
	defer selection.End("response_closed")

	upstreamBody, errRead := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	helps.RecordAPIResponseMetadata(ctx, s.cfg, resp.StatusCode, resp.Header.Clone())
	helps.AppendAPIResponseChunk(ctx, s.cfg, upstreamBody)
	if errRead != nil {
		helps.RecordAPIResponseError(ctx, s.cfg, errRead)
		c.JSON(clienterror.HTTPStatusFromErrorOr(errRead, http.StatusBadGateway), gin.H{"error": "Failed to read TypeSafe response"})
		return
	}
	if resp.StatusCode == http.StatusUnauthorized {
		manager.ReportHomeUnauthorized(ctx, selected, "typesafe", selectionModel, upstreamBody)
	}
	s.systemOneWriteUpstreamResponse(c, resp.StatusCode, resp.Header, upstreamBody)
}
