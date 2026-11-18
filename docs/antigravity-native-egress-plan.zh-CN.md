# Antigravity 通用出站对齐计划

日期：2026-11-18。状态：开始实施；最终结果以 RESULT.zh-CN.md 为准。

## 目标与验收对象

未知第三方 harness 通过 Responses 或 Chat Completions 接入 CPA，统一由 Antigravity 执行器生成具有原生结构的出站请求。客户端名字和提示词模板不参与这层判断。验收对象是接收端实际收到的请求头、请求正文和会话字段；工具执行通过只是辅助检查。

首个真实客户端是 DSH；已有 Antigravity CLI 1.5.10 运行记录和桌面 Hub 2.32.1 内存 CCPA 正文作为证据。两种原生客户端及 gateway/CCPA 路径分别标注。CLI gateway UA 中的 auth_method=gateway 不复制到 OAuth 请求。已有日志只记录 User-Agent、Content-Type、Accept，必须增加完整脱敏接收端记录，不能拿三项记录宣称完整头部已对齐。

## 工作区和实施保护

- 原源码：服务器 `/home/ubuntu/cpa-merge-pr11`，HEAD `322c891cda4e963df95a5915e1dea7dbb1d3a70e`，有五项既有未提交改动。
- 修改前完整 tar 备份原目录，包括 Git 和未提交内容；校验归档可读和 SHA256。
- 在独立源码副本修改、测试、构建；先保存原目录源码哈希清单。
- 对最终文件逐项检查差异，验证原目录未被其他任务修改后，再把本次文件放回原工作区。出现并发变化时不覆盖，保留独立副本交付。
- 检查修改文件的 LF/CRLF，Go 源码使用 gofmt；不改无关文件行尾。
- 不提交、不推送、不部署、不修改本机代理、不使用 WSL；保留 Worker、429、重试和凭据轮换。未获得子代理授权，采用自审。

## 当前源码事实

1. 普通流式与非流式生成共用 buildRequestDoc；CountTokens 单独构造请求；HttpRequest 还有一套入站头部清理。
2. buildRequestDoc 与 CountTokens 设置 Content-Type、Authorization、User-Agent 后应用管理员自定义头。自定义头是显式操作配置，保留覆盖优先级；不把它与第三方 SDK 默认透传混为一谈。
3. HttpRequest 使用 WithContext 后清空 Header，实际会修改传入请求共享的 map。Request.Close、Host、RequestURI、Trailer 等也要从入站语义转成上游请求语义。
4. Responses -> Gemini 系统指令缺 role；原生 CLI、桌面样本均为 user。原生工具每个 wrapper 一个声明，DSH 翻译后一个 wrapper 有 23 个声明。
5. 现有工具清理将 Gemini Pro 也放进占位参数处理条件，给无必填参数工具增加 reason / _。需要只把占位字段限定到原本要求它的 Claude VALIDATED 路径，保留其他兼容清理，不凭离线响应宣称所有约束都受真实上游支持。
6. 现有未提交默认生成配置将 CLI 单模型样本推广到多种模型。显式预算包括 1024 必须继续生效；只给已实测的同一 CLI 模型补对应缺省值，其他模型不猜。桌面 262144/HIGH 不作为 CLI 通用默认。

## 实现方案

### A. 统一请求头构造

- 新增小型 Antigravity 帮助函数，供生成、计数和直接转发共用。
- 新建或克隆上游请求；创建独立 Header map，仅从明确的出口参数设置基线头；上游 Bearer 凭据不使用第三方 API Key。
- SDK 自带 UA、x-stainless-*、x-client-*、追踪/重试信息等不因出现在入站请求而自动转发，采用默认不透传策略，不维护品牌黑名单。
- Host 来自上游 URL；传输层自行计算长度及分帧。清除入站 RequestURI、Close 和 Trailer 等不应沿用的字段。
- 保留管理员自定义头覆盖；用测试明确覆盖行为，不能静默使已有配置失效。
- 继续复用现有 HTTP/1.1、TLS、连接池和 Worker 传输；不根据不完整证据新造 TLS 参数或请求头。

### B. 统一正文外层结构

- 只在 Antigravity 出口、payload override 之前归一化，Responses 和 Chat 自动共用，不改变其他 provider。
- 系统指令对象含有效 parts 且缺 role 时补 user；已有显式角色不覆盖，不凭空插入系统提示词。
- 将纯 functionDeclarations wrapper 拆成每个声明一个 wrapper，保留声明内容和顺序；混有未知 sibling 配置的 wrapper、内置工具保持原样，避免复制配置或改变作用域。
- 会话请求 ID 保持现有五段形态和来源；保留原会话关联，不临时新增无界会话存储或猜测 step/trajectory 的演进。
- labels 保留入站真实值和管理员显式配置；未查清来源的标签不补伪值。分别列出与原生样本仍有的差异。
- 已有特征句规则照原顺序运行；未知文本不凭关键词猜来源。工具说明、用户内容和参数不能整块替换为原生模板。

### C. 参数和工具约束

- 完整保留显式生成参数及原 CPA 思考转换功能；按模型现有能力校验处理。
- 已有 broad 默认值收敛到实测模型，不引入 HIGH/262144 统一配置。显式关闭、显式预算及档位不能被兜底覆盖。
- Gemini 不注入 Claude 的 reason/_ 占位参数；Claude VALIDATED 和现有结构化输出保留原路径。
- 不扩大到全量工具 schema 重写策略，记录仍存在的 enum 等兼容转换和证据缺口。

## 测试与证据

1. 用没有品牌特征的新工具与提示词构造语义等价的 Responses/Chat 请求，走生产执行器到本地 HTTP 接收端。比较 role、工具 wrapper、完整工具关联和显式参数。
2. 给入站放任意 SDK 头、重复头、Connection/Trailer；接收端核验它们未泄漏，调用方的 Header/URL 等未被修改。
3. 验证静态与动态管理员头覆盖、UA 配置和上游 Bearer，记录这些显式配置可产生预期差异。
4. 验证 payload override 是最后语义屏障：可覆盖 role、工具组织和生成参数；归一化不得在之后恢复字段。
5. Gemini 无参数/全可选参数工具不再新增占位字段；Claude 仍保留所需占位兼容。
6. 按本次改动跑相关 executor/helps、UA 和 schema 测试；完成服务端构建。仅有新失败或改动才重复扩大测试。
7. 真实 Antigravity CLI、DSH 使用既有安装，在仅有回环网络的命名空间运行，服务端只提供固定响应，凭据虚构。记录所有接收头，敏感值脱敏。不再做跨客户端工具互换实验。
8. 比较已有原始请求与修改后出站，逐项列出已对齐、刻意保留和仍缺原生证据的差异。不得给伪造的整体相似度百分数。
9. 自审改动、输出 diff --check、格式和构建结果，更新项目 AGENTS.md，交付本地计划、结果、补丁及备份位置。

## 具体边界

未知任意提示词无法仅凭正文确认来源，因此本轮不承诺自动消除所有 harness 行为特征。TLS 完全一致、原生 OAuth 全套头部、动态标签及 trajectory/step 的真实演进仍取决于对应原生路径证据，不能由固定本地响应验证。交付报告必须把这些与本次已完成的通用出口功能分别写明。

实现以以上目标和约束为准；如源码显示更小、更可靠的实现，应采用并在结果中说明理由。

## 后续授权：AstrBot 4.28.0

用户要求通用阶段完成后推送暂存，再以 AstrBot 4.28.0 检验未知 harness 路径，评估后针对实际差异兼容。

1. 在独立 staging 分支提交已测试的通用阶段及本任务继承的相关改动，推送 fork，不合并 main 或部署。固定提交号作为未知客户端测试基准。
2. 检查 `C:\astrbot` 已有内容，在该目录下创建独立的版本目录；不覆盖既有 AstrBot、插件、配置或运行环境。检出并核验 4.28.0 对应标签和提交。
3. 阅读该版本的真实 provider 和 Agent Runner 入口；用隔离环境、虚构凭据、本地固定服务驱动真实 AstrBot 代码发起请求，不凭空手写 AstrBot 请求代替客户端。
4. 在未添加任何 AstrBot 专属 CPA 规则的 staging 基准上，记录入站与出站的完整脱敏请求，评估指令、头部、工具 schema、工具结果及显式参数。
5. 根据实测差异再实施有必要的 AstrBot 兼容；改动优先作用于 CPA，以原版 AstrBot 作为可重复的客户端。若需要修改 AstrBot，本轮不默认推送它的仓库。
6. 每批代码修改前做该批适用备份，完成定向测试和自审，更新计划与结果。明确记录通用基准结果和后加兼容的效果，不能把后者当成前者的能力。
