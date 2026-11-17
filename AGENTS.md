# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Gemini/Claude/Codex compatible APIs with OAuth and round-robin load balancing.

## Repository
- Upstream: https://github.com/router-for-me/CLIProxyAPI
- This checkout's fork: https://github.com/Kalospacer/CLIProxyAPI

## Fork Compatibility
- TypeSafe is part of the v8 API-key family map (`api-keys.typesafe`). Configuration reads must remain non-mutating; v8 writes migrate legacy TypeSafe keys and preserve parent/bundled key values.
- Codex, xAI, and TypeSafe synthesize nested `api-key-entries`. Credential resolution must honor the entry's effective proxy override, configuration index, prefix, and base URL to retain the correct model mapping.
- `config_v8_typesafe_test.go` covers the restored management frontend's GET/PUT list-replacement contract, invalid-weight atomicity, and reload snapshots. See `docs/typesafe-v8-frontend-contract.md` for verification and deployment boundaries.
- JEV SystemOne must rotate credentials when a 403 response has `detail.error_type=authentication_error`; other 403 errors stay request-scoped. `server_systemone_test.go` covers the distinction.
- Native decision protocols remain separate passthroughs: JEV uses `/v1/systemone` (`api-keys.typesafe`), OpenAI uses `/v1/decisions` and `/openai/v1/decisions` (`api-keys.decision-openai`), and Gemini uses `/v1beta/decisions` (`api-keys.decision-gemini`). Their shared handler in `server_decisions.go` bypasses the chat model catalog, isolates credentials into dedicated provider families, and honors wildcard `excluded-models` disabling. See `docs/decisions-api.md`.

## 上游同步与插件契约

- 同步流程：本地 `git merge --no-ff origin/main` → 解决冲突 → `go build ./...` 与关键包测试（`sdk/cliproxy/usage`、`internal/watcher`、`internal/runtime/executor/helps`、`internal/translator/codex`）→ 服务器 `CGO_ENABLED=1` 构建 → 8319 测试端口验证 → 备份生产二进制后替换上线。
- 固定冲突：`internal/watcher/config_reload.go` 一律取上游实现（`configApplyMu` 串行化 + 重载后记录 `config.SourceHash`）。不要保留本 fork 的旧写法。
- `sdk/pluginapi` 必须保留 `UsageDetail.Breakdown` 与 `UsageTokenBreakdown*`（本 fork 新增，下游 `cpa-key-billing` 依赖宿主 token 口径）。上游没有这些类型，合并后需确认仍在。
- 生产插件通过 C ABI / JSON 动态库接口加载，不使用 Go `plugin.Open`；修改 `sdk/pluginapi` 时仍需用隔离实例验证真实插件加载和实际行为，不应仅凭结构体变化断言兼容。
- token 记账修复不要回退：subset 语义下允许上报的 reasoning 大于 output（`NewSubsetTokenBreakdownWithReportedReasoning`），以及 `internal/runtime/executor/helps/openai_compat_envelope.go` 对 `{"data":…,"success":true}` 包裹响应的解包。
- 已下线、不再恢复：`internal/translator/common/reasoning_default.go`（按注册档位挑选最接近的 reasoning effort）与 `/v1/models/api.json` 的 `support_efforts` 字段。
- 同步进度：上游已合并至 `43f27e3a`，本 fork 合并提交 `e9ab3fd9`。该次合并在 `sdk/pluginapi` 仅新增 `HostAuthModelsRequest`、`HostAuthModelsResponse`、`HostAuthModels` 三个独立类型，未改动既有结构体字段，因此现有插件无需重建即可加载。
- `cmd/server` 已移除 `-port` 启动参数，监听端口只由配置文件 `port` 决定。创建 8319 测试实例时，需复制配置文件并把 `port`、`oauth.auth-dir`、`plugins.dir`、`plugins.configs.*.state_file` 改到独立路径，再以 `--config` 启动。

## 本次上游同步状态
- 本工作区正在对照 Antigravity 2.32.1 CCPA：出站普通文本请求使用空串 FNV-1a 的有符号值，无 trajectory 时的 requestId 使用 `requestType/UUID`；内部推理回放仍按原有会话派生隔离。429、重试及 Worker 不在本次修改范围。
- 按当前任务要求，Antigravity 的敏感内容规则仅处理最终请求中的系统指令和普通对话文本；工具名称、声明、参数、调用和结果不由该规则改写。更多客户端样本及原版传输仍在调查，当前工作区未部署。
- `internal/runtime/executor/testdata/antigravity_harness_rules.yaml` 保存候选改写规则；普通文本样本覆盖 DSH 身份、上下文和规则包装、Claude 部分风格与本机 Codex 5 份完整模板。OpenCode 覆盖已按交付范围移除，生产配置未更新。
- 改写顺序是硬约束：敏感内容替换必须早于结构化输出 schema 注入与 payload override。放到最终出口会连带改写内联 JSON schema 和用户覆盖文本，破坏结构化输出与 override 语义。
- 独立工作区已将 PR #11 的上游 `7df56074` 合并到基线 `fe1e08b4`，解决了 `sdk/cliproxy/service_executors.go` 中的执行器注册冲突；该合并尚未提交或推送。
- 生产环境中的原帖规则及 DSH、OpenCode 身份替换属于部署配置；线上当前实现仅作用于 Antigravity 系统指令，独立工作区的普通对话文本扩展尚未部署。
- Go 1.26.10、CGO 构建及相关测试已通过；4 个生产插件文件在无外网的隔离实例中完成加载和注册，业务功能仍需单独验证。
- 2026-11-18：该未提交合并构建已部署至 `/opt/cliproxy/cli-proxy-api`；生产配置启用了 14 条正则和 1 条词替换。健康检查与模型列表正常；无 Antigravity OAuth 凭证的真实上游请求验证。

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
- Do not use `github.com/tidwall/gjson`, `github.com/tidwall/sjson`, Go's standard `encoding/json`, Go's standard `encoding/base64`, or Go's standard `regexp`. Always use `github.com/MoeForge/moego/json`, `github.com/MoeForge/moego/ojson`, `github.com/MoeForge/moego/base64`, and `github.com/MoeForge/moego/regexp` for all JSON, Base64, and regular expression operations. All JSON write operations (`Set`, `SetRaw`, `Delete`, etc.) MUST be performed on `*ojson.Doc` (`ojson.NewDoc` / `ojson.NewDocString`), passing `*ojson.Doc` across mutation stages and serializing only once at the final exit (`doc.Bytes()` / `doc.String()`), rather than mutating raw `[]byte` or `string` payloads directly.
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
