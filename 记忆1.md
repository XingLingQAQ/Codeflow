A. [Time Anchor]
2026-01-10 08:51:43 America/Denver (MST)

UTC Offset: -07:00

Current Year: 2026

B. [Restated Need] (记忆系统深度规划)
目标： 在 Code Flow V10.0 Hook 架构基础上，深度优化“持久化记忆”与“结构化压缩”流程。

核心功能：

全量向量化存储： 利用 Hook 实时将对话碎片化并存入向量数据库。
压缩前导图生成： 在 Context 压缩触发前，自动提取实体、决策与逻辑关联，生成“记忆导图”。
语义检索工具： 为主/子 AI 提供专门的记忆查找工具，解决压缩后的信息丢失或任务执行中的模糊性问题。
状态一致性： 确保 Git 回滚时，向量记忆同步执行“撤回”操作。
C. [Workflow] (记忆流转架构)
流式写入 (Persistence): hook_post_response -> 文本分片 (Chunking) -> 向量化 (Embedding) -> 存入向量库。
触发压缩 (Compression Trigger): 监控 Token 阈值 -> 触发 hook_before_compress。
知识建模 (Knowledge Graphing): 调用专用的 Map Agent，将即将被压缩的原始对话转为结构化 JSON/Mermaid 导图。
工具检索 (Retrieval): 当 Sub-AI 调用 query_memory 工具时 -> 混合搜索 (Vector + Keyword) -> 返回上下文。
D. [Final Deliverable] Code Flow V10.0 深度记忆系统规格说明
1. 全量向量化层 (Vector Storage Layer)
逻辑： 每一轮对话都是一次“索引行为”。

Hook 接入点： hook_on_message_complete
实现逻辑：
数据清洗： 过滤掉 CLI 的转义字符和冗余的进度条信息。
元数据绑定： 每个向量块必须绑定 session_id、agent_role、git_commit_hash 和 timestamp。
回滚支持： 当触发 git_rollback Hook 时，GUI 同步发送指令给向量库：DELETE FROM memory WHERE timestamp > target_time。这能确保 AI 不会“记起”未来的、已被回滚的错误尝试。
2. 压缩前导图生成 (Pre-Compression Knowledge Mapping)
逻辑： 在丢失细节前，先留住“骨架”。

Hook 接入点： hook_before_context_compression
执行逻辑：
上下文冻结： 暂停当前所有 AI 进程。
导图提取： 调用一个轻量级模型（如 Qwen-2.5-7B）作为 Map-Agent，执行以下任务：
实体提取： 涉及的文件名、类名、函数名。
决策记录： “为什么选择 Redis 而不是 Memcached”。
逻辑关联： “模块 A 依赖于模块 B 的修改”。
导图持久化： 生成 Mermaid 格式或 JSON 格式的导图，存入 .Code Flow/memory_maps/ 目录下，并更新 GUI 侧边栏的“知识树”。
压缩执行： 执行原生的 /compact 或 rewind 命令，但在新的 Context 头部注入此导图的摘要。
3. 记忆检索工具 (Semantic Retrieval Tool)
逻辑： 为 AI 提供“翻看旧账”的主动权。

工具名称： search_historical_context
适用场景：
子 AI 模糊： Coder AI 被指派修改代码，但不清楚之前的架构决策。
长序列遗忘： 主 AI 因为多次压缩，记不清 50 轮对话前的具体参数定义。
检索策略：
向量搜索： 匹配语义相似的对话。
导图索引： 优先查找“知识导图”中的决策节点，定位到具体的 step_id。
结果重组： 返回的结果不是零散的句子，而是：“在 Step 45 中，您曾提到... 当时代码状态为...”
4. GUI 配置项：记忆治理 (Memory Governance UI)
配置项描述GUI 交互建议Vector Indexing全量向量化开关切换按钮，显示当前索引数量。Auto-Mapping Threshold触发导图生成的 Token 阈值滑块，建议设置在压缩阈值的 90% 处。Map Visualization导图可视化预览提供一个浮动窗口，以交互式图形展示当前 Session 的逻辑流。Cross-Session Memory是否允许子 AI 访问其他对话的记忆多选框，允许跨项目/跨会话知识共享。E. 核心技术细节：状态一致性保证
您提到的 “回滚代码后也要回滚 AI 对话”，在记忆系统中需通过以下逻辑闭环实现：

快照绑定： 每一个 Git Commit 对应一个 Memory_Checkpoint_ID。
联动删除： ```python
def on_gui_rollback(target_commit):
# 1. 代码回滚
git.reset_hard(target_commit)
# 2. 对话回滚 (通过 Hook 发送给各 AI CLI)
for agent in active_agents:
agent.send_command("/rewind", count=calculated_steps)
# 3. 向量库回滚 (清除被回滚掉的伪记忆)
vector_db.delete_after(target_commit.timestamp)
# 4. 导图重构
knowledge_map.revert_to(target_commit.id)

F. 总结：多配置对话系统 (Multi-Config & Multi-Memory)
多对话支持： 每个 Session 拥有独立的向量 Collection。
不同配置： * 对话 A (开发模式): 开启 Coder AI，开启激进的自动总结 (每 5k)，强制生成详细导图。
对话 B (研究模式): 仅 Main AI + Sub AI，禁用自动总结，开启大窗口向量检索 (128k)。
工具触发： 如果 Coder AI 的 Prompt 中包含 [Ambiguity detected]，GUI 通过 Hook 自动静默触发 search_historical_context 并将结果补全到 Coder 的输入中，实现“无感记忆增强”。
G. Next Step
这个规划是否满足了您对“记忆导图”和“深度检索”的架构要求？如果需要，我可以为您编写一份 Sub-AI 如何识别“不明确问题”并自动触发检索工具 的逻辑判断流。

