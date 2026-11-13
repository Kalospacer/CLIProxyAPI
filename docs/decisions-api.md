# Native decision APIs

CLIProxyAPI exposes three independent decision protocols. It forwards each request body and response unchanged; it does not translate JEV, OpenAI, and Gemini into one another. These APIs are not chat endpoints and do not support streaming.

| Provider | Downstream endpoint | Upstream endpoint | Eligible credentials |
| --- | --- | --- | --- |
| TypeSafe JEV | `POST /v1/systemone` | `POST {base-url}/v1/systemone` | `api-keys.typesafe` API keys |
| OpenAI Decisions | `POST /v1/decisions` or `POST /openai/v1/decisions` | `POST https://api.openai.com/v1/decisions` | `api-keys.codex` API keys using `https://api.openai.com` |
| Gemini Decisions | `POST /v1beta/decisions` | `POST https://generativelanguage.googleapis.com/v1beta/decisions` | `api-keys.gemini` API keys using `https://generativelanguage.googleapis.com` |

Decision credential selection bypasses the chat model catalog because `gpt-6-luna` and `gemini-decision-1` are specialized models. The request's `model` is still sent unchanged and recorded for failures. Only API keys for the official provider are eligible: Codex OAuth, Gemini OAuth, Mistral and other third-party Codex groups, and Gemini's `/v1beta/openai` chat-compatibility endpoint are excluded.

Credentials rotate on transport failures, `401`, `403`, `429`, and `5xx`. TypeSafe retains its narrower 403 behavior: only `detail.error_type=authentication_error` rotates. Other 4xx responses, including Gemini invalid-argument `400`, return unchanged without cooling a credential. A `Retry-After` response is retained when the final upstream response is `429`.

No additional provider family or model declaration is required. Add an official API key using the existing v8 groups:

```yaml
api-keys:
  codex:
    - name: openai-official
      base-url: "https://api.openai.com/v1"
      keys:
        - api-key: "sk-..."
  gemini:
    - name: ai-studio
      base-url: "https://generativelanguage.googleapis.com"
      api-key: "AIza..."
```

An empty base URL selects the same official defaults. Use `https://generativelanguage.googleapis.com/v1beta` or `https://api.openai.com/v1` if preferred; the proxy does not append `/v1` or `/v1beta` twice.

## OpenAI example

```bash
curl -X POST "http://127.0.0.1:8317/v1/decisions" \
  -H "Authorization: Bearer CPA_CLIENT_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-6-luna",
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
