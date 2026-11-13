package api

import (
	"net/http"
	"testing"
)

func TestSystemOneClassifyStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   systemOneAttemptResult
	}{
		{"success", http.StatusOK, `{}`, systemOneOutcomeSuccess},
		{"unauthorized", http.StatusUnauthorized, `{}`, systemOneOutcomeRotate},
		{"disabled organization", http.StatusForbidden, `{"detail":{"error_type":"authentication_error","message":"Your organization has been deactivated."}}`, systemOneOutcomeRotate},
		{"other forbidden", http.StatusForbidden, `{"detail":{"error_type":"permission_denied"}}`, systemOneOutcomeRequestScoped},
		{"malformed forbidden", http.StatusForbidden, `not json`, systemOneOutcomeRequestScoped},
		{"invalid request", http.StatusUnprocessableEntity, `{"error":{"error_type":"authentication_error"}}`, systemOneOutcomeRequestScoped},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := systemOneClassifyStatus(tc.status, []byte(tc.body)); got != tc.want {
				t.Fatalf("classify status %d: got %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}
