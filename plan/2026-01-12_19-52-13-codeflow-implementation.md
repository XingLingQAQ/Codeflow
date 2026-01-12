---
mode: plan
cwd: D:/project/CodeFlow
task: Code Flow 智能集成开发环境 - 技术实施计划
complexity: complex
planning_method: builtin
created_at: 2026-01-12T19:52:13+08:00
---

# Plan: Code Flow Agentic IDE 技术实施计划

🎯 任务概述

基于项目设计文档，制定 Code Flow 新一代智能集成开发环境的完整技术实施计划。项目核心目标是构建一个从"命令执行"转向"意图驱动"的 Agentic IDE，包含多层级配置系统、多模型协作编排、双轨记忆架构、原子状态管理等核心能力。

---

📋 执行计划

## Phase 1: 基础架构搭建 (Week 1-2)

### 1.1 项目脚手架初始化
- 创建 Monorepo 结构 (pnpm workspace)
- 配置 TypeScript + ESLint + Prettier
- 搭建 Electron + React 基础框架
- 建立 `packages/gui`、`packages/core`、`packages/shared` 三层架构

### 1.2 Hook Bus 事件系统
- 实现 `IHookManager` 接口
- 开发核心 Hooks：
  - `hook_before_send`
  - `hook_post_response`
  - `hook_on_stream`
- 建立事件订阅/发布机制 (EventEmitter / RxJS)

### 1.3 Claude Adapter 封装
- 实现 `ICliAdapter` 接口
- 封装 Claude API 通信层
- 实现 `send()` / `receive()` / `getHistory()` / `setHistory()` 方法
- 集成流式响应处理

### 1.4 基础对话 UI
- 开发 Chat 组件（用户气泡 + AI 气泡）
- 实现消息列表渲染
- 集成 Markdown 渲染器
- 建立 GUI ↔ Core 通信桥接

---

## Phase 2: 状态与持久化 (Week 3-4)

### 2.1 SQLite 会话存储
- 设计数据库 Schema（sessions / messages / checkpoints）
- 实现 CRUD 操作封装
- 开发会话列表 UI 组件

### 2.2 配置系统实现
- 实现三级配置继承（Global → Session → Role）
- 开发配置面板 UI
- 实现 PAPI 变量路由机制
- 配置持久化与 Session ID 序列化

### 2.3 Git 快照绑定
- 实现 `hook_after_exec` 快照生成
- 开发 Git 操作封装层
- 建立 Snapshot ID ↔ Git Commit 映射
- 实现基础回滚逻辑

---

## Phase 3: 多模型协作 (Week 5-6)

### 3.1 Gemini/Codex Adapter
- 封装 Gemini API Adapter
- 封装 Codex/OpenAI API Adapter
- 统一 Adapter 接口行为

### 3.2 指挥官模式编排
- 实现 `call_coder_agent` 工具定义
- 实现 `consult_sub_expert` 工具定义
- 开发嵌套子对话渲染 UI
- 实现上下文传递 (Context Grafting)

### 3.3 自动总结机制
- 实现 Token 计数器
- 开发 `hook_before_compress` 逻辑
- 实现 80/20 压缩策略
- 集成 Summary Agent

---

## Phase 4: 记忆系统 - 向量层 (Week 7-8)

### 4.1 Memory 1 向量存储
- 集成 Chroma 向量数据库
- 实现流式写入 (`hook_post_response` → Chunking → Embedding)
- 开发元数据绑定（session_id / agent_role / git_commit_hash）

### 4.2 语义检索工具
- 实现 `search_historical_context` 工具
- 开发混合搜索（向量 + 关键词）
- 集成到 AI 工具列表

### 4.3 压缩前导图
- 实现 Map Agent 调用逻辑
- 开发决策骨架提取算法
- 导图持久化存储

---

## Phase 5: 记忆系统 - 图谱层 (Week 9-10)

### 5.1 Memory 2 SAMG 图谱
- 设计 S-P-O 三元组存储格式 (JSON-LD)
- 实现增量提取机制 (`hook_on_message_complete`)
- 开发 `memory_graph.jsonl` 读写层

### 5.2 图谱可视化
- 集成 Cytoscape.js
- 实现自动拓扑渲染
- 开发节点点击回溯功能
- 实现扩展激活 (Spreading Activation) 检索

### 5.3 双轨记忆协同
- 实现 `hybridSearch()` 混合检索
- 开发记忆管理仪表盘 UI
- 实现拖拽归档交互

---

## Phase 6: 全栈回滚与一致性 (Week 11-12)

### 6.1 原子快照完善
- 完善三位一体快照结构（Git + Conversation + Vector）
- 实现向量库同步回滚 (`DELETE WHERE timestamp > target`)
- 实现图谱版本回滚

### 6.2 一致性保障
- 实现执行锁机制
- 开发语义预检校验
- 实现 `hook_restore_state` 完整逻辑

### 6.3 Git 时间轴 UI
- 开发右侧 Git 时间轴组件
- 实现 Diff 预览悬浮窗
- 实现一键回滚按钮

---

## Phase 7: 智能干预功能 (Week 13-14)

### 7.1 渐进式记忆披露
- 实现 `hook_on_user_input_submitted` 语义预检
- 开发建议性弹窗 UI
- 实现动态上下文注入

### 7.2 模型热切换
- 开发 Hot-Swap 下拉菜单
- 实现重试/接力逻辑
- 集成 PAPI 变量联动
- 实现上下文平滑迁移

### 7.3 记忆预警指示灯
- 开发输入框边框变色逻辑
- 实现气泡操作栏
- 开发 Plan 模式批量修改功能

---

## Phase 8: 高级功能与优化 (Week 15-16)

### 8.1 AST 上下文构建器
- 集成 Tree-sitter 解析引擎
- 开发交互式 AST 树组件
- 实现 Token 级精准选择

### 8.2 性能优化
- 实现前缀缓存 (Prefix Caching)
- 开发观察屏蔽策略
- 实现上下文预算字典
- 开发图谱衰减算法 (BLA)

### 8.3 语义冲突检测
- 实现 S-P-O 冲突探测
- 开发语义 Diff 视图
- 实现冲突分级分类

---

## Phase 9: 安全与企业级 (Week 17+)

### 9.1 权限隔离
- 实现隔离式上下文容器
- 开发 RBAC 访问控制
- 实现 I/O 验证层

### 9.2 隐私感知 RAG
- 实现 AES-CBC 加密 (Method A)
- 实现链式密钥衍生 (Method B)
- 开发自动 PII 脱敏 Hook

### 9.3 审计与合规
- 实现不可篡改审计日志
- 开发隐私模式分流
- 集成 BFT 共识协议

---

⚠️ 风险与注意事项

### 技术风险
- **向量数据库选型**：Chroma 适合本地开发，生产环境可能需迁移至 Milvus/Pinecone
- **Electron 性能**：大型项目可能遇到内存瓶颈，需关注分片加载策略
- **多模型 API 兼容性**：不同厂商 API 行为差异需要 Adapter 层充分抽象

### 依赖风险
- Claude/Gemini/Codex API 可用性与配额限制
- Tree-sitter 对不同语言的支持程度
- Cytoscape.js 大规模节点渲染性能

### 安全风险
- MCP 配置文件提示词注入攻击
- 敏感信息（API Key）泄露
- 多智能体环境下的权限边界模糊

---

📎 参考

- `Code Flow：新一代智能集成开发环境.md` - 完整设计文档
- `1.md:1-132` - GUI 功能规划书
- `2.md:1-107` - Hook-Verse 架构规划
- `3.md:1-60` - 可视化 Debug 面板设计
- `4.md:1-60` - 记忆管理仪表盘设计
- `5.md:1-59` - 智能干预与动态编排功能规划
- `记忆1.md:1-79` - 深度记忆系统规格
- `记忆2.md:1-80` - 结构化记忆图谱系统规格
- `项目规划.md` - 技术实施规划总览
