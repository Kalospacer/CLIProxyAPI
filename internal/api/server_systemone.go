package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// typeSafeDefaultBaseURL is used when the credential does not carry a base_url attribute.
const typeSafeDefaultBaseURL = "https://api.typesafe.ai"

// systemOneHandler preserves the original JEV endpoint entry point. The shared
// decisionProtocolHandler supplies the same credential pooling and passthrough.
func (s *Server) systemOneHandler(c *gin.Context) {
	s.decisionProtocolHandler(systemOneProtocol)(c)
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

// systemOneClassifyStatus keeps TypeSafe's special disabled-organization error
// distinct from other 403 responses, which belong to the request, not the key.
func systemOneClassifyStatus(status int, body []byte) systemOneAttemptResult {
	if status == http.StatusForbidden {
		var response struct {
			Detail struct {
				ErrorType string `json:"error_type"`
			} `json:"detail"`
		}
		if json.Unmarshal(body, &response) == nil && response.Detail.ErrorType == "authentication_error" {
			return systemOneOutcomeRotate
		}
	}
	switch {
	case status >= 200 && status < 300:
		return systemOneOutcomeSuccess
	case status == http.StatusUnauthorized, status == http.StatusTooManyRequests, status == 529:
		return systemOneOutcomeRotate
	case status >= 500:
		return systemOneOutcomeRotate
	default:
		return systemOneOutcomeRequestScoped
	}
}
