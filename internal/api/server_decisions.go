package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

const (
	openAIDecisionsDefaultBaseURL = "https://api.openai.com"
	geminiDecisionsDefaultBaseURL = "https://generativelanguage.googleapis.com"
	decisionsMaxAttempts         = 16
)

// decisionProtocol describes one native decision API. Decision models are not
// chat models, so credential selection deliberately bypasses the model catalog.
type decisionProtocol struct {
	name             string
	provider         string
	policy           string
	defaultBaseURL   string
	versionedPath    string
	unversionedPath  string
	unavailableError string
	classifyStatus   func(int, []byte) systemOneAttemptResult
}

var (
	openAIDecisionsProtocol = decisionProtocol{
		name:             "openai decisions",
		provider:         "codex",
		policy:           auth.CredentialPolicyOpenAIDecisionsV1,
		defaultBaseURL:   openAIDecisionsDefaultBaseURL,
		versionedPath:    "/v1/decisions",
		unversionedPath:  "/decisions",
		unavailableError: "OpenAI Decisions auth unavailable",
		classifyStatus:   decisionsClassifyStatus,
	}
	geminiDecisionsProtocol = decisionProtocol{
		name:             "gemini decisions",
		provider:         "gemini",
		policy:           auth.CredentialPolicyGeminiDecisionsV1,
		defaultBaseURL:   geminiDecisionsDefaultBaseURL,
		versionedPath:    "/v1beta/decisions",
		unversionedPath:  "/decisions",
		unavailableError: "Gemini Decisions auth unavailable",
		classifyStatus:   decisionsClassifyStatus,
	}
	systemOneProtocol = decisionProtocol{
		name:             "typesafe systemone",
		provider:         "typesafe",
		policy:           auth.CredentialPolicyTypeSafeSystemOneV1,
		defaultBaseURL:   typeSafeDefaultBaseURL,
		versionedPath:    "/v1/systemone",
		unversionedPath:  "/systemone",
		unavailableError: "TypeSafe auth unavailable",
		classifyStatus:   systemOneClassifyStatus,
	}
)

func decisionsClassifyStatus(status int, _ []byte) systemOneAttemptResult {
	switch {
	case status >= 200 && status < 300:
		return systemOneOutcomeSuccess
	case status == http.StatusUnauthorized, status == http.StatusForbidden, status == http.StatusTooManyRequests, status == 529:
		return systemOneOutcomeRotate
	case status >= 500:
		return systemOneOutcomeRotate
	default:
		return systemOneOutcomeRequestScoped
	}
}

func decisionUpstreamURL(protocol decisionProtocol, current *auth.Auth) string {
	baseURL := protocol.defaultBaseURL
	if current != nil && current.Attributes != nil {
		if configured := strings.TrimSpace(current.Attributes["base_url"]); configured != "" {
			baseURL = configured
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{protocol.versionedPath, protocol.unversionedPath} {
		if strings.HasSuffix(baseURL, suffix) {
			return baseURL
		}
	}
	if protocol.provider == "gemini" && strings.HasSuffix(baseURL, "/v1beta") {
		return baseURL + protocol.unversionedPath
	}
	if protocol.provider == "codex" && strings.HasSuffix(baseURL, "/v1") {
		return baseURL + protocol.unversionedPath
	}
	return baseURL + protocol.versionedPath
}

func (s *Server) decisionProtocolHandler(protocol decisionProtocol) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s == nil || s.handlers == nil || s.handlers.AuthManager == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": protocol.unavailableError})
			return
		}
		body, errRead := io.ReadAll(io.LimitReader(c.Request.Body, 16<<20))
		if errRead != nil {
			c.JSON(clienterror.HTTPStatusFromErrorOr(errRead, http.StatusBadRequest), gin.H{"error": "Failed to read " + protocol.name + " request"})
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
		headers := c.Request.Header.Clone()
		ctx := context.WithValue(c.Request.Context(), "gin", c)
		ctx = handlers.EnrichContextWithSessionHierarchy(ctx, headers, body, nil)
		if s.handlers.AuthManager.HomeEnabled() {
			s.decisionHomeAttempt(ctx, c, protocol, body, headers, selectionModel)
			return
		}
		s.decisionLegacyAttempts(ctx, c, protocol, body, headers, selectionModel)
	}
}

func (s *Server) decisionLegacyAttempts(ctx context.Context, c *gin.Context, protocol decisionProtocol, body []byte, headers http.Header, model string) {
	manager := s.handlers.AuthManager
	maxAttempts := decisionAttemptBudget(s.cfg.RequestRetry, s.cfg.MaxRetryCredentials)
	selectionOpts := coreexecutor.Options{Headers: headers, OriginalRequest: body}
	tried := make(map[string]struct{})
	var lastStatus int
	var lastBody []byte
	var lastHeader http.Header

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if errCtx := ctx.Err(); errCtx != nil {
			c.JSON(clienterror.HTTPStatusFromErrorOr(errCtx, http.StatusRequestTimeout), gin.H{"error": errCtx.Error()})
			return
		}
		selected, errSelect := manager.SelectAuthWithCredentialPolicy(ctx, protocol.provider, "", protocol.policy, selectionOpts)
		if errSelect != nil || selected == nil {
			break
		}
		if _, used := tried[selected.ID]; used {
			break
		}
		tried[selected.ID] = struct{}{}
		logging.SetGinCPATraceID(c, selected.EnsureIndex())

		resp, errDo := s.decisionPerformRequest(ctx, protocol, selected, body, c)
		if errDo != nil {
			manager.MarkResult(ctx, auth.Result{
				AuthID: selected.ID, Provider: protocol.provider, Model: model, Success: false,
				Error: &auth.Error{Code: auth.ErrorCodeTransientTransport, Message: errDo.Error(), Retryable: true},
			})
			lastStatus, lastBody, lastHeader = http.StatusBadGateway, nil, nil
			log.WithError(errDo).Warn(protocol.name + ": transport error, rotating credential")
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
				AuthID: selected.ID, Provider: protocol.provider, Model: model, Success: false,
				Error: &auth.Error{Code: auth.ErrorCodeTransientTransport, Message: "failed to read upstream response", Retryable: true},
			})
			lastStatus, lastBody, lastHeader = http.StatusBadGateway, nil, nil
			continue
		}

		switch protocol.classifyStatus(statusCode, upstreamBody) {
		case systemOneOutcomeSuccess:
			manager.MarkResult(ctx, auth.Result{AuthID: selected.ID, Provider: protocol.provider, Model: model, Success: true})
			decisionWriteUpstreamResponse(c, statusCode, headerCopy, upstreamBody)
			return
		case systemOneOutcomeRotate:
			manager.MarkResult(ctx, auth.Result{
				AuthID: selected.ID, Provider: protocol.provider, Model: model, Success: false, RetryAfter: systemOneRetryAfter(resp),
				Error: &auth.Error{Message: protocol.name + " upstream error", Retryable: true, HTTPStatus: statusCode},
			})
			log.WithField("status", statusCode).Warnf("%s: upstream error, rotating credential: %s", protocol.name, logging.SafeDiagnosticForLog(string(upstreamBody)))
			lastStatus, lastBody, lastHeader = statusCode, upstreamBody, headerCopy
			continue
		default:
			manager.MarkResult(ctx, auth.Result{
				AuthID: selected.ID, Provider: protocol.provider, Model: model, Success: false,
				Error: auth.NewRequestScopedError(protocol.name+" request rejected", statusCode),
			})
			decisionWriteUpstreamResponse(c, statusCode, headerCopy, upstreamBody)
			return
		}
	}

	if lastStatus != 0 {
		decisionWriteUpstreamResponse(c, lastStatus, lastHeader, lastBody)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": protocol.unavailableError})
}

func decisionAttemptBudget(requestRetry, maxRetryCredentials int) int {
	if requestRetry < 0 {
		requestRetry = 0
	}
	perRound := maxRetryCredentials
	if perRound <= 0 {
		perRound = 4
	}
	attempts := (requestRetry + 1) * perRound
	if attempts > decisionsMaxAttempts {
		return decisionsMaxAttempts
	}
	return attempts
}

func (s *Server) decisionPerformRequest(ctx context.Context, protocol decisionProtocol, current *auth.Auth, body []byte, c *gin.Context) (*http.Response, error) {
	upstreamURL := decisionUpstreamURL(protocol, current)
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
		URL: upstreamURL, Method: http.MethodPost, Headers: req.Header.Clone(), Body: body,
		Provider: protocol.provider, AuthID: current.ID, AuthLabel: current.Label, AuthType: authType, AuthValue: authValue,
	})
	return manager.HttpRequest(ctx, current, req)
}

func decisionWriteUpstreamResponse(c *gin.Context, status int, header http.Header, body []byte) {
	for name, values := range header {
		switch http.CanonicalHeaderKey(name) {
		case "Content-Length", "Connection", "Transfer-Encoding", "Trailer", "Upgrade":
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(name, value)
		}
	}
	if header.Get("Content-Type") == "" {
		c.Header("Content-Type", "application/json")
	}
	if status == 0 {
		status = http.StatusBadGateway
	}
	c.Status(status)
	_, _ = c.Writer.Write(body)
}

func (s *Server) decisionHomeAttempt(ctx context.Context, c *gin.Context, protocol decisionProtocol, body []byte, headers http.Header, model string) {
	manager := s.handlers.AuthManager
	selectionOpts := coreexecutor.Options{Headers: headers, OriginalRequest: body}
	selection, errSelect := manager.SelectHomeAuthWithCredentialPolicy(ctx, protocol.provider, "", protocol.policy, selectionOpts)
	if errSelect != nil {
		status := clienterror.HTTPStatusFromErrorOr(errSelect, http.StatusServiceUnavailable)
		for _, value := range auth.SafeResponseHeaders(errSelect).Values("Retry-After") {
			c.Writer.Header().Add("Retry-After", value)
		}
		c.JSON(status, gin.H{"error": errSelect.Error()})
		return
	}
	if selection == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": protocol.unavailableError})
		return
	}
	selected := selection.CloneAuth()
	if selected == nil {
		selection.End("missing_auth")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": protocol.unavailableError})
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

	resp, errDo := s.decisionPerformRequest(ctx, protocol, selected, body, c)
	if errDo != nil {
		selection.End("request_failed")
		helps.RecordAPIResponseError(ctx, s.cfg, errDo)
		c.JSON(clienterror.HTTPStatusFromErrorOr(errDo, http.StatusBadGateway), gin.H{"error": errDo.Error()})
		return
	}
	closeResponseBody := func() error { return resp.Body.Close() }
	if errBind := selection.Bind(closeResponseBody); errBind != nil {
		if resp.StatusCode == http.StatusUnauthorized {
			manager.ReportHomeUnauthorized(ctx, selected, protocol.provider, model)
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
		c.JSON(clienterror.HTTPStatusFromErrorOr(errRead, http.StatusBadGateway), gin.H{"error": "Failed to read " + protocol.name + " response"})
		return
	}
	if resp.StatusCode == http.StatusUnauthorized {
		manager.ReportHomeUnauthorized(ctx, selected, protocol.provider, model, upstreamBody)
	}
	decisionWriteUpstreamResponse(c, resp.StatusCode, resp.Header, upstreamBody)
}
