# Antigravity 通用出站阶段结果

日期：2026-11-18。服务未部署。AstrBot 阶段另行记录，不与本次未知协议测试混为一谈。

## 已实现

- 生成、计数、直接 HTTP 转发共用请求头构造函数；默认不透传未知 SDK 头，鉴权使用上游凭据，管理员显式自定义头保留最后覆盖权限。
- 直接 HTTP 转发克隆入站请求，清除入站 Host、RequestURI、Close、Trailer 和手工分帧信息，不再清空调用方共享的 Header map。
- Antigravity agent 正文补缺失的 systemInstruction.role=user；纯函数声明容器改为一声明一容器，保留声明顺序与内容；带额外作用域配置的容器保留。
- Gemini 不再注入 Claude VALIDATED 使用的 reason/_ 必填占位参数；Claude 兼容、现有工具 schema 清理保留。
- 65535/1024 缺省值仅用于 CLI 1.5.10 实测的 gemini-3.1-pro-preview，其他模型不由此推导默认值；显式配置及原有思考转换保持。
- 正文归一化位于 payload override 之前，管理员最终值不会被之后恢复。

## 验证

新增测试覆盖未知 Responses/Chat 语义等价请求、随机 SDK 头不泄漏、原请求不被转发代码修改、显式自定义头、工具容器作用域、无参数/可选参数工具、payload override 和未实测模型缺省值。

以下相关包完整测试通过：executor、executor/helps、misc、Antigravity Claude/Gemini/Interactions/Chat/Responses translators。服务端和独立探针构建通过。

真实 Antigravity CLI 1.5.10、DSH 0.2.2-rc.1 使用已有安装，在只有 lo 的网络命名空间运行，全部凭据虚构，响应固定。8 项真实客户端读文件/原会话续接检查及 2 项陌生协议正文检查通过；没有运行跨客户端工具交换实验。

原生 CLI gateway 与 DSH 经 CPA 的接收端完整请求头均为：Host、User-Agent、Content-Length、Authorization、Content-Type、Accept-Encoding。原生 UA 带 auth_method=gateway，OAuth CCPA UA 不声明该字段。Host 与长度是当前目标和正文对应的动态值。普通出口在本次修改前已经重建 UA，本轮主要收敛其他路径和补齐正文结构，不能将既有能力记作新增。

DSH 修改前后对照：系统角色从缺失变为 user，工具从一个容器 23 个声明变为 23 个单声明容器；23 个工具名称和描述均保留，新增加的 reason/_ 为零。原有兼容清理仍改变 7 个工具的参数格式：bash、edit、list_agents、todo_write、update_goal、workflow、write，主要涉及 enum 和 additionalProperties；未作为本轮的未知提示词处理而重写。

## 自审和剩余差异

- 本轮使用生产执行器/翻译器探针，不等价于完整 CPA API router；头部是本地 HTTP 接收端记录，不是原生 OAuth/TLS 抓包。
- Worker 代码、429、重试、凭据轮换未修改；Worker 到真实源站的最终头部未取得。
- 未生成来源不明的 labels，未增加 trajectory/step 状态系统；既有 requestId 五段形态中的 step 和 trajectory 演进仍与原生存在差异。
- 未知提示词保持语义，不会因包含某个客户端名字或目录路径就删除文本。不能因此宣称任意第三方 prompt 与原生 prompt 已完全相同。
- 使用现有架构增加两个小型帮助函数，未另造通用 AST 或客户端识别系统。这样可使两个入口共享出口逻辑，同时保留已有 provider 行为和配置优先级。

完整证据见同目录 evidence/requests.jsonl、comparison.json、summary.json、checksums.json。计划见 PLAN.zh-CN.md；备份和源码位置见 location.json。
