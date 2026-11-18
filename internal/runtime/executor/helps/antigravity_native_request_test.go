package helps

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestConfigureAntigravityRequestUsesOnlyOutboundHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://upstream.example/generate", nil)
	req.Header = http.Header{"User-Agent": {"unknown-sdk"}, "X-New-Sdk": {"private"}, "Connection": {"close"}}
	req.Host = "downstream.example"
	req.Close = true
	req.RequestURI = "/v1/responses"
	req.TransferEncoding = []string{"chunked"}
	req.Trailer = http.Header{"X-New-Sdk-Trailer": {"private"}}
	ConfigureAntigravityRequest(req, "Bearer upstream", "native-cli", "application/json", nil)
	want := http.Header{"Authorization": {"Bearer upstream"}, "User-Agent": {"native-cli"}, "Content-Type": {"application/json"}}
	if !reflect.DeepEqual(req.Header, want) {
		t.Fatalf("outbound headers = %v, want %v", req.Header, want)
	}
	if req.Host != "upstream.example" || req.Close || req.RequestURI != "" || req.Trailer != nil || req.TransferEncoding != nil {
		t.Fatalf("inbound framing survived: host=%q close=%v uri=%q trailer=%v transfer=%v", req.Host, req.Close, req.RequestURI, req.Trailer, req.TransferEncoding)
	}
}

func TestConfigureAntigravityRequestKeepsExplicitOverrides(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://upstream.example/generate", nil)
	ConfigureAntigravityRequest(req, "Bearer upstream", "native-cli", "application/json", map[string]string{
		"header:User-Agent": "operator-selected-agent", "header:X-Route": "private-route", "header:Host": "configured.example",
	})
	if req.Header.Get("User-Agent") != "operator-selected-agent" || req.Header.Get("X-Route") != "private-route" || req.Host != "configured.example" {
		t.Fatalf("explicit overrides lost: host=%q headers=%v", req.Host, req.Header)
	}
}

func TestNormalizeAntigravityAgentRequestPreservesContentAndTools(t *testing.T) {
	doc := ojson.NewDocString(`{"requestType":"agent","request":{
		"systemInstruction":{"parts":[{"text":"Custom instructions from an unknown harness"}]},
		"contents":[{"role":"model","parts":[{"functionCall":{"name":"probe","args":{"title":"keep","enum":["keep"]}}}]}],
		"tools":[{"functionDeclarations":[{"name":"probe","parameters":{"type":"object","properties":{}}},{"name":"list","parameters":{"type":"object","properties":{"scope":{"type":"string"}}}}]},
		{"googleSearch":{}},{"functionDeclarations":[{"name":"scoped-a"},{"name":"scoped-b"}],"futureScope":"preserve"}]
	}}`)
	contents := doc.Get("request.contents").Raw
	first := doc.Get("request.tools.0.functionDeclarations.0").Raw
	second := doc.Get("request.tools.0.functionDeclarations.1").Raw
	scoped := doc.Get("request.tools.2").Raw
	NormalizeAntigravityAgentRequestDoc(doc)
	if doc.Get("request.systemInstruction.role").String() != "user" || doc.Get("request.systemInstruction.parts.0.text").String() != "Custom instructions from an unknown harness" {
		t.Fatalf("system instruction changed unexpectedly: %s", doc.Bytes())
	}
	if doc.Get("request.tools.0.functionDeclarations.0").Raw != first || doc.Get("request.tools.1.functionDeclarations.0").Raw != second || doc.Get("request.tools.3").Raw != scoped {
		t.Fatalf("tool content or scoped wrapper changed: %s", doc.Bytes())
	}
	if doc.Get("request.contents").Raw != contents || !doc.Get("request.tools.2.googleSearch").Exists() {
		t.Fatalf("conversation or built-in tool changed: %s", doc.Bytes())
	}
	before := doc.String()
	NormalizeAntigravityAgentRequestDoc(doc)
	if doc.String() != before {
		t.Fatal("native organization is not idempotent")
	}
}

func TestNormalizeAntigravityAgentRequestKeepsExplicitRoleAndSpecialRequests(t *testing.T) {
	for _, requestType := range []string{"agent", "web_search", "image_gen"} {
		doc := ojson.NewDocString(`{"request":{"systemInstruction":{"role":"system","parts":[{"text":"keep"}]},"tools":[{"functionDeclarations":[{"name":"a"},{"name":"b"}]}]}}`)
		_ = doc.Set("requestType", requestType)
		NormalizeAntigravityAgentRequestDoc(doc)
		if doc.Get("request.systemInstruction.role").String() != "system" {
			t.Fatalf("explicit role overwritten for %s", requestType)
		}
		if requestType != "agent" && len(doc.Get("request.tools").Array()) != 1 {
			t.Fatalf("special request %s was regrouped", requestType)
		}
	}
}
