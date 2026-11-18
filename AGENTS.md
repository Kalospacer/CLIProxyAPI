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
- 按当前任务要求，Antigravity 的敏感内容规则仅处理最终请求中的系统指令和普通对话文本；工具名称、声明、参数、调用和结果不由该规则改写。当前工作区未部署。
- `internal/runtime/executor/testdata/antigravity_harness_rules.yaml` 保存改写规则，共 56 条正则和 66 条字面替换；覆盖 DSH 身份与上下文、Claude Code 身份句与风格段、本机 Codex 5 份完整模板。DSH 实测系统提示词中的 12 段工具行为指引均已改为中性表述，工具名保持不变。OpenCode 覆盖已按交付范围移除，生产配置未更新。
- 改写顺序是硬约束：敏感内容替换必须早于结构化输出 schema 注入与 payload override。放到最终出口会连带改写内联 JSON schema 和用户覆盖文本，破坏结构化输出与 override 语义。
- requestId 采用原生五段形态 `requestType/conversationId/时间戳/trajectoryId/stepIndex`，会话标识取 CPA 会话派生 UUID；无会话时退化为 `requestType/UUID`。trajectoryId 由会话标识派生，与原生按轨迹推进的语义仍有差距。
- UA 采用实测 Antigravity CLI 1.5.10 形态：`antigravity/cli/1.5.10 (aidev_client; os_type=linux; arch=amd64; cl=1023297289)`；桌面版回退版本仍为 2.32.1。TLS 侧沿用上游既有实现：强制 HTTP/1.1 并清空 ALPN，与原生 Go 客户端一致，已有测试锁定。
- 缺省生成配置只应用于 CLI 1.5.10 已实测的 `gemini-3.1-pro-preview`：缺省输出上限不超过 65535，缺省思考预算为 1024；显式配置和既有思考转换优先。其他模型不根据该样本补值。保留真实传入的 `request.labels`，未知标签不伪造。
- 敏感词匹配会先剔除零宽字符再匹配，命中区间映射回原文；字面替换路径同样先归一化，覆盖被零宽字符打断的特征句。CRLF 行尾风险已核查，当前只有 persona 规则使用 `$` 行尾锚点，并已写成 `\r?$`。
- 独立工作区已将 PR #11 的上游 `7df56074` 合并到基线 `fe1e08b4`，解决了 `sdk/cliproxy/service_executors.go` 中的执行器注册冲突，并把结果快进合并到 `main`（`0c091453`），PR #11 状态为已合并。
- 生产环境中的原帖规则及 DSH、OpenCode 身份替换属于部署配置；线上当前实现仅作用于 Antigravity 系统指令，独立工作区的普通对话文本扩展和生成配置默认值尚未部署。
- Go 1.26.10 的 Antigravity 定向测试和 executor 全包测试已通过；`internal/thinking` 的 `TestClaudeToCodexStandaloneEffort` 在未修改的 HEAD 上同样失败，属于基线问题。
- 2026-11-18：线上 `/opt/cliproxy/cli-proxy-api` 仍使用旧构建；生产配置启用了 14 条正则和 1 条词替换。本次工作区修改尚未提交、构建产物尚未部署。
- 通用 Antigravity 出口：生成、计数及直接 HTTP 转发共用 `helps.ConfigureAntigravityRequest`，只从出口配置构造基础头部，管理员自定义头最后生效。直接转发先克隆请求，清除入站 Host、RequestURI、Close、Trailer 和显式分帧信息，不修改调用方 Header map。
- agent 正文在 payload override 之前补缺失的 systemInstruction.role=user，并把纯 functionDeclarations 容器组织为一声明一容器；混有其他配置的容器保留作用域。不会按客户端名称决定是否处理，也不重写未知提示词或工具内容。
- Gemini 不再进入 Claude VALIDATED 的占位参数注入路径；既有 enum 等工具格式兼容转换保留。离线真实 DSH 样本仍有 7 个工具的参数格式转换，不能宣称工具 schema 完全不变。
- 通用出口已有陌生 Responses/Chat、直接转发不污染入站请求、payload override 和缺省值边界测试。相关 executor/helps/misc/Antigravity translator 包测试、服务端构建及 10 项回环隔离检查通过。真实客户端探针走生产执行器和翻译器，不是完整 CPA API router；原生 OAuth 全头、Worker 到源站及 TLS 尚未抓取。

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

## AstrBot 4.28.0 专项记录

- 通用基准提交为 `71619995`，已推送 `staging/antigravity-native-egress-20261118`；未合并或部署。
- 原版 AstrBot v4.28.0 / `a4121464` 真实主代理与内置文件工具在未加专项规则前，通过 Chat/Responses 流式及非流式四项读文件和带历史后续请求；SDK 头被通用出口隔离。
- 八条实测固定指引句复用配置规则做中性改写，工具声明、生成参数、安全约束和动态目录保持；专项后四项再次通过。规则包需要显式加载到运行配置，只替换二进制不会自动启用，生产配置未修改。
- 五个工具仍有原有 schema 清理；原版 provider 未发送显式 session/cache 标识。探针不是完整 CPA API router，不从两段 requestId 推断生产行为。详细记录见 `docs/antigravity-astrbot-4.28-compat.zh-CN.md`。

## Codex 真实客户端专项记录

- 2026-11-18：0.160.0 原版 CLI 在仅回环沙盒完成专项前后真实文件工具循环；请求头与正文对照 Antigravity 原生样本。
- 规则包现有 101 条正则、66 条字面替换。可执行路径、环境变量和工具合同保留，详细证据及未验证项见 `docs/antigravity-codex-real-client.zh-CN.md`。
- 本批只推送暂存分支；按用户最新授权，后续完成三客户端后统一合并 main、加载规则并部署。旧记录中“OpenCode 排除/不部署”不再约束本批任务。
