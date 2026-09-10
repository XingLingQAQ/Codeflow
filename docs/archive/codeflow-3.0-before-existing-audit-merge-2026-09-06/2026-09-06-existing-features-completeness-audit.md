# 现有功能完整性专项审查

> 日期：2026-09-06。
> 范围：当前工作树已存在的 HTTP 接口、设置项和后端库接口，包含用户尚未提交的源码修改。文档提交基线为 d8637a9，但本报告不是只审查该提交中的源码。
> 方法：读取入口、实现、调用者及相关测试，静态推导触发条件；没有运行测试、启动应用、调用模型或修改产品源码。下文的“确认”指代码路径已核实，不表示已动态复现。
> 排除：新增 Run/ExecBackend、书签、MCP server、统一审批、未来调度器等 3.0 待实现能力；不以这些能力不存在作为本轮发现。也不把测试专用 mock 或明确标注待交付的页面重复记为缺陷。
> 本轮记录 14 个独立问题。E 编号仅用于本报告，不修改 I-01 至 I-70 或已提交的实施计划。

## 1. 结论与范围

现有代码中同时存在三种不完整：接口返回了成功形状但未履行承诺，正常路径完成但失败恢复缺失，以及已经实现的能力没有接到实际入口。以下问题都可以在现有功能内修复，不需要先建设新的 Run 系统。

| 编号 | 优先级 | 现有功能 | 确认的问题 | 可达范围 |
|---|---|---|---|---|
| E-01 | P1 | 项目工作流回放 | 未验证会话属于该项目，能混入其他项目的 trace/审计 | 已注册 HTTP 接口 |
| E-02 | P1 | 原子记忆更新/删除 | 正文已提交，向量同步失败后缺恢复路径 | 已初始化的持久化服务与 HTTP 接口 |
| E-03 | P1 | Skill 版本管理 | 当前版本与历史分两次提交，崩溃可丢失回滚版本 | 已初始化的 SQLite registry 与 HTTP 接口 |
| E-04 | P2 | API 模型流式响应 | 错误/截断/非正常 EOF 也返回 Done 与 stop | 现有 adapters.Stream 库接口 |
| E-05 | P2 | API 模型流取消 | 消费者停读后发送阻塞，取消无法释放 goroutine/body | 现有 adapters.Stream 库接口 |
| E-06 | P2 | 会话摘要/上下文压缩 | HTTP 仍接演示实现，保留条数/目标 token 未兑现 | 生产 bootstrap 与 HTTP 接口 |
| E-07 | P2 | 文本压缩输入处理 | 比例越界可 panic，中文可能被按字节截坏 | 已注册 HTTP 接口 |
| E-08 | P2 | Skill 回滚 | 旧版本的空触发词/空阶段标签无法恢复 | 已注册 rollback 接口 |
| E-09 | P2 | PAPI 配置持久化 | 保存失败仍改内存，并发成功也可内存/数据库分叉 | 已注册 PAPI CRUD/hotswap 接口 |
| E-10 | P2 | PAPI 类别冲突检查 | 检测区分大小写、解析不区分，漏报且选取不稳定 | 已注册 conflicts/resolve 接口 |
| E-11 | P2 | 原子记忆条件检索 | 先限 TopK 后过滤，存在匹配项却返回空页 | 已注册 search 接口 |
| E-12 | P2 | 原子记忆批量衰减 | 忽略逐行执行错误，回执可能将失败项计为成功 | 已注册 decay 接口 |
| E-13 | P2 | 设置中的实验开关 | 只更新页面临时状态，不控制功能 | 当前默认 Shell 设置页 |
| E-14 | P2 | Git 状态/提交差异 | 普通未暂存路径、重命名及含空格路径解析不正确 | 现有 GitManager 库接口 |

P1 指优先处理的数据边界或不可恢复版本问题。P2 指已有功能的实际行为不符合接口含义，不等同于本轮已经观察到线上故障。库接口条目不会被包装成“默认界面上已经可以操作该能力”；当前生产 UI 的调用覆盖与接口正确性是两件事。

## 2. 具体问题

### E-01 [P1] 项目回放没有校验会话归属

- 入口：`GET /api/v1/workflows/:projectId/replay?session_id=...`。handler 只检查 projectId 非空，然后原样传递 session_id。
- 证据：[workflow handler](D:/Project/Codeflow/backend/internal/api/handlers/workflow.go:57)、[GetReplay](D:/Project/Codeflow/backend/internal/workflow/service.go:311)、[按会话取审计](D:/Project/Codeflow/backend/internal/workflow/service.go:493)。服务读取项目后，直接对调用者给出的 sessionID 取 trace；没有检查它是否在该项目的 sessionIDs 中。审计也只按 sessionID 过滤。
- 触发：A、B 两项目各有会话，在 A 的 replay URL 传 B 的 sessionID。返回结构仍标 projectID=A，trace/部分审计却来自 B；A 的 Flow 事件还会一起合并，形成混合回放。
- 同域缺口：[loadRelevantAgents](D:/Project/Codeflow/backend/internal/workflow/service.go:475) 在项目没有任何 sessionID 时返回全部 Agent，又把其 sessionID 加回项目快照。旧数据或未形成会话关联的项目会因此扩大统计范围。
- 修复边界：在现有 workflow service 内验证项目与会话关系；没有绑定应返回空范围，而非退回全局集合。即使当前是单用户工具，项目回放和来源归属也不能混淆。
- 建议回归：跨项目 session 拒绝；无会话项目不显示全局 Agent；正常项目 trace/审计/Flow 事件均来自同一项目。
- 与 3.0 区别：这是当前 `/workflows/...` 的行为错误，不是新 Run identity 还未实现。

### E-02 [P1] 原子记忆更新/删除只完成了正文操作

- 证据：[Update](D:/Project/Codeflow/backend/internal/memory/atomic_service.go:390) 先提交 SQL UPDATE，随后 [replaceVector](D:/Project/Codeflow/backend/internal/memory/atomic_service.go:648) 先删除旧向量再 Add 新向量；[Delete](D:/Project/Codeflow/backend/internal/memory/atomic_service.go:451) 先删除正文，之后才删向量。
- 生产接线：[main](D:/Project/Codeflow/backend/cmd/codeflow-server/main.go:154) 初始化正文和向量的两个 SQLite 文件；不是只存在于测试里的 service。
- 触发一：更新正文成功，删除旧向量成功，添加新向量失败。接口报错，但正文已经改变、索引已经缺失，搜索不能找到该条记忆。
- 触发二：正文删除成功，向量删除失败。再次调用 Delete 会因正文不存在提前返回 ErrAtomicMemoryNotFound，不再到达向量清理；残留向量还可能占掉后续搜索的 TopK。
- 缺口：当前路径没有持久的索引修复回执，也没有对这些中间态的重试协调。Add 有部分补偿，不代表 Update/Delete 已具有同样保证。
- 修复边界：保留权威正文，并持久化可重入的索引同步/清理意图；删除重试必须还能处理索引残留。不能把两个现有 SQLite 文件说成一个事务。
- 建议回归：向量 Delete/Add 分别故障；重启后恢复；重复删除可完成清理；失败后读取和搜索的状态可解释。

### E-03 [P1] Skill 更新与历史归档不是同一提交

- 证据：[registry.Update](D:/Project/Codeflow/backend/internal/skill/registry.go:234) 先 `store.put(s)` 提交新当前版本，再 [archiveVersionLocked](D:/Project/Codeflow/backend/internal/skill/registry.go:246) 保存旧版。底层 [sqliteSkillStore](D:/Project/Codeflow/backend/internal/skill/sqlite_store.go:86) 的当前写入、归档和裁剪均为独立 Exec。
- 触发：新内容已写入 skills 表，但在旧内容进入 skill_versions 表之前进程退出。重开数据库后只能读到新版，最后一版旧内容不在历史里，回滚无法恢复它。
- 错误路径还通过 `_ = r.store.put(prev)` 尝试补偿，忽略补偿失败；历史超过 20 条时，内存已经裁剪，按 slice 长度截断也不能完整恢复原历史集合。
- 修复边界：在现有 skills.db 内用同一事务写当前版、归档旧版并裁剪；事务成功后发布内存状态。这不需要等待 S12 单库迁移。
- 建议回归：在当前写入/归档/裁剪之间分别注入失败或重启，核对当前内容和完整历史；特别覆盖达到 20 条上限后的失败。

### E-04 [P2] 流式响应把不完整输出记成正常结束

- 证据：[Claude Stream](D:/Project/Codeflow/backend/internal/adapters/claude.go:487)、[OpenAI Stream](D:/Project/Codeflow/backend/internal/adapters/openai.go:129)、[Gemini Stream](D:/Project/Codeflow/backend/internal/adapters/gemini.go:107) 均在 scanner 循环结束后无条件追加 assistant 历史、发送 Done=true，并通知 FinishReason="stop"。
- 三者没有检查 scanner.Err；JSON 解析失败直接 continue；公开的 [StreamChunk](D:/Project/Codeflow/backend/internal/adapters/types.go:63) 只有 Delta/Index/Done，没有异步错误或真正终止原因。
- 触发：响应读到一半断开、单帧超过 scanner 默认限制、provider 在流内返回错误事件，均可能生成一个看起来正常完成的部分回答。空白成功和真实结束也不能可靠区分。
- 修复边界：在现有 adapters 层区分协议完成、异常 EOF、扫描失败、provider error 和取消；只有可信完成才能记录成功。明确流式 usage/finish metadata，而不是固定填 stop。
- 建议回归：每种 provider 的正常完成帧、流内 error、关键帧损坏、超长帧和中途断开；历史不得把截断回答标成完整成功。
- 与 3.0 区别：这是当前 HTTP 模型适配库，不是未来 Claude/Codex/Gemini CLI adapter。

### E-05 [P2] 流式取消不能解除通道发送阻塞

- 证据：[OpenAI 发送 chunk](D:/Project/Codeflow/backend/internal/adapters/openai.go:157)、[Gemini](D:/Project/Codeflow/backend/internal/adapters/gemini.go:129)、[Claude](D:/Project/Codeflow/backend/internal/adapters/claude.go:515) 都直接 `ch <- chunk`，最终 Done 发送也一样。
- 触发：调用者取消并停止读取，provider 已输出超过容量为 100 的 channel。goroutine 阻塞在发送语句上，ctx 取消 HTTP 请求不能让该语句返回；defer close(ch)/resp.Body.Close() 也到不了。
- 修复边界：所有通道发送和退出路径都选择 ctx.Done，明确定义消费者关闭/取消责任；与 E-04 的错误终结统一，但需要独立的资源释放验收。
- 建议回归：输出超过缓冲容量，消费者中途退出并取消；等待 goroutine 与响应 body 在限定时间内关闭。

### E-06 [P2] HTTP 摘要仍是演示实现，关键参数未使用

- 证据：[main](D:/Project/Codeflow/backend/cmd/codeflow-server/main.go:245) 给生产服务注入 `NewSummarizerService()`；[HTTP handler](D:/Project/Codeflow/backend/internal/api/handlers/summarize.go:14) 直接调用它。
- [SummarizeConversation](D:/Project/Codeflow/backend/internal/summarize/service.go:37) 计算 preserveRecent 后不使用；[generateSummary](D:/Project/Codeflow/backend/internal/summarize/service.go:192) 只是“消息数量 + 最多三条英文关键词句子”；compression_target 直接原样回填到 CompressionRatio，并非测量结果。
- `CompressRequest.target_tokens` 在 [类型](D:/Project/Codeflow/backend/internal/summarize/types.go:53) 中公开，但 CompressContext 不读取它。相同内容设置不同 target_tokens 不会改变该控制路径。
- 仓库另有 [Compressor](D:/Project/Codeflow/backend/internal/summarize/compressor.go:41)，支持消息级策略及可选模型摘要；不能因此宣称 HTTP 接口已经接上该实现。
- 修复边界：选定现有摘要实现的真实入口，兑现保留最近消息与目标预算，输出实际 token/压缩结果；若仅支持本地提取，明确返回该模式和限制。
- 建议回归：不同保留条数必须改变结果；目标预算约束可观察；中文决策和否定/撤销内容不能被简单关键词计数伪装成可靠总结。

### E-07 [P2] 压缩接口缺少范围与 Unicode 边界处理

- 证据：[splitPoint](D:/Project/Codeflow/backend/internal/summarize/service.go:78) 按 len(string) 直接切分；[summarizeText](D:/Project/Codeflow/backend/internal/summarize/service.go:211) 按计算出的字节下标截断。类型和 handler 没有比例区间校验。
- 触发一：非空 context 配 preserve_recent_pct=2，会产生负下标；compression_ratio>1 同样可能使 targetLength 为负，进入 panic 后由 HTTP recovery 返回 500，而非字段错误。
- 触发二：context="你好" 使用默认比例，切分点可落到 UTF-8 字符内部，summary/recent_context 中出现非法字节，JSON 编码时变成替换字符。
- 修复边界：入口校验比例与预算，按合法字符/消息/token 边界裁剪。即便保持本地提取算法，也必须先保证输入输出合法。
- 建议回归：负数/超过 1/极小文本/中文/多字节符号，核对错误为 4xx 或合法结果，不能只测英文长文本。

### E-08 [P2] Skill 回滚无法恢复为空的匹配规则

- 证据：[RollbackVersion](D:/Project/Codeflow/backend/internal/skill/registry.go:348) 用 append([]string(nil), snap.Triggers...) 构造更新；旧版本为空时结果为 nil。随后 [Update](D:/Project/Codeflow/backend/internal/skill/registry.go:223) 仅在字段非 nil 时修改，StageTags 同样如此。
- 触发：创建无 triggers/stage_tags 的 Skill，更新为有过滤条件，再回滚到初始版本。正文恢复了，新增的触发词和阶段限制仍保留，Match/Inject 行为不等于旧版本。
- 修复边界：区分“不修改字段”和“恢复为空集合”，回滚按明确快照字段恢复；不要用 nil 同时表示两种语义。
- 建议回归：空 -> 非空 -> 回滚为空，以及非空 -> 空 -> 回滚为非空；同时核对 Match，而不只核对 Body。已读版本测试主要验证正文恢复。

### E-09 [P2] PAPI 的失败与并发保存会造成状态分叉

- 证据：[DefinePAPIVariable](D:/Project/Codeflow/backend/internal/config/service.go:579)、DeletePAPIVariable、HotSwapPAPI 先改 papiManager，再单独调用持久化函数；只有数据库写函数内部持有 s.mu，整个操作没有共同串行边界。
- 失败触发：变量内存已替换，数据库保存失败，HTTP 返回错误，但随后的 Get/Resolve 已使用新值；重启后又加载旧值。Delete 失败也会暂时从内存移除仍存在于数据库的变量。
- 并发触发：A 发布内存 A 后暂停；B 发布内存 B 并写盘 B；A 再写盘 A。两个调用都可能成功，最后内存 B、数据库 A。
- 修复边界：针对同一配置操作协调内存与持久化版本，成功提交后发布内存；保存失败保持原事实，并处理并发覆盖。单纯增加数据库互斥锁不够。
- 建议回归：写盘失败后即时查询/重开一致；并发修改同一变量后内存与重开结果一致。
- 与 3.0 区别：不涉及资产配置的新继承模型，只修当前 PAPI CRUD 的一致性。

### E-10 [P2] PAPI 类别冲突检查和解析采用不同规则

- 证据：[ResolveByCategory](D:/Project/Codeflow/backend/internal/config/papi.go:103) 遍历 Go map 并 EqualFold；[DetectConflicts](D:/Project/Codeflow/backend/internal/config/papi.go:205) 则用原始 category 字符串作 map key。
- 触发：变量 A 标 backend，B 标 Backend。冲突接口不报告冲突，但解析任意大小写的 backend 时两项都匹配，结果取决于 map 遍历顺序。
- 修复边界：定义唯一类别规范化规则，冲突检测、创建/更新、解析共同使用；对多个合法候选给显式冲突/优先级，不能随机选择。
- 建议回归：大小写、前后空白、同一变量重复类别、两个变量同类别；结果须稳定或明确拒绝。

### E-11 [P2] 原子记忆过滤发生在 TopK 截断之后

- 证据：[Search](D:/Project/Codeflow/backend/internal/memory/atomic_service.go:165) 向量查询只取 limit+offset（默认 10），仅提前传 session 过滤；[matchAtomicFilters](D:/Project/Codeflow/backend/internal/memory/atomic_service.go:730) 对 folder/tags/time 的过滤在候选截断之后执行。
- 触发：最高分记录不符合指定 tag，第二名符合；limit=1 时仅取到第一名，再过滤掉，返回空列表。增加分页或指定较窄日期也会出现存在匹配结果却无法取得的情况。
- 修复边界：支持前置过滤，或分批检索直到足够匹配项/确定候选耗尽，并正确计算过滤后分页；不能简单放大一个固定 TopK 声称解决。
- 建议回归：高分不匹配、低分匹配、跨页过滤、多条件组合，验收符合条件的第一页与第二页都可取到。

### E-12 [P2] 热度衰减忽略逐行更新失败

- 证据：[ApplyHeatDecay](D:/Project/Codeflow/backend/internal/memory/atomic_service.go:531) 对 stmt.ExecContext 的 result/error 均未处理，最后返回 len(items)，而不是已成功更新数量。
- 触发：某行执行失败但事务仍可提交的 SQLite 错误，或采集后记录被其他操作移除，都没有逐项检查。返回数量依据计划更新集合，不足以证明实际更新成功。
- 修复边界：检查每次执行错误，失败回滚或返回明确部分结果；定义 affected 数含义，并检查遍历 rows.Err。不能只有 tx.Commit 检查。
- 建议回归：单行失败、并发移除、完整成功；失败不能作为正常“已衰减 N 条”返回。具体 SQLite 故障行为需后续动态用例核实。

### E-13 [P2] 设置页的实验开关只改变显示值

- 证据：[Settings](D:/Project/Codeflow/apps/workbench/src/shell/pages/Settings.tsx:33) 中 autoSnapshot/livePreview 都是组件 useState，唯一消费者是 [Switch](D:/Project/Codeflow/apps/workbench/src/shell/pages/Settings.tsx:141)。未接 store、后端配置或功能判断。
- 触发：关闭阶段自动快照或打开 Live Preview 后，切出页面再回来恢复默认；功能执行没有读取这些值。
- 修复边界：已可控制的功能接真实配置；尚未交付的功能应禁用或明确不可用。不能提供可切换的设置却让用户以为已经改变行为。
- 建议回归：更改设置、离开页面、重载、观察对应功能；外观主题和动效已有 store，不应连带判为假开关。
- 与 3.0 区别：不要求现在实现书签/Live Preview，只处理当前已露出的控制项未生效问题。

### E-14 [P2] Git 输出解析破坏状态路径和重命名信息

- 证据：[execGit](D:/Project/Codeflow/backend/internal/git/manager.go:51) 对整个输出 TrimSpace；[Status](D:/Project/Codeflow/backend/internal/git/manager.go:87) 再按 porcelain 固定列取 line[:2] 和 line[3:]。
- 常见触发：首行是未暂存修改 ` M foo.go`，TrimSpace 后成为 `M foo.go`，固定列提取得到文件名 `oo.go`，首字符丢失。
- 第二处证据：[DiffBetween](D:/Project/Codeflow/backend/internal/git/manager.go:449) 用 strings.Fields 拆文件名，且只识别状态字面值 R，不能正确处理 R100/R087 以及旧/新两个路径。带空格路径也会被截断。
- 修复边界：保留结构化输出字节，使用 Git 的 NUL 分隔状态/diff 格式并解析完整状态与新旧路径；不要通过全局 TrimSpace/Fields 处理文件列表。
- 建议回归：首行 unstaged 修改、空格/中文/换行文件名、纯 rename 和 rename+edit，返回路径应与实际文件精确一致。
- 与 3.0 区别：这是已存在 GitManager.Status/DiffBetween 的正确性，不是未来 worktree 或合入日志缺失。

## 3. 本轮排除与剩余边界

以下内容经过初查，但没有当作新的已确认功能缺陷重复计数：

- 新 Shell 的 Config/Plugins 页面明确标 M5/M6 待交付，3.0 已有配置整合或后置记录；这里只保留 E-13 的可操作但不生效控件问题。
- disclosure.MockSearcher 明确是测试用搜索器，不能仅凭“模拟搜索”注释认定生产搜索在造假。
- 没有自研工具循环、旧聊天无真实执行、WS 无持久回放、书签/MCP/审批不存在，均已在既有计划中，本轮不再列入。
- hotswap/cache/retriever 部分实现目前缺主程序调用者，属于库与接线范围问题；本轮没有把这些目录的存在直接当成交付，也没有为未启用路径下更多推断单独报缺陷。
- Gemini 流式传输格式、真实 provider 协议版本、桌面运行表现尚未动态核验，不把静态阅读当真实环境 smoke。

本报告不是全仓“已经无其他问题”的证明。下一轮适合继续查现有 SAMG 激活/衰减、Hook 配置与超时、旧插件/集成生命周期以及各导出接口的完整性；这些本轮仅做了入口扫描，未形成足够完整的逐项证据。

修复建议优先级：先 E-01/E-02/E-03 的项目边界与持久化一致性，再处理真实接口的失败语义、配置/回滚/查询正确性。修复应落在现有模块和现有入口，不先扩展新的 3.0 产品范围。
