package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// Exercise the v8 GET/PUT contract used by the restored frontend without
// touching the legacy TypeSafe management endpoint.
func TestConfigV8TypeSafeFrontendRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := `# Keep TypeSafe configuration
port: 8317
api-keys: [fixture-client]
plugins:
  enabled: false
  configs:
    fixture:
      opaque: preserved
typesafe-api-key:
  - api-key: fixture-parent
    base-url: https://api.typesafe.ai
    models: [{name: jev-fixture, alias: jev}]
    api-key-entries:
      - api-key: fixture-a
        weight: 3
        proxy-url: direct
      - api-key: fixture-b
        weight: 4
codex-api-key:
  - api-key: fixture-codex
    base-url: https://codex.example.invalid
`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{cfg: cfg, configFilePath: path}
	reloads := make(chan *config.Config, 8)
	h.SetConfigReloadHook(func(_ context.Context, next *config.Config) { reloads <- next })
	router := gin.New()
	router.GET("/v8/management/config", h.ConfigV8)
	router.GET("/v8/management/config/*path", h.ConfigV8)
	router.PUT("/v8/management/config/*path", h.ConfigV8)
	router.DELETE("/v8/management/config/*path", h.ConfigV8)
	request := func(method, url string, body any, want int) []byte {
		t.Helper()
		var data []byte
		if body != nil {
			var errEncode error
			data, errEncode = json.Marshal(body)
			if errEncode != nil {
				t.Fatal(errEncode)
			}
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, url, strings.NewReader(string(data))))
		if rec.Code != want {
			t.Fatalf("%s %s: status=%d body=%s", method, url, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	readGroups := func() []map[string]any {
		t.Helper()
		var groups []map[string]any
		if err := json.Unmarshal(request(http.MethodGet, "/v8/management/config/api-keys/typesafe", nil, 200), &groups); err != nil {
			t.Fatal(err)
		}
		return groups
	}
	groups := readGroups()
	if len(groups) != 1 {
		t.Fatal("TypeSafe legacy entry did not reach v8 groups")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != legacy {
		t.Fatal("GET must not migrate the saved file")
	}
	// The frontend submits the original primary key when the password field is
	// left blank; entries are reordered and rotated through their sourceIndex.
	keys := groups[0]["keys"].([]any)
	parent := keys[0].(map[string]any)
	entries := parent["api-key-entries"].([]any)
	rotated := entries[1].(map[string]any)
	rotated["api-key"] = "fixture-b-rotated"
	rotated["weight"] = float64(6)
	parent["api-key-entries"] = []any{rotated, entries[0]}
	request(http.MethodPut, "/v8/management/config/api-keys/typesafe", groups, 200)
	loaded, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.TypeSafeKey) != 1 {
		t.Fatal("missing runtime TypeSafe key")
	}
	key := loaded.TypeSafeKey[0]
	if key.APIKey != "fixture-parent" || len(key.APIKeyEntries) != 2 || key.APIKeyEntries[0].APIKey != "fixture-b-rotated" || key.APIKeyEntries[1].APIKey != "fixture-a" || key.APIKeyEntries[1].ProxyURL != "direct" {
		t.Fatalf("runtime bundled values changed: %#v", key)
	}
	if key.APIKeyEntries[0].Weight == nil || *key.APIKeyEntries[0].Weight != 6 {
		t.Fatal("rotated entry weight not retained")
	}
	if len(loaded.CodexKey) != 1 || loaded.CodexKey[0].APIKey != "fixture-codex" {
		t.Fatal("TypeSafe write changed Codex")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateV8Config(saved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "Keep TypeSafe configuration") || !strings.Contains(string(saved), "opaque: preserved") {
		t.Fatal("save dropped document comments or plugin fields")
	}
	if strings.Contains(string(saved), "typesafe-api-key:") {
		t.Fatal("write retained legacy TypeSafe spelling")
	}
	// A buffered channel captures the real management reload snapshot instead of
	// sleeping to guess that the async callback already ran.
	select {
	case next := <-reloads:
		if !reflect.DeepEqual(next.TypeSafeKey, loaded.TypeSafeKey) {
			t.Fatal("reload snapshot differs from saved TypeSafe config")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TypeSafe v8 write did not schedule runtime reload")
	}
	// A bundled-only row must survive sanitization even when the primary key is
	// empty.
	groups = readGroups()
	groups = append(groups, map[string]any{"name": "bundled-only", "base-url": "https://api.typesafe.ai", "keys": []any{map[string]any{"api-key": "", "api-key-entries": []any{map[string]any{"api-key": "fixture-only", "weight": float64(9)}}}}})
	request(http.MethodPut, "/v8/management/config/api-keys/typesafe", groups, 200)
	loaded, err = config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.TypeSafeKey) != 2 || loaded.TypeSafeKey[1].APIKey != "" || len(loaded.TypeSafeKey[1].APIKeyEntries) != 1 || loaded.TypeSafeKey[1].APIKeyEntries[0].APIKey != "fixture-only" {
		t.Fatal("bundled-only TypeSafe credential was removed")
	}
	// Frontend deletes replace the whole group list; empty groups must drop the
	// runtime credentials.
	request(http.MethodPut, "/v8/management/config/api-keys/typesafe", []any{}, 200)
	loaded, err = config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.TypeSafeKey) != 0 || len(loaded.CodexKey) != 1 {
		t.Fatal("clearing TypeSafe did not preserve other providers")
	}
	request(http.MethodDelete, "/v8/management/config/api-keys/typesafe", nil, 200)
	request(http.MethodGet, "/v8/management/config/api-keys/typesafe", nil, 404)
}

// An invalid entry weight in a v8 write must be rejected atomically, without
// mutating the on-disk config or the runtime snapshot first.
func TestConfigV8TypeSafeRejectsInvalidBundledWeights(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, weight := range []string{"1.5", "1000001", `"7"`} {
		t.Run(weight, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			raw := []byte("port: 8317\ntypesafe-api-key:\n  - api-key: fixture-parent\n    base-url: https://api.typesafe.ai\n")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{cfg: cfg, configFilePath: path}
			router := gin.New()
			router.PUT("/v8/management/config/*path", h.ConfigV8)
			body := `[{"name":"fixture","base-url":"https://api.typesafe.ai","keys":[{"api-key":"","api-key-entries":[{"api-key":"fixture-bundled","weight":` + weight + `}]}]}]`
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v8/management/config/api-keys/typesafe", strings.NewReader(body)))
			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("invalid weight must be rejected: %d %s", rec.Code, rec.Body.String())
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(raw) || h.cfg != cfg {
				t.Fatal("rejected TypeSafe update mutated disk or runtime config")
			}
		})
	}
}
