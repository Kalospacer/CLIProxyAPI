# Native decision APIs

CLIProxyAPI exposes three independent decision protocols. It forwards each request body and response unchanged; it does not translate JEV, OpenAI, and Gemini into one another. These APIs are not chat endpoints and do not support streaming.

| Provider | Downstream endpoint | Upstream endpoint | v8 credential family |
| --- | --- | --- | --- |
| TypeSafe JEV | `POST /v1/systemone` | `POST {base-url}/v1/systemone` | `api-keys.typesafe` |
| OpenAI Decisions | `POST /v1/decisions` or `POST /openai/v1/decisions` | `POST https://api.openai.com/v1/decisions` | `api-keys.decision-openai` |
| Gemini Decisions | `POST /v1beta/decisions` | `POST https://generativelanguage.googleapis.com/v1beta/decisions` | `api-keys.decision-gemini` |

Each decision protocol has its own v8 credential family (`api-keys.decision-openai` and `api-keys.decision-gemini`), isolated from ordinary Codex and Gemini chat credentials. Chat credentials never serve decision routes, and decision credentials do not serve chat completions. Credential selection bypasses the chat model catalog so new decision models pass through, while honoring wildcard `"*"` entries in `excluded-models` to disable a key.

Credentials rotate on transport failures, `401`, `403`, `429`, and `5xx`. TypeSafe retains its narrower 403 behavior: only `detail.error_type=authentication_error` rotates. Other 4xx responses, including Gemini invalid-argument `400`, return unchanged without cooling a credential. A `Retry-After` response is retained when the final upstream response is `429`.

Add credentials under the dedicated v8 families (or legacy `decision-openai-api-key` / `decision-gemini-api-key`):

```yaml
api-keys:
  decision-openai:
    - name: openai-decisions
      base-url: "https://api.openai.com"
      keys:
        - api-key: "sk-..."
  decision-gemini:
    - name: gemini-decisions
      base-url: "https://generativelanguage.googleapis.com"
      keys:
        - api-key: "AIza..."
```

An empty `base-url` selects the official upstream default. Use `https://generativelanguage.googleapis.com/v1beta` or `https://api.openai.com/v1` if preferred; the proxy does not append `/v1` or `/v1beta` twice.

## OpenAI example

```bash
curl -X POST "http://127.0.0.1:8317/v1/decisions" \
  -H "Authorization: Bearer CPA_CLIENT_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-6-luna-decision",
    "input": "I was charged twice for my order.",
    "questions": [{
      "type": "predicate",
      "name": "refund_requested",
      "instructions": "Does the customer ask for a refund?"
    }]
  }'
```

The OpenAI SDK can use `base_url="http://127.0.0.1:8317/v1"` and a CPA client API key, then call `client.decisions.create(...)` with the native request fields.

## Gemini example

```bash
curl -X POST "http://127.0.0.1:8317/v1beta/decisions" \
  -H "Authorization: Bearer CPA_CLIENT_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-decision-1",
    "input": "The blue mug I ordered on Monday arrived cracked on Wednesday.",
    "questions": {
      "is_damaged": {
        "type": "noul",
        "instructions": "Did the item arrive damaged?"
      }
    }
  }'
```

Gemini responses retain the upstream `answers` object and `usage`. As of November 2026, Google's documented AI Studio no-cost tier provides 200 requests/minute, 50,000 input tokens/minute, and 10,000 requests/day per project. This is a documentation quota, not an account-specific measurement. The model is billed only for input on paid tiers and produces zero output tokens.
