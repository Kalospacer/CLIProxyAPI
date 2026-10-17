package helps

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestUnwrapCompatResponseEnvelopeFlattensSuccessWrapper(t *testing.T) {
	body := []byte(`{"data":{"id":"gen-1","model":"xiaomi/mimo-v2.6-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":710,"completion_tokens":64,"total_tokens":774}},"success":true}`)

	got := UnwrapCompatResponseEnvelope(body)
	if strings.Contains(string(got), `"success"`) {
		t.Fatalf("envelope was not removed: %s", got)
	}
	if score := gjson.GetBytes(got, "model").String(); score != "xiaomi/mimo-v2.6-flash" {
		t.Fatalf("model = %q, want the inner completion model", score)
	}
	if score := gjson.GetBytes(got, "usage.prompt_tokens").String(); score != "710" {
		t.Fatalf("usage.prompt_tokens = %q, want 710", score)
	}
	if score := gjson.GetBytes(got, "choices.0.message.content").String(); score != "ok" {
		t.Fatalf("content = %q, want ok", score)
	}
}

func TestUnwrapCompatResponseEnvelopeKeepsPlainCompletion(t *testing.T) {
	body := []byte(`{"id":"gen-2","model":"xiaomi/mimo-v2.6-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	if got := string(UnwrapCompatResponseEnvelope(body)); got != string(body) {
		t.Fatalf("plain completion changed: %s", got)
	}
}

func TestUnwrapCompatResponseEnvelopeRequiresSuccessAndChoices(t *testing.T) {
	cases := map[string][]byte{
		"no success marker": []byte(`{"data":{"choices":[{"index":0}],"usage":{"prompt_tokens":1}}}`),
		"no choices":        []byte(`{"data":{"usage":{"prompt_tokens":1}},"success":true}`),
		"data is a string":  []byte(`{"data":"oops","success":true}`),
		"stream frames":     []byte("data: {\"choices\":[{\"index\":0}]}\n\ndata: [DONE]\n"),
		"empty":             {},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := string(UnwrapCompatResponseEnvelope(body)); got != string(body) {
				t.Fatalf("body changed: %s", got)
			}
		})
	}
}
