# CodeFlow - 新一代智能集成开发环境

## 项目定位

**CodeFlow** 是一个构建"计算外皮层"（Computational Exocortex）的智能开发环境，采用从"命令执行"到"意图驱动"的创新架构。

### 核心价值主张

| 传统模式痛点 | CodeFlow 解决方案 |
|-------------|-------------------|
| 状态碎片化 | 代码-对话-向量三位一体原子快照 |
| 认知过载 | 分层记忆架构 + 自动总结机制 |
| 反馈滞后 | ReAct循环 + 实时干预机制 |
| 上下文丢失 | 双轨记忆系统（向量 + 图谱） |

---

## 系统架构

> 当前默认主前端为 `apps/desktop`（React + Tauri 双模式；历史 `codeflow_template` 已删除）。`packages/gui` 为历史 GUI 组件资产，`codeflow_extracted` 为迁移中遗留前端，不再作为默认交付入口。

```
┌─────────────────────────────────────────────────────────────────┐
│              GUI Layer (React / Browser / Tauri)                │
│  Chat UI | Plan View | Memory Dashboard | Context Builder | ... │
├─────────────────────────────────────────────────────────────────┤
│              Hook Bus (Event Emitter / RxJS)                    │
│  hook_before_send | hook_post_response | hook_on_stream | ...   │
├─────────────────────────────────────────────────────────────────┤
│           Adapter Layer (Claude/Gemini/Codex)                   │
├─────────────────────────────────────────────────────────────────┤
│                    Memory Layer                                 │
│  Memory 1: Vector DB (Episodic) | Memory 2: SAMG Graph (Semantic)
├─────────────────────────────────────────────────────────────────┤
│                    State Layer                                  │
│  SQLite (Sessions) | Git Snapshots | Atomic Checkpoints        │
└─────────────────────────────────────────────────────────────────┘
```

### 技术栈

- **前端**：React + TypeScript（默认主前端为 `apps/desktop`，支持浏览器与 Tauri/sidecar 双模式）
- **后端**：Go 1.23+ (Gin Web Framework)
- **向量存储**：Chroma / Milvus + SQLite
- **图谱可视化**：Cytoscape.js + JSON-LD
- **LLM 适配器**：Claude / Gemini / Codex

---

## 核心功能模块

### 1. 多层级颗粒度配置系统
- 三级继承：CLI 参数 > 角色配置 > 会话配置 > 全局配置
- 角色级精细化定义（Main/Coder/Sub）
- 专属 MCP 挂载机制
- PAPI 动态路由与热切换

### 2. 智能协作编排
- 指挥官模式（Commander）：单一接口下的多智能体调用
- 嵌套子对话渲染：实时展示执行轨迹
- 基于 Context 告警的自动对话总结

### 3. 原子状态管理

> 当前状态：Snapshot API 已标记为 `experimental`。后端捕获/恢复实现仍包含占位逻辑，在 `P0-001` 完成前不应作为生产级回滚能力使用。

- 代码-对话-向量三位一体快照（experimental）
- 联动回滚：Git Reset + 向量记忆撤回（experimental）
- SQLite 对话历史分片存储

### 4. 双轨记忆架构

**Memory 1：向量化记忆（Episodic Memory）**
- 实时流式写入
- 压缩前导图生成
- 状态一致性同步撤销

**Memory 2：结构化原子图谱（SAMG）**
- S-P-O 三元组存储
- 增量提取机制
- 自动拓扑渲染
- 扩展激活与路径发现

### 5. 多模型协作
- Claude / Gemini / Codex 适配器
- 热切换支持
- 模型状态监控

### 6. 安全与合规
- PII 脱敏与加密存储
- 不可变审计日志
- 哈希链完整性验证
- RBAC 权限隔离

---

## 关键特性亮点

✅ **意图驱动架构** - 从命令执行到自然语言意图理解
✅ **双轨记忆系统** - 向量记忆 + 结构化图谱
✅ **原子快照管理（experimental）** - 代码、对话、向量三位一体；后端捕获/恢复仍在补齐中
✅ **多智能体协作** - 指挥官模式 + 黑板协作
✅ **热切换支持** - 运行时模型切换
✅ **隐私保护** - PII 脱敏、加密存储
✅ **审计合规** - 不可变日志、哈希链验证
✅ **渐进式披露** - 智能上下文管理
✅ **实时交互** - WebSocket 流式通信
✅ **企业级扩展** - 分布式、多租户支持
