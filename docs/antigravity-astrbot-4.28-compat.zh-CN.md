# AstrBot 4.28.0 实测与兼容结果

日期：2026-11-18。CPA 通用基准为 `71619995d71fbf1e3c9299bef6dc0972ad004dff`。AstrBot 官方标签 v4.28.0 / a412146401426c0cdff8bbefb8627a03da519da8，位于 `C:\astrbot\AstrBot-4.28.0`，原版源码保持干净。

## 测试方式

运行真实 `build_main_agent`、`Context`、`FunctionToolManager`、`ToolLoopAgentRunner`、`FunctionToolExecutor`、内置 FileReadTool 和 Chat/Responses provider，经 openai 2.45.0 SDK 发 HTTP 请求。只有配置服务、平台消息事件和模型响应使用本地固定数据；没有手拼 AstrBot 的模型请求。真实内置工具读取每项新生成的随机文件内容，再通过完整历史回传。

运行目录、配置和虚拟环境独立。使用虚构凭据和回环固定上游，Python socket 连接限制为回环，未使用真实模型或现有机器人账号。Go 探针只配置回环上游；未启动完整机器人平台和 CPA API router。录制代理缓冲 SSE 返回，因此验证了流式事件解析和转换，没有测量分块时序或延迟。

## 通用阶段先验结果

在添加 AstrBot 专属规则之前，Chat/Responses 的流式、非流式四项均通过，每项包含读文件、结果回传和带历史的后续请求。原始证据保存在 baseline/，专项后保存到 after/，两个阶段各 12 个出站请求。

入站 SDK 的 17 种头部收敛为 6 种出站头：Host、User-Agent、Content-Length、Authorization、Content-Type、Accept-Encoding。系统角色为 user，七个工具分别放在单声明容器；名称和描述保留。输出上限 2048 以及 includeThoughts=true / thinkingBudget=1024 正确保留。说明这部分通用出口在未认识 AstrBot 时已经生效。

## 专项修改及效果

实测没有发现需要新增 AstrBot 协议分支的功能失败。因此复用既有文本替换机制，提供八条有原版来源的固定指引句中性改写，覆盖安全模式引导、本地执行引导、托管 shell 会话、write_line、非脱离执行、工具调用风格和工作目录指引。

专项后四项再次全部通过；每项八条原句都被替换，安全约束、PowerShell 能力说明、动态工作目录、工具名和动作名保留。专项前后全部七个工具声明和 generationConfig 完全一致，头部名称集合不变。没有修改 AstrBot 源码，也没有给它新增专属协议分支。

规则包是 CPA 配置，位于 `internal/runtime/executor/testdata/antigravity_harness_rules.yaml`，现在有 56 条正则和 66 条字面替换。**专项效果需要在运行配置中加载这份规则；提交代码或只替换二进制不会自动更新生产规则。** 本次没有修改生产配置或部署服务。

## 效果评估与剩余差异

头部和正文外层结构已实现通用覆盖，专项减少了已核实的固定指引原句；不能据此声称整个正文已与原生完全一致。原生 CLI 的章节及文本规模仍与 AstrBot 不同，实际工具名按要求保留。

既有 schema 清理仍影响五个工具：astrbot_execute_python、astrbot_execute_shell、astrbot_file_read_tool、astrbot_grep_tool、astrbot_shell_session；这是通用基准中已存在的差异，专项没有继续改动它们。没有实际上游接受性证据，未随意删除清理逻辑。

原版 provider 没有发送显式 session/cache 标识。探针中 requestId 为两段，完整生产路由还可能做额外会话解析，本轮没有把探针结果扩大为生产结论。没有伪造 labels、会话信息或工具协议。原生 OAuth 全套头、TLS、Worker 到真实源站及 trajectory/step 演进仍没有新增证据。

## 验证与交付

新增 Go 验证使用真实捕获请求，仅匿名化本地路径并省略传输信封；检查八条改写、工具与生成参数保持、安全约束保持、工具描述及参数描述不被文本规则污染。执行器完整测试通过；发布前执行 diff 和格式检查，验证日志随本地证据保留。

本地 `comparison.json`、baseline/ 和 after/ 保留原始脱敏请求、运行摘要与校验和；`baseline/ASSESSMENT.zh-CN.md` 是专项修改前评估。计划见 PLAN.zh-CN.md。CPA 所有改动按用户授权推送同一暂存分支；未合并、未部署。
