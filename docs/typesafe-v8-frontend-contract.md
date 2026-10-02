# TypeSafe v8 frontend contract and bundled credentials

## Scope

The local fork already contains `typesafe-api-key -> api-keys.typesafe` in the v8 family map at baseline `20a8956f`. This work does not duplicate that implementation or modify deprecated v0 routes. It adds explicit management-API regression coverage for the restored frontend and completes two related bundled-credential runtime paths.

- TypeSafe v8 GET/PUT list replacement, migration, runtime reload snapshot, bundled-only rows, clearing and invalid-weight atomicity are covered by `internal/api/handlers/management/config_v8_typesafe_test.go`.
- xAI now synthesizes nested `api-key-entries` and resolves them back to their parent configuration using the existing Codex-style resolver.
- The shared bundled resolver matches the effective proxy (entry override, otherwise parent proxy). A bundled TypeSafe/Codex/xAI credential using `proxy-url: direct` must retain its parent's model mapping even if the parent uses another proxy.

The management frontend preserves both TypeSafe's primary `api-key` and its optional nested `api-key-entries`. These are distinct credentials; a primary key plus two entries produces three runtime credentials. OpenAI compatibility's group-level `keys` is a separate shape.

## Request contract

The frontend reads the complete v8 configuration or the provider family list and writes the complete updated family list:

```text
GET /v8/management/config
GET /v8/management/config/api-keys/typesafe
PUT /v8/management/config/api-keys/typesafe
DELETE /v8/management/config/api-keys/typesafe
```

```json
[
  {
    "name": "typesafe-fixture",
    "base-url": "https://api.typesafe.ai",
    "keys": [
      {
        "api-key": "fixture-primary",
        "api-key-entries": [
          {"api-key": "fixture-one", "weight": 3, "proxy-url": "direct"},
          {"api-key": "fixture-two", "weight": 4}
        ],
        "models": [{"name": "jev-fixture", "alias": "jev-fixture"}]
      }
    ]
  }
]
```

The fixture strings above are not real credentials. Empty family lists clear TypeSafe without changing other providers. Invalid nested weights are rejected before disk/runtime replacement. GET may expose the normalized v8 view but must not rewrite a legacy file.

## Verification

Validation used tracked local source in a separate Linux directory, with no production binary/configuration replacement.

Passed:

- Entire `internal/config`, `internal/api/handlers/management`, and `internal/watcher/synthesizer` test packages.
- Relevant bundled/TypeSafe/xAI tests in `sdk/cliproxy/auth` and relevant service model tests in `sdk/cliproxy`.
- `go vet` for affected configuration, management, synthesis and authentication packages.
- Main server `go build ./cmd/server`.
- An actual compiled CPA server on a loopback temporary port with fixture keys: authenticated TypeSafe v8 reads/updates/clears returned HTTP 200; changing a configured TypeSafe alias appeared in `/v1/models` after the management reload; the Codex family was preserved.

API-key configuration credentials are not ordinary auth files. `/v8/management/credentials` may filter these out because they lack a backing file path; do not use that list to infer that TypeSafe synthesis failed. Use the synthesizer tests and the registered model/reload behavior for this contract.

No upstream inference request was sent, and the full repository `go test ./...` was not run in this scoped verification. Production `:8317` was not restarted or changed. Publishing and deployment require a separate approval; the existing production binary may still lack the v8 TypeSafe map even when local source has it.
