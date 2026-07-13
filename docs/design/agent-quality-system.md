# Agent 生态与质量体系详细设计

> 状态：Active / 目标态设计  
> 覆盖：Agent 广场、按需多模型辩论、守卫系统、配置中心  
> 里程碑：M3（守卫）、M4（辩论/广场）、M5（配置中心）  
> 纳入跟踪：2026-07-13（M0.6）

---

## 1. Agent 广场（M4）

### 1.1 Agent 资产模型

```
Agent
├── id / name / avatar / description / version / source: builtin|user|plugin
├── role_base: main|coder|sub|critic|researcher（继承现有三级角色配置）
├── binding: { model, channel, temperature... }
├── mounts: { mcp_tools[], skills[] }
├── stage_tags: 适用阶段标签
└── stats: 使用量 / 评分
```

### 1.2 交互

- 卡片市场：筛选（阶段/能力/来源）、详情页（提示词预览/挂载清单/版本历史）
- "添加到工作流"：拖入工作流模板编辑器阶段节点的 Agent 插槽，或绑定到运行中 Flow 的某阶段
- 自建向导：基于角色基底 → 改提示词 → 挂 MCP/Skill → 沙箱试运行 → 发布
- 后端：internal/agent 扩展为 Registry（CRUD + 版本 + 来源标记）

## 2. 按需多模型辩论（M4）

### 2.1 触发

1. Agent 自主：置信度低/方案分歧时建议发起
2. 用户手动：Companion 面板、产物卡片、编辑器选中代码右键
3. Gate 升级：校验失败且 on_fail=escalate_to_debate

### 2.2 配置弹层

- 议题（自动从上下文提取，可改）
- 参与方：从广场选 2~N，**每方独立绑定模型/渠道**（经 hotswap 路由）
- 轮数上限、共识策略：仲裁裁决 | 投票 | 用户终裁

### 2.3 画布与回流

- 复用 Review 辩论面板组件，任意阶段以浮动面板/Tab 打开
- 多方时间轴 + 冲突点高亮 + 论点结构卡（主张/论据/反驳）+ 结论卡
- 产物 debate-resolution 注入发起处上下文；结论沉淀 SAMG（方案A --rejected_because--> 风险X）；披露机制下次自动提示历史辩论

### 2.4 后端改造

internal/debate：双方 → 多方；每方独立 adapter 绑定；辩论会话关联 Flow 阶段外键。

## 3. 守卫系统 internal/guard（M3，影子系统强制化补充）

### 3.1 三道防线

1. **hook_before_write（强制锚点，不可绕过）**：拦截 Agent 全部文件写入
   - 重复定义检测：基于 internal/ast 全项目符号索引，签名 + 语义向量双重比对；同名同签名拒绝，高相似度警告并给出位置（"疑似重复 parseConfig @ a.ts:42"）
   - 堆叠行为检测：utils2/xxx_new/xxx_v2 命名、复制大段已有代码小改、在弃用模块追加、绕过现有抽象重写 → 拒绝并返回结构化原因，强制改为修改原符号或显式重构
   - 影子对照：写入先落 internal/shadow 影子区，全量 lint/typecheck/重复检测通过才合入，失败反馈进入 Agent 修复循环
2. **规则引擎**：规则分级 error/warn/off；项目级 .codeflow/guard.yaml；插件贡献点 guardRules（命名规范、分层约束如 stages 不得 import shell）
3. **豁免有代价**：Agent 显式申请 → 用户审批 → internal/audit 不可变记录 → 提交阶段归档报告汇总豁免清单

### 3.2 前端

编码画布守卫面板（拦截记录/警告流/豁免审批）+ 状态栏守卫健康度灯。

### 3.3 符号索引

编码阶段开始全量构建，写入后增量更新；与导入流的 AST 索引共享存储。

## 4. 配置中心（M5）

| 子区 | 能力 | 后端 |
|---|---|---|
| 渠道 | 卡片管理（BaseURL/Key/类型）、连通性测试（延迟/可用模型）、配额用量仪表、故障转移拖拽排序、Key 脱敏 | config api_pool + hotswap 健康探测 |
| 模型 | 注册表：能力标签（上下文/工具/视觉）、价格、默认参数表单、按角色绑定、热切换测试、继承链可视化（值来自全局/被会话覆盖标注） | 三级配置继承（已有） |
| MCP | 添加（stdio/SSE）、启停、工具自动发现逐项开关、拖拽分配到 Agent、调用日志、权限审批策略 | MCP 挂载 + isolation 沙箱 + 配置数字签名 |
| Skill | Markdown 编辑（frontmatter 表单：触发条件/依赖工具）、版本历史、按 Agent 分配、市场安装、沙箱测试 | 新增 internal/skill（存储 + 匹配注入提示词），兼容迁移 .claude/ 资产 |

统一原则：改动即时预览 + 显式保存；配置版本化可回滚；危险操作二次确认；全部变更入审计日志。
