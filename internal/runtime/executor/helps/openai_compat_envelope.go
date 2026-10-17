package helps

import "github.com/tidwall/gjson"

// UnwrapCompatResponseEnvelope flattens OpenAI-compatible gateways that return
// the whole completion inside a success envelope such as
// {"data": {"choices": [...], "usage": {...}}, "success": true}.
//
// The envelope carries no completion fields of its own, so leaving it in place
// hides the model and usage from accounting and from clients. Only a top-level
// object without choices/usage/model, holding a "data" object that does contain
// choices and a "success" marker, is unwrapped; every other body is returned
// unchanged.
func UnwrapCompatResponseEnvelope(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return body
	}
	if root.Get("choices").Exists() || root.Get("usage").Exists() || root.Get("model").Exists() {
		return body
	}
	if !root.Get("success").Exists() {
		return body
	}
	inner := root.Get("data")
	if !inner.IsObject() || !inner.Get("choices").Exists() {
		return body
	}
	return []byte(inner.Raw)
}
