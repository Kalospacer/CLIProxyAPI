package util

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Google endpoint rewriting for deployments that cannot reach Google endpoints
// directly. When enabled, HTTP requests aimed at Google API hosts are
// transparently rerouted through an HTTP reverse-proxy worker:
//
//	https://<google-host><path>  ->  <GOOGLE_PROXY_BASE>/<google-host><path>
//
// The worker forwards the request to the real endpoint and streams the
// response back, so OAuth token exchanges and streaming API calls both work.
//
// Configuration (environment variables):
//   - GOOGLE_PROXY_BASE: reverse-proxy base URL, e.g. "https://proxy.example".
//     The value "off"/"none"/"" explicitly disables rewriting.
//   - GOOGLE_PROXY_KEY: shared key sent as the "x-proxy-key" header.
//
// Defaults are baked in for private deployments; set GOOGLE_PROXY_BASE=off to
// restore direct connectivity.
const (
	defaultGoogleProxyBase = "https://proxy.example.com"
	defaultGoogleProxyKey  = "replace-with-your-proxy-key"
	googleProxyKeyHeader   = "x-proxy-key"
)

var (
	googleProxyBase = resolveGoogleProxyBase()
	googleProxyURL  = mustParseGoogleProxyBase(googleProxyBase)
	googleProxyAuth = strings.TrimSpace(os.Getenv("GOOGLE_PROXY_KEY"))
)

func resolveGoogleProxyBase() string {
	raw, ok := os.LookupEnv("GOOGLE_PROXY_BASE")
	if !ok {
		return defaultGoogleProxyBase
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "none":
		return ""
	default:
		return strings.TrimRight(strings.TrimSpace(raw), "/")
	}
}

func mustParseGoogleProxyBase(base string) *url.URL {
	if base == "" {
		return nil
	}
	parsed, errParse := url.Parse(base)
	if errParse != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	return parsed
}

func googleProxyKey() string {
	if googleProxyAuth != "" {
		return googleProxyAuth
	}
	return defaultGoogleProxyKey
}

// isGoogleEndpointHost reports whether the given host is a Google API endpoint
// whose traffic should flow through the reverse proxy.
func isGoogleEndpointHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	switch h {
	case "googleapis.com", "google.com":
		return true
	}
	return strings.HasSuffix(h, ".googleapis.com") || strings.HasSuffix(h, ".google.com")
}

type googleRewriteTransport struct {
	base http.RoundTripper
}

func (t *googleRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if googleProxyURL == nil || req.URL == nil || !isGoogleEndpointHost(req.URL.Hostname()) {
		return base.RoundTrip(req)
	}

	r2 := req.Clone(req.Context())
	r2.URL.Scheme = googleProxyURL.Scheme
	r2.URL.Host = googleProxyURL.Host
	prefix := googleProxyURL.Path
	if prefix == "" || prefix == "." {
		prefix = ""
	}
	r2.URL.Path = prefix + "/" + req.URL.Host + req.URL.Path
	r2.URL.RawPath = ""
	r2.Host = "" // let net/http derive the Host header from the rewritten URL
	if key := googleProxyKey(); key != "" {
		r2.Header.Set(googleProxyKeyHeader, key)
	}
	return base.RoundTrip(r2)
}

// WrapGoogleRewrite installs the Google endpoint rewrite transport on top of
// the provided RoundTripper. A nil input yields nil so callers can keep their
// nil-transport fallbacks intact.
func WrapGoogleRewrite(rt http.RoundTripper) http.RoundTripper {
	if rt == nil || googleProxyURL == nil {
		return rt
	}
	if _, ok := rt.(*googleRewriteTransport); ok {
		return rt // already wrapped
	}
	return &googleRewriteTransport{base: rt}
}

// UnwrapGoogleRewrite strips any Google endpoint rewrite layers from the
// provided RoundTripper chain, returning the underlying transport.
func UnwrapGoogleRewrite(rt http.RoundTripper) http.RoundTripper {
	for {
		inner, ok := rt.(*googleRewriteTransport)
		if !ok || inner == nil {
			return rt
		}
		rt = inner.base
	}
}

// WrapGoogleRewriteClient installs the Google endpoint rewrite transport on
// the provided client when rewriting is enabled. Unlike WrapGoogleRewrite, a
// nil client Transport is acceptable: the wrapper falls back to
// http.DefaultTransport per request, so rewriting applies even when no proxy
// transport was configured.
func WrapGoogleRewriteClient(httpClient *http.Client) *http.Client {
	if httpClient == nil || googleProxyURL == nil {
		return httpClient
	}
	if _, ok := httpClient.Transport.(*googleRewriteTransport); ok {
		return httpClient // already wrapped
	}
	httpClient.Transport = &googleRewriteTransport{base: httpClient.Transport}
	return httpClient
}
