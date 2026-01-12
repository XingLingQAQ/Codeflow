# Code Flow 技术实施规划

> 新一代智能集成开发环境（Agentic IDE）

---

## 一、项目概述

### 1.1 核心定位

Code Flow 是一个从"命令执行"转向"意图驱动"的智能集成开发环境。其目标是构建"计算外皮层"（Computational Exocortex），将用户的自然语言意图转化为可执行的语义路径。

### 1.2 核心价值

| 传统模式痛点 | Code Flow 解决方案 |
|-------------|-------------------|
| 状态碎片化 | 代码-对话-向量 三位一体原子快照 |
| 认知过载 | 分层记忆架构 + 自动总结机制 |
| 反馈滞后 | ReAct 循环 + 实时干预机制 |
| 上下文丢失 | 双轨记忆系统（向量 + 图谱） |

### 1.3 核心术语

- **原子快照 (Atomic Snapshot)**：包含 Git Commit + 对话状态 + 向量指针的复合体
- **S-P-O 三元组**：主-谓-宾 结构化知识片段
- **MCP 协议**：Model Context Protocol，系统的"USB-C 接口"
- **PAPI**：Prompt AI-Profile Interface，提示词配置文件接口

---

## 二、系统架构

### 2.1 架构总览

```
┌─────────────────────────────────────────────────────────────────┐
│                         GUI Layer (Electron/React)              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐   │
│  │ Chat UI  │ │ Plan View│ │ Memory   │ │ Context Builder  │   │
│  │          │ │          │ │ Dashboard│ │ (AST Tree)       │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                      Hook Bus (Event Emitter)                   │
│  hook_before_send | hook_post_response | hook_on_stream | ...   │
├─────────────────────────────────────────────────────────────────┤
│                    Adapter Layer (ICliAdapter)                  │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐            │
│  │ ClaudeAdapter│ │ GeminiAdapter│ │ CodexAdapter │            │
│  └──────────────┘ └──────────────┘ └──────────────┘            │
├─────────────────────────────────────────────────────────────────┤
│                      Memory Layer                               │
│  ┌──────────────────────┐  ┌──────────────────────┐            │
│  │ Memory 1: Vector DB  │  │ Memory 2: SAMG Graph │            │
│  │ (Episodic Memory)    │  │ (Semantic Memory)    │            │
│  └──────────────────────┘  └──────────────────────┘            │
├─────────────────────────────────────────────────────────────────┤
│                      State Layer                                │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐            │
│  │ SQLite       │ │ Git Snapshots│ │ Checkpoints  │            │
│  │ (Sessions)   │ │ (Code State) │ │ (Atomic Bind)│            │
│  └──────────────┘ └──────────────┘ └──────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 模块职责与技术选型

| 模块 | 技术选型 | 职责 |
|------|----------|------|
| **GUI** | Electron + React + TypeScript | 主控界面、配置面板、对话流渲染 |
| **Hook Bus** | Node.js EventEmitter / RxJS | 统一事件分发、生命周期管理 |
| **Adapter** | TypeScript Classes | 封装 Claude/Gemini/Codex API |
| **Memory 1** | Chroma / Milvus + SQLite | 向量存储 + 元数据索引 |
| **Memory 2** | JSON-LD + Cytoscape.js | S-P-O 三元组 + 图谱可视化 |
| **State** | SQLite + Git | 对话历史 + 代码快照 + 原子绑定 |

---

## 三、核心功能模块

### 3.1 功能 1：多层级颗粒度配置系统

#### 3.1.1 配置层级

```
CLI 参数 > 角色配置 > 会话配置 > 全局配置
```

#### 3.1.2 配置结构

```typescript
interface ConfigHierarchy {
  global: {
    default_model: string;           // 默认模型底座
    api_pool: APIChannel[];          // API 渠道池
    public_mcp: string[];            // 公共 MCP 工具
  };
  session: {
    session_id: string;
    mode: 'development' | 'research' | 'creative';
    override_model?: string;
  };
  role: {
    main: RoleConfig;                // 指挥官配置
    coder: RoleConfig;               // 代码员配置
    sub: RoleConfig;                 // 专家配置
  };
}

interface RoleConfig {
  model: string;
  temperature: number;
  top_p: number;
  api_channel: string;
  mcp_tools: string[];               // 专属 MCP 挂载
  system_prompt: string;
}
```

#### 3.1.3 PAPI 系统

- **变量级任务路由**：`${BACKEND_EXPERT}` 自动映射至最适合的模型
- **热切换支持**：对话中途实时更换底层模型
- **配置安全**：沙箱策略 + 数字签名验证 + 环境脱敏

---

### 3.2 功能 2：智能协作编排与多模型调度

#### 3.2.1 指挥官模式 (Commander)

```
用户 ──► Main AI (指挥官) ──► call_coder_agent ──► Coder AI
                          └─► consult_sub_expert ──► Sub AI
```

- **单一对话入口**：仅 Main AI 与用户直接交互
- **工具化代理调用**：子智能体封装为标准 Tool
- **嵌套子对话渲染**：UI 层隔离展示执行轨迹

#### 3.2.2 自动对话总结机制

- **触发条件**：Token > 20k
- **压缩策略**：前 80% 语义摘要 + 后 20% 原始对话
- **保留优先级**：架构决策 > 未修复 Bug > 变量定义 > 工具日志

#### 3.2.3 任务级模型热切换

| 场景 | 操作 |
|------|------|
| 重试 | 步骤失败 → Hot-Swap 切换模型 → /rewind → 重新请求 |
| 接力 | 规划完成 → 手动切换低成本模型 → 执行编码 |

---

### 3.3 功能 3：原子状态管理与全栈双向回滚

#### 3.3.1 原子快照结构

```typescript
interface AtomicSnapshot {
  id: string;                        // UUID
  timestamp: Date;
  git_commit_hash: string;           // 代码状态
  conversation_state: {              // 对话状态
    main: Message[];
    coder: Message[];
    sub: Message[];
  };
  vector_index_pointer: string;      // 向量库偏移量
  memory_graph_version: number;      // 图谱版本
}
```

#### 3.3.2 全栈回滚逻辑

```python
def on_gui_rollback(target_commit):
    # 1. 代码回滚
    git.reset_hard(target_commit)

    # 2. 对话回滚
    for agent in active_agents:
        agent.send_command("/rewind", count=calculated_steps)

    # 3. 向量库回滚
    vector_db.delete_after(target_commit.timestamp)

    # 4. 图谱回滚
    memory_graph.revert_to(target_commit.id)
```

#### 3.3.3 一致性保障

- **执行锁**：Git 事务未结束前禁用回滚按钮
- **语义预检**：恢复后自动校验 Context 与代码哈希匹配

---

### 3.4 功能 4：高级记忆架构

#### 3.4.1 记忆 1：深度持久化向量系统

| 组件 | 功能 |
|------|------|
| **流式写入** | `hook_post_response` → 文本分片 → 向量化 → 存储 |
| **压缩前导图** | `hook_before_compress` → Map Agent 提取决策骨架 |
| **语义检索** | `search_historical_context` 工具供 AI 调用 |
| **状态同步** | Git 回滚时同步清除"伪记忆" |

#### 3.4.2 记忆 2：结构化原子图谱系统 (SAMG)

```typescript
interface MemoryTriple {
  timestamp: Date;
  step_id: number;
  nodes: {
    id: string;
    label: string;
    type: 'Class' | 'Function' | 'Requirement' | 'Decision';
  }[];
  edges: {
    source: string;
    target: string;
    relation: 'solves' | 'depends_on' | 'implements' | 'conflicts_with';
    strength: number;  // 0-1
  }[];
  context_summary: string;
}
```

**存储格式**：`memory_graph.jsonl`

```json
{
  "timestamp": "2026-01-10T17:50:00Z",
  "step_id": 42,
  "nodes": [
    {"id": "auth_mod", "label": "AuthModule", "type": "Class"},
    {"id": "jwt_fix", "label": "JWT Security Fix", "type": "Requirement"}
  ],
  "edges": [
    {"source": "auth_mod", "target": "jwt_fix", "relation": "solves", "strength": 0.9}
  ],
  "context_summary": "用户要求修复 JWT 验证漏洞，涉及 AuthModule。"
}
```

#### 3.4.3 双轨记忆协同

| 记忆类型 | 存储形式 | 检索方式 | 适用场景 |
|----------|----------|----------|----------|
| Memory 1 | 向量嵌入 | 语义相似度 | 模糊意图匹配 |
| Memory 2 | S-P-O 图谱 | 图遍历 + 扩展激活 | 因果关联推理 |

---

### 3.5 功能 5：智能干预与模型热切换

#### 3.5.1 渐进式记忆披露 Hook

```typescript
// hook_on_user_input_submitted
async function preflightCheck(input: string): Promise<MemoryMatch[]> {
  // 1. 提取关键词
  const keywords = extractKeywords(input);

  // 2. 向量库碰撞
  const vectorMatches = await vectorDB.search(keywords);

  // 3. 规则库匹配
  const ruleMatches = await matchRules(keywords);

  // 4. 返回建议
  return [...vectorMatches, ...ruleMatches];
}
```

**UI 交互**：
- 强匹配：命中 `.claude/rules` 中的规则文件
- 弱匹配：命中历史会话簇
- 弹窗选项：[查看内容] [确定加载] [忽略并发送]

#### 3.5.2 记忆预警指示灯

- 输入框边框变色提示高价值匹配
- 气泡操作栏显示参考的规则节点
- 支持基于气泡分叉对话支线

---

## 四、核心接口定义

### 4.1 Hook Manager 接口

```typescript
interface IHookManager {
  // 生命周期 Hooks
  hook_before_send(payload: RequestPayload): Promise<RequestPayload>;
  hook_post_response(response: AIResponse): Promise<void>;
  hook_on_stream(chunk: StreamChunk): void;

  // 上下文治理 Hooks
  hook_before_compress(context: Context): Promise<DecisionSkeleton>;
  hook_on_message_complete(message: Message): Promise<void>;

  // 状态管理 Hooks
  hook_after_exec(result: ExecResult): Promise<SnapshotID>;
  hook_restore_state(snapshotId: SnapshotID): Promise<void>;

  // 记忆检索 Hooks
  hook_on_user_input_submitted(input: string): Promise<MemoryMatch[]>;
}
```

### 4.2 CLI Adapter 接口

```typescript
interface ICliAdapter {
  // 基础通信
  send(prompt: string, options?: SendOptions): Promise<AIResponse>;
  receive(): AsyncGenerator<StreamChunk>;

  // 上下文管理
  getHistory(): Message[];
  setHistory(messages: Message[]): void;

  // 状态控制
  rewind(steps: number): Promise<void>;
  compact(): Promise<void>;

  // 配置
  configure(config: AdapterConfig): void;
}
```

### 4.3 Memory Service 接口

```typescript
interface IMemoryService {
  // 向量层
  indexMessage(message: Message, metadata: Metadata): Promise<void>;
  searchSimilar(query: string, limit: number): Promise<MemoryFragment[]>;
  deleteAfter(timestamp: Date): Promise<void>;

  // 图谱层
  addTriple(triple: MemoryTriple): Promise<void>;
  queryRelations(nodeId: string, depth: number): Promise<GraphPath[]>;
  revertTo(version: number): Promise<void>;

  // 混合检索
  hybridSearch(query: string): Promise<HybridResult>;
}
```

---

## 五、GUI 组件设计

### 5.1 布局结构

```
┌─────────────────────────────────────────────────────────────────┐
│                          顶部工具栏                              │
├──────────────┬──────────────────────────────┬──────────────────┤
│              │                              │                  │
│  左侧边栏     │        主编排流              │    右侧边栏       │
│              │                              │                  │
│  - 会话列表   │  - 用户气泡                  │  - CodexLens    │
│  - 配置面板   │  - Main AI 气泡              │  - Git 时间轴    │
│  - 记忆仪表盘 │  - 协作卡片 (Coder/Sub)      │  - 知识图谱      │
│              │                              │                  │
└──────────────┴──────────────────────────────┴──────────────────┘
```

### 5.2 核心组件

#### 5.2.1 记忆管理仪表盘

- **左侧**：短期记忆池（自动聚类的记忆簇）
- **右侧**：长期规则库（`.claude/rules/` 文件树）
- **交互**：拖拽归档 + 自动提炼 Hook

#### 5.2.2 动态上下文构建器

- 基于 AST 树的交互式组件
- 支持选择函数签名/全量代码
- Token 级精准控制

#### 5.2.3 语义检索引擎 UI

- 全局 `Cmd+K` 指令中心
- FTS + 向量 + 图谱 三位一体搜索
- 分类过滤与结果预览

---

## 六、安全治理

### 6.1 多智能体权限隔离

- **隔离式上下文容器**：每个代理独立的数据分片
- **RBAC 访问控制**：M2M 身份验证机制
- **I/O 验证层**：跨阶段数据脱敏

### 6.2 隐私感知 RAG

| 方案 | 适用场景 | 机制 |
|------|----------|------|
| Method A | 中等安全 | AES-CBC 加密 |
| Method B | 高价值数据 | 链式动态密钥衍生 |

### 6.3 审计追踪

- 不可篡改审计日志（加密哈希链）
- 自动 PII 脱敏 Hook
- 隐私模式分流（绕过模型推理层）

---

## 七、性能优化

### 7.1 前缀缓存 (Prefix Caching)

| Token 规模 | 加速比 |
|------------|--------|
| 10,000 | 44x |
| 100,000 | 569x |

**实现要点**：
- 确定性序列化（JSON 键值对顺序固定）
- 缓存粒度：System Prompt + Project Rules
- 临时 AI 存储 (EAS) 管理 KV 缓存

### 7.2 注意力预算管理

- **观察屏蔽**：隐藏旧的/次要信息，推理成本降低 50%+
- **混合压缩**：常规步骤屏蔽 + 超步阈值全局总结
- **上下文预算字典**：显式 Token 额度分配

### 7.3 图谱衰减算法

- **BLA 衰减**：基于 ACT-R 理论的激活得分
- **动态折叠**：低分节点自动隐藏
- **异步整合**：后台执行图谱维护操作

---

## 八、开发路线图

### Phase 1: MVP - 核心回路验证

**目标**：实现基础的"感知-规划-执行"闭环

| 任务 | 优先级 | 依赖 |
|------|--------|------|
| 项目脚手架搭建 (Electron + React + TS) | P0 | - |
| Hook Bus 事件系统实现 | P0 | - |
| Claude Adapter 封装 | P0 | Hook Bus |
| 基础对话 UI | P0 | Adapter |
| SQLite 会话存储 | P1 | - |
| 单文件读写 MCP 工具 | P1 | Adapter |

### Phase 2: 生产化就绪 - 记忆与持久化增强

**目标**：解决系统的长效稳定性与记忆可靠性

| 任务 | 优先级 | 依赖 |
|------|--------|------|
| Gemini/Codex Adapter 封装 | P0 | Hook Bus |
| Memory 1 向量层 (Chroma 集成) | P0 | - |
| Git 快照绑定逻辑 | P0 | SQLite |
| 自动总结机制 (80/20 压缩) | P1 | Hook Bus |
| 配置面板 UI | P1 | - |
| PostgreSQL 持久化迁移 | P2 | SQLite |

### Phase 3: 高级进阶 - 结构化推理与多跳检索

**目标**：赋能 Agent 处理高度复杂的长程任务

| 任务 | 优先级 | 依赖 |
|------|--------|------|
| Memory 2 SAMG 图谱层 | P0 | - |
| Cytoscape.js 图谱可视化 | P0 | SAMG |
| AST 上下文构建器 (Tree-sitter) | P1 | - |
| 多跳推理检索 | P1 | SAMG |
| 渐进式记忆披露 Hook | P1 | Memory 1 |
| 语义冲突检测 | P2 | SAMG |

### Phase 4: 企业级扩展 - 分布式治理与合规

**目标**：支持大规模并发协作与严苛安全审计

| 任务 | 优先级 | 依赖 |
|------|--------|------|
| 隐私感知 RAG (加密检索) | P0 | Memory 1 |
| BFT 共识协议 | P1 | Adapter |
| 分布式事件总线 (Kafka) | P1 | Hook Bus |
| 多租户隔离 | P2 | - |
| CLHF 评估流 | P2 | - |

---

## 九、协议采纳路径

```
Phase 1: MCP ──► Phase 2: ACP ──► Phase 3: A2A ──► Phase 4: ANP
```

| 阶段 | 协议 | 目标 |
|------|------|------|
| 1 | MCP (Model Context Protocol) | 工具调用标准化 |
| 2 | ACP (Agent Communication Protocol) | 异步多模态消息传递 |
| 3 | A2A (Agent-to-Agent) | 智能体卡片与任务委派 |
| 4 | ANP (Agent Network Protocol) | 分布式身份验证 (DID) |

---

## 十、效能评估指标

### 10.1 工程效率

- PR 周期缩短比例
- Agent 自动修复后的代码覆盖率
- Diff 块人类重修比例

### 10.2 成本控制

- Token 投产比
- TTFT 延迟降低率（目标 80%+）
- 总成本削减率（目标 50%+）

### 10.3 安全合规

- PII 自动脱敏率（目标 100%）
- 规则违背拦截率
- 审计日志完整性

---

## 十一、目录结构规划

```
CodeFlow/
├── packages/
│   ├── gui/                    # Electron + React 前端
│   │   ├── src/
│   │   │   ├── components/     # UI 组件
│   │   │   ├── hooks/          # React Hooks
│   │   │   ├── stores/         # 状态管理
│   │   │   └── views/          # 页面视图
│   │   └── package.json
│   │
│   ├── core/                   # 核心逻辑层
│   │   ├── src/
│   │   │   ├── adapters/       # CLI Adapters
│   │   │   ├── hooks/          # Hook Manager
│   │   │   ├── memory/         # 记忆服务
│   │   │   ├── state/          # 状态管理
│   │   │   └── config/         # 配置系统
│   │   └── package.json
│   │
│   └── shared/                 # 共享类型与工具
│       ├── src/
│       │   ├── types/          # TypeScript 类型定义
│       │   └── utils/          # 工具函数
│       └── package.json
│
├── data/
│   ├── sqlite/                 # SQLite 数据库
│   ├── vectors/                # 向量存储
│   └── graphs/                 # 图谱数据
│
├── .claude/
│   └── rules/                  # 长期规则库
│
├── package.json                # Monorepo 根配置
├── pnpm-workspace.yaml
└── tsconfig.json
```

---

## 十二、附录

### A. 关键技术参考

- LangGraph Checkpointer
- Tree-sitter AST 解析
- Cytoscape.js 图谱渲染
- Chroma/Milvus 向量数据库
- ACT-R 认知架构

### B. 相关文档

- `Code Flow：新一代智能集成开发环境.md` - 完整设计文档
- `1.md` - GUI 功能规划书
- `2.md` - Hook-Verse 架构规划
- `3.md` - 可视化 Debug 面板设计
- `4.md` - 记忆管理仪表盘设计
- `5.md` - 智能干预与动态编排功能规划
- `记忆1.md` - 深度记忆系统规格
- `记忆2.md` - 结构化记忆图谱系统规格
