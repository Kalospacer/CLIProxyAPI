# Antigravity 请求与出口对齐记录（对照 Stravia v0.7.5 与官方 CLI 1.5.13）

## 结论与当前状态

本工作区已在隔离环境中完成 Antigravity 出站传输层（TLS 指纹、HTTP/1.1 头序与分帧）、CLI 1.5.13 consumer User-Agent、官方 296 字节身份前缀与独立三段式桥接、最小会话轨迹生命周期状态、普通文本非流式统一走上游 SSE、`thinkingLevel` 规范编码，以及移除源码内 Cloudflare Worker 改写（`internal/util/google_rewrite.go`）。

- 当前状态：**隔离工作区已通过单元测试与服务端构建；尚未提交、尚未推送、尚未部署生产、未向 Google 发起真实账号推理实测。**
- 离线真实客户端（AstrBot、DSH、Codex）对照结果将在隔离端到端验证完成后补充至本文档「隔离客户端验证结果」一节。

## 基线与证据边界

| 项目 | 固定基线 | 证据来源与限制 |
| :--- | :--- | :--- |
| CPA | `Kalospacer/CLIProxyAPI` `main` `ffeaf6361c72388ef09b0e704578984e08c82107`（Go 1.26.10） | 实施前核对与线上二进制 `vcs.revision` 一致且 `vcs.modified=false`；已在项目外目录完成全量备份 |
| Stravia | `v0.7.5` commit `aa69767523da8010cbbbd306bded13f212ec52e2` | 只读对照参考，不修改或部署；其 Antigravity 插件许可为 `AGPL-3.0-only` |
| 官方 Antigravity CLI | `1.5.13` Linux/amd64 consumer（SHA256 `7fa9cc6df0d09562693d637c406a86c1b0f52ab3364714b1fd2d79cc6060d4b2`） | 使用虚构 OAuth Token 与本地回环 MITM（同时通告 `h2` 与 `http/1.1`）完成全离线单轮及 `--continue` 双轮捕获；零真实账号与外网推理流量，不证明 Google 服务端接受行为或防封效果 |

### 关键实测边界

1. **ALPN 行为**：官方 CLI 1.5.13 访问 `www.googleapis.com` 与 `play.googleapis.com` 时发送 `[h2, http/1.1]` 并协商 HTTP/2，但访问 `daily-cloudcode-pa.googleapis.com` 时自身仅通告 `["http/1.1"]`。
2. **动态字段**：TLS 随机数、KeyShare 材料、时间戳、UUID、凭据与用户对话正文按规范动态生成，仅对固定套件、扩展顺序、头序、前缀与结构进行逐字节或规则校验，不固定随机值。
3. **出口变更**：删除 `internal/util/google_rewrite.go` 后，Antigravity 请求不再经过 Cloudflare Worker，消除 Worker 重新握手与自动由 Bearer 生成 `x-api-key` 的行为；生产部署切换前需注意出口公网 IP/地区由 Cloudflare 边缘变更为服务器直连（美国加州圣克拉拉 AS132203，或后续单独配置的代理）。

## 已确认的逐项对齐策略

| 维度 | 实现行为 | 代码位置 |
| :--- | :--- | :--- |
| TLS ClientHello | 基于仓库既有 `uTLS v1.8.2` 复刻官方 1.5.13 套件、扩展序列 `[0,11,65281,23,18,5,10,13,50,16,43,51]`、支持曲线（含 `X25519MLKEM768`）、签名算法（含 ML-DSA `0x0904-0x0906`）与 ALPN `["http/1.1"]`（`JA3 = 03117a8ed39ef02427ebbc39f121275c`）；保持完整证书校验、SNI、代理与取消语义，非法代理显式报错不回退 | `internal/antigravitywire/wire.go` |
| HTTP/1.1 头序与分帧 | CCPA RPC 按 `Host` → `User-Agent` → `Content-Length` / `Transfer-Encoding` → `Authorization` → `Content-Type` → `X-Goog-User-Project`（按需）→ `Accept-Encoding` 排序；流式推理使用 `Transfer-Encoding: chunked`；OAuth 刷新按表单头序排序 | `internal/antigravitywire/wire.go`、`internal/runtime/executor/antigravity_executor_request.go` |
| User-Agent | 升级为 `antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)`；`onboardUser` 移除旧 Node SDK UA 与 `X-Goog-Api-Client`；OAuth 刷新保持 `Go-http-client/2.0` | `internal/misc/antigravity_version.go`、`internal/auth/antigravity/auth.go` |
| 入口覆盖与例外 | 覆盖推理、OAuth 刷新、项目发现、模型目录发现、额度查询、搜索与图片入口；CPA 独有 `countTokens` 绑定 `OperationCountTokens` 不迁移至 Antigravity uTLS 形态；OAuth 登录本地回环回调（`http://localhost:51121/oauth-callback`）保持不变 | `internal/runtime/executor/antigravity_executor*.go`、`sdk/auth/antigravity_models.go`、`internal/auth/antigravity/auth.go`、`internal/api/handlers/management/api_tools.go` |
| 身份前缀与三段式桥接 | 空客户端系统指令时仅设置官方 296 字节通用助手前缀且 `systemInstruction.role = "user"`；非空客户端系统指令时，先完成敏感词改写，再注入官方 296 字节前缀 + 独立英文桥接指令，并将改写后的客户端系统指令与独立中文确认回复前置至 `contents[0..1]`；工具声明拆分与内容保护保持不变；`FinalizePayloadDoc` 仍位于最终序列化前 | `internal/runtime/executor/helps/antigravity_native_request.go`、`internal/runtime/executor/antigravity_executor_request.go` |
| 会话轨迹与 `sessionId` | 独立于 reasoning replay 缓存存储最小轨迹元数据（Home KV 键前缀 `cpa:antigravity:lifecycle:v1:`，带 CAS 重试与单机内存后备）；可信会话按 FNV-1a64 生成稳定 `sessionId` 并维护多轮 `requestId`/`labels`；缺会话标识、前序失败或 KV 异常时优雅降级（省略 `sessionId` 与不确定步号、退回 `agent/<uuid>`、保守标记置 `true`），不编造步号 | `internal/cache/antigravity_lifecycle_cache.go`、`internal/runtime/executor/antigravity_executor_execute.go`、`internal/runtime/executor/antigravity_executor_stream.go` |
| 非流式与 `thinkingLevel` | 保留 CPA 既有模型别名与显式模型选择；`thinkingLevel` 字符串规范为大写枚举（如 `LOW`/`MEDIUM`/`HIGH`/`EXTRA_HIGH`/`MAX`），数值型保留原样；除图片与 `web_search` 外，普通文本非流式推理统一走上游 `streamGenerateContent?alt=sse` 并保留无 `finishReason` 正常收尾保护 | `internal/runtime/executor/antigravity_executor_request.go`、`internal/runtime/executor/antigravity_executor_execute.go` |
| Worker 清理 | 删除 `internal/util/google_rewrite.go` 及其全部调用点 | `internal/util/proxy.go`、`internal/runtime/executor/helps/proxy_helpers.go`、`internal/api/handlers/management/api_tools.go`、`internal/runtime/executor/antigravity_executor.go`、`sdk/auth/antigravity_models.go` |

## 许可边界说明

- CPA 仓库许可为 MIT；Stravia Antigravity 插件许可为 `AGPL-3.0-only`。
- 本实现仅保留来自 Google 官方 Antigravity CLI 的 296 字节通用助手 `<identity>` 前缀；两段三段式桥接文案（英文桥接指令与中文模拟确认回复）均采用独立编写的新措辞，未复制 Stravia 原创桥接原文；全部 Go 传输与状态机实现均按协议实测独立编写，未照搬 Stravia Rust 源码。

## 单元测试与构建验证

- `go build -o /tmp/cpa-stage-c/verify-final-build ./cmd/server`：通过。
- `go test -count=1` 覆盖全部 9 个受影响包（`internal/antigravitywire`、`internal/cache`、`internal/misc`、`internal/util`、`internal/auth/antigravity`、`internal/runtime/executor/helps`、`sdk/auth`、`internal/api/handlers/management`、`internal/runtime/executor`）：全部通过。
- `go test -race -count=1` 覆盖 4 项新增核心测试入口（`TestAntigravityWireProfile`、`TestAntigravityLifecycleState`、`TestAntigravityIdentityBridge`、`TestAntigravityWireEntryPoints`）：全部通过。
- 预存基线说明：未改动基线 `ffeaf6361c72388ef09b0e704578984e08c82107` 上 `internal/thinking` 的 `TestClaudeToCodexStandaloneEffort` 原先即失败，与本次改动无关。

## 隔离客户端验证结果（阶段 C）

- 测试环境：隔离编译的 Linux/amd64 CPA 二进制（独立端口 `127.0.0.1:28319`、独立 `auth-dir` 单条合成凭据指向 `https://localhost:28443`、关闭插件、`--local-model`、清零 Worker 环境变量、通过 `SSL_CERT_FILE` 信任隔离本地 CA 且不关闭 TLS 校验）+ 可控本地 TLS/HTTP/1.1 抓包接收端（服务端同时通告 `["h2", "http/1.1"]`）。零生产凭据、零真实 Google 外网请求。
- 最终编译产物共捕获 16 条完整出站记录（`seq=47..62`），覆盖开机 `fetchAvailableModels`（1 条）及三大真实客户端端到端流程（15 条），所有固定项逐字节校验全部为 `True`：
  - TLS：`JA3 = 03117a8ed39ef02427ebbc39f121275c`、套件、扩展顺序、曲线（含 `X25519MLKEM768`）、签名算法（含 ML-DSA `0x0904-0x0906`）、`ALPN = ["http/1.1"]`、实际协商 `http/1.1` 与 SNI `localhost` 全部与官方 CLI 1.5.13 捕获一致。
  - HTTP/1.1：`fetchAvailableModels` 为 `Content-Length` 6 头顺序；全部推理请求（含 AstrBot 非流式）均为 `streamGenerateContent?alt=sse` + `Transfer-Encoding: chunked` 6 头顺序；`User-Agent` 为官方 `1.5.13` consumer 值；全部请求无 `x-api-key`、无 `x-proxy-key`。
  - 正文与桥接：全部推理请求均满足 `systemInstruction.role = "user"`、官方 296 字节前缀 + 独立英文桥接指令、`contents[0..1]` 保留客户端系统指令与独立中文确认回复、单声明工具容器。
- 分客户端结果（均使用独立配置/数据目录、合成 API Key 与随机生成的本地标记文件）：
  1. **Codex (`codex-cli 0.160.0`)** — **通过**（`POST /v1/responses`，模型 `gemini-3-flash`，`model_reasoning_effort = "high"`）：`codex exec` 触发内置工具读回随机标记，第 2 轮工具结果回传并命中 `thoughtSignature` 回放，第 3 轮 `codex exec resume --last` 续接同一会话；3 轮（`seq=48..50`）`thinkingLevel` 规范为 `"HIGH"`，`sessionId = 890120636129621402`、同一 `cascade_id`/`trajectory_id`，`requestId` 末段 `/1 → /3 → /5`，`last_step_index` `"0" → "2" → "4"`，`last_execution_id` 逐轮衔接。
  2. **DSH (`DeepSeek Harness 0.2.2-rc.1`)** — **通过**（`POST /v1/messages`，模型 `claude-sonnet-4-6`）：`dsh --profile headless --patch ...` 触发内置 `read` 工具读回随机标记并完成 23 个单声明工具声明转发，第 3 轮 `--session-id` 续接同一会话；3 轮（`seq=51..53`）稳定维持同一 `sessionId = 3776580360563111728`、`cascade_id`/`trajectory_id`，`requestId` 与 `last_step_index` 连续递增，`used_claude = "true"`、`used_non_gemini_model = "true"`。
  3. **AstrBot (`v4.28.0`)** — **通过**（真实 `ToolLoopAgentRunner`、`FunctionToolExecutor`、内置 `FileReadTool`、`ProviderOpenAIOfficial` 与 `ProviderOpenAIResponses`，模型 `gemini-3-flash`）：覆盖 **Chat 流式**（`seq=54..56`）、**Chat 非流式**（`seq=57..59`，实证普通文本非流式经上游 SSE 聚合且第 2 轮 `thoughtSignature` 回放正常）、**Responses 流式**（`seq=60..62`）三条路径；各路径均完成真实工具读随机标记文件、结果回传与第 3 轮续聊，CPA 路由层派生出彼此隔离的稳定 `sessionId` 并连续推进轨迹；无会话标识时的优雅降级行为已在单元测试中独立验证。

## 未验证项

- 真实 Google 账号向线上 Google 上游发起模型推理（需另行确认授权）。
- 生产服务 `/opt/cliproxy/cli-proxy-api` 部署与运行期观察（尚未部署）。
- Google 服务端内部风控、路由或封号判定规则不在可验证范围内。
