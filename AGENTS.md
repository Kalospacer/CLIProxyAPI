# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex compatible APIs with OAuth and round-robin load balancing.

## Repository
- Upstream: https://github.com/router-for-me/CLIProxyAPI
- This checkout's fork: https://github.com/Kalospacer/CLIProxyAPI

## Fork Compatibility
- TypeSafe is part of the v8 API-key family map (`api-keys.typesafe`). Configuration reads must remain non-mutating; v8 writes migrate legacy TypeSafe keys and preserve parent/bundled key values.
- Codex, xAI, and TypeSafe synthesize nested `api-key-entries`. Credential resolution must honor the entry's effective proxy override, configuration index, prefix, and base URL to retain the correct model mapping.
- `config_v8_typesafe_test.go` covers the restored management frontend's GET/PUT list-replacement contract, invalid-weight atomicity, and reload snapshots. See `docs/typesafe-v8-frontend-contract.md` for verification and deployment boundaries.

## 上游同步与插件契约

- 同步流程：本地 `git merge --no-ff origin/main` → 解决冲突 → `go build ./...` 与关键包测试（`sdk/cliproxy/usage`、`internal/watcher`、`internal/runtime/executor/helps`、`internal/translator/codex`）→ 服务器 `CGO_ENABLED=1` 构建 → 8319 测试端口验证 → 备份生产二进制后替换上线。
- 固定冲突：`internal/watcher/config_reload.go` 一律取上游实现（`configApplyMu` 串行化 + 重载后记录 `config.SourceHash`）。不要保留本 fork 的旧写法。
- `sdk/pluginapi` 必须保留 `UsageDetail.Breakdown` 与 `UsageTokenBreakdown*`（本 fork 新增，下游 `cpa-key-billing` 依赖宿主 token 口径）。上游没有这些类型，合并后需确认仍在。
- 插件是 `-buildmode=plugin` 动态库，与宿主共享 `sdk/pluginapi` 的结构体布局；任何字段增删都要重建全部启用插件。`kimi-apijson-editor` 本地无源码，不能重建，因此不要改动该包结构体。
- token 记账修复不要回退：subset 语义下允许上报的 reasoning 大于 output（`NewSubsetTokenBreakdownWithReportedReasoning`），以及 `internal/runtime/executor/helps/openai_compat_envelope.go` 对 `{"data":…,"success":true}` 包裹响应的解包。
- 已下线、不再恢复：`internal/translator/common/reasoning_default.go`（按注册档位挑选最接近的 reasoning effort）与 `/v1/models/api.json` 的 `support_efforts` 字段。
- 同步进度：上游已合并至 `43f27e3a`，本 fork 合并提交 `e9ab3fd9`。该次合并在 `sdk/pluginapi` 仅新增 `HostAuthModelsRequest`、`HostAuthModelsResponse`、`HostAuthModels` 三个独立类型，未改动既有结构体字段，因此现有插件无需重建即可加载。
- `cmd/server` 已移除 `-port` 启动参数，监听端口只由配置文件 `port` 决定。创建 8319 测试实例时，需复制配置文件并把 `port`、`oauth.auth-dir`、`plugins.dir`、`plugins.configs.*.state_file` 改到独立路径，再以 `--config` 启动。

## Commands
```bash
gofmt -w . # Format (required after Go changes)
go build -o cli-proxy-api ./cmd/server # Build
go run ./cmd/server # Run dev server
go test ./... # Run all tests
go test -v -run TestName ./path/to/pkg # Run single test
go build -o test-output ./cmd/server && rm test-output # Verify compile (REQUIRED after changes)
```
- Common flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`, `--no-browser`, `--oauth-callback-port <port>`

## Config
- Default config: `config.yaml` (template: `config.example.yaml`)
- `.env` is auto-loaded from the working directory
- Auth material defaults under `auths/`
- Storage backends: file-based default; optional Postgres/git/object store (`PGSTORE_*`, `GITSTORE_*`, `OBJECTSTORE_*`)

## Architecture
- `cmd/server/` — Server entrypoint
- `internal/api/` — Gin HTTP API (routes, middleware, modules)
- `internal/api/modules/amp/` — Amp integration (Amp-style routes + reverse proxy)
- `internal/thinking/` — Main thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (`suffix.go`, suffix overrides body), normalizes config to canonical `ThinkingConfig` (`types.go`), normalizes and validates centrally (`validate.go`/`convert.go`), then applies provider-specific output via `ProviderApplier`. Do not break this "canonical representation → per-provider translation" architecture.
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/usage/` — Usage and token accounting
- `internal/home/` — CLIProxyAPIHome control plane integration (bootstrap, RESP communication, dispatch coordination)
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline)
- `test/` — Cross-module integration tests

## Code Conventions
- Keep changes small and simple (KISS)
- Comments in English only
- If editing code that already contains non-English comments, translate them to English (don’t add new non-English comments)
- For user-visible strings, keep the existing language used in that file/area
- New Markdown docs should be in English unless the file is explicitly language-specific (e.g. `README_CN.md`)
- As a rule, do not make standalone changes to `internal/translator/`. You may modify it only as part of broader changes elsewhere.
- If a task requires changing only `internal/translator/`, run `gh repo view --json viewerPermission -q .viewerPermission` to confirm you have `WRITE`, `MAINTAIN`, or `ADMIN`. If you do, you may proceed; otherwise, file a GitHub issue including the goal, rationale, and the intended implementation code, then stop further work.
- `internal/runtime/executor/` should contain executors and their unit tests only. Place any helper/supporting files under `internal/runtime/executor/helps/`.
- Payload configuration MUST be the final semantic barrier before sending requests in every executor, including streaming, WebSocket, continuation, retry/fallback, image, and token-count paths where applicable. Complete all built-in payload translation, normalization, injection, and cleanup first, then evaluate and apply user payload rules exactly once to the final business payload. No subsequent logic may overwrite configured values or restore filtered fields. Only necessary transport framing, serialization, signature calculation, and read-only validation may follow without changing business payload semantics. Add regression tests when introducing or changing request-building paths to enforce this ordering.
- Follow `gofmt`; keep imports goimports-style; wrap errors with context where helpful
- Do not use `log.Fatal`/`log.Fatalf` (terminates the process); prefer returning errors and logging via logrus
- Shadowed variables: use method suffix (`errStart := server.Start()`)
- Wrap defer errors: `defer func() { if err := f.Close(); err != nil { log.Errorf(...) } }()`
- Use logrus structured logging; avoid leaking secrets/tokens in logs
- Avoid panics in HTTP handlers; prefer logged errors and meaningful HTTP status codes
- Timeouts are allowed only during credential acquisition; after an upstream connection is established, do not set timeouts for any subsequent network behavior. Intentional exceptions that must remain allowed are the Codex websocket liveness deadlines in `internal/runtime/executor/codex_websockets_executor.go`, the wsrelay session deadlines in `internal/wsrelay/session.go`, the management APICall timeout in `internal/api/handlers/management/api_tools.go`, and the `cmd/fetch_antigravity_models` utility timeouts
- Avoid wall-clock `time.Sleep` in TTL, expiration, ordering, or cache-eviction unit tests due to platform timer granularity (e.g. Windows default timer resolution of ~15.6ms) and CI jitter under load; prefer controllable clocks (`nowFunc` / mock clock), explicit timestamp manipulation, or deterministic synchronization primitives.
- Note: if modifying features that involve CLIProxyAPIHome, check if corresponding updates are needed in the CLIProxyAPIHome repository.
- Endpoints under the `/v0/management` base URL are deprecated and no longer maintained. For any feature changes, do not modify endpoints under `/v0/management` unless necessary to fix compilation errors.
