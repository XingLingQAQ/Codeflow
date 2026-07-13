A. [Time Anchor]
2026-01-10 17:50:35 America/Denver (MST)

UTC Offset: -07:00

Current Year: 2026

B. [Restated Need]
目标： 优化记忆图谱架构，通过**“结构化原子存储”**替代“全量 AI 总结”，实现记忆图谱的自动渲染与精确检索。

核心逻辑：

增量式提取： AI 仅在每轮对话结束时提取“事实增量（Delta）”，而非每次压缩时重读全文。
专用存储格式： 采用 JSON-LD 或 S-P-O（主-谓-宾）三元组格式持久化知识点。
自动渲染： 前端 GUI 根据存储格式直接绘图（如 Cytoscape.js），无需 AI 参与视觉构图。
精确检索工具： 为子 AI 提供“图谱查询”工具，解决模糊性问题。
C. [Workflow]
定义原子知识架构 (AKS)： 确定内存中存储知识的 JSON 模式。
设计增量 Hook： 拦截 on_post_response，调用小模型提取三元组并追加到文件。
实现自动渲染引擎： 利用图形库将 JSON 文件实时转化为可视化拓扑图。
构建混合检索逻辑： 结合向量数据库（搜索文本）与图数据库（搜索关系）。
D. [Key Evidence & Minimal Reasoning]
1. Key Evidence
增量更新优于全量： 相比于昂贵的上下文全量总结，将内存维护转化为“流式图更新”可大幅降低计算成本 [1.2]。
结构化标准： S-P-O 三元组（Subject-Predicate-Object）是 AI 提取结构化知识的标准原子单位 [1.4]。
可视化标准： Cytoscape.js 支持完全通过 JSON 驱动节点的增删与渲染，适合与流式数据对接 [3.1]。
检索深度： 向量搜索（Vector Search）擅长找“事实”，而知识图谱（KG）擅长连接事实间的“逻辑” [4.5]。
2. Minimal Reasoning
去 AI 总结化： 为了避免重复总结，必须在对话发生时就进行“特征提取”。通过 Hook 在每轮对话后自动追加 {"subject": "X", "action": "implements", "object": "Y"}，图谱便能自动生长。
自动渲染逻辑： GUI 只需要监听 memory.json 的变化。每当有新三元组产生，前端图形库直接在 Canvas 上添加对应的 Node 和 Edge，这种“确定性渲染”比 AI 生成的 Mermaid 图表更稳定且支持交互。
E. [Final Deliverable] Code Flow 结构化记忆图谱系统规格 (SAMG)
1. 记忆存储格式：原子知识片断 (Atomic Knowledge Fragments)
不再存储冗长的段落，而是存储一个名为 memory_graph.jsonl 的日志文件。

存储示例：

JSON



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

2. 增量提取 Hook (Incremental Extraction Hook)
注入点： hook_on_message_complete
逻辑： 1. AI 获取“当前用户输入 + 当前助手输出 + 历史图谱摘要”。
2. AI 仅输出：“本轮对话新增了哪些实体和关系？”
3. 系统将结果追加到 memory_graph.jsonl，并同步给向量数据库。
3. 自动渲染引擎 (GUI Rendering)
技术选型： Cytoscape.js (推荐) 或 D3.js。
运行逻辑：
GUI 启动时加载全量 JSONL。
实时更新： 当文件追加新行时，GUI 无需刷新，直接在画布上 cy.add() 新节点。
交互性： 点击节点可查看原始对话片段（由 step_id 关联数据库）。
4. 协作工具集成：query_graph_memory
为主 AI 和 子 AI (Coder) 提供一个高级工具：

参数说明focus_node想要深入了解的实体名（如 AuthModule）。depth关联检索深度（如 2 层关系）。logic_only是否只返回决策逻辑（如“为什么回滚了版本 A”）。

子 AI 调用场景：

Coder AI 在写代码时发现 AuthModule 逻辑复杂，它不调用向量搜索，而是调用 query_graph_memory(focus_node="AuthModule")。

系统直接返回图谱中的关联路径：AuthModule -> fixed_by -> Step_38 -> because -> Security_Vulnerability。这样 Coder 就能立刻明白不能修改某些敏感逻辑。

F. [Uncertainty & Risks]
节点爆炸风险 [Uncertain]： 长时间对话可能导致图谱节点过多（数千个）。
验证方案： 需要实现“衰减算法”，对旧的、访问频率低的边缘节点进行视觉上的“隐藏”或“折叠”。
实体对齐精度 [Evidence insufficient]： AI 可能会将 UserAuth 和 UserAuthentication 识别为两个节点。
解决路径： 在提取 Hook 中强制加入“实体对齐”步骤，让 AI 对比现有节点列表后再创建新节点。
G. [References / Evidence Trail]
[1.4] Robert McDermott (2025). "From Unstructured Text to Interactive Knowledge Graphs." 定义了 S-P-O JSON 数组作为知识提取的基石。
[3.1] Cytoscape.js Manual. 确认了 JSON 是其标准数据交换格式 { elements: { nodes: [], edges: [] } }。
[4.2] ResearchGate (Binary Information Fragment). 提出了将记忆存储为碎片而非完整文件的“人类脑模拟”存储方式。
[4.5] Medium (Memory Architecture). 强调向量搜索只能找到“什么”，而图谱能找到“为什么”（Connect the dots）。
下一步建议
您是否需要我为您编写一个 “三元组提取 Prompt”？它能确保 AI 在提取时严格遵守您的特殊存储格式，并自动过滤掉无效的聊天废话。

Cytoscape.js: interactive visualization of complex networks

这段视频详细展示了如何将结构化的 JSON 数据通过 Cytoscape.js 转化为交互式图谱界面，非常适合作为您 GUI 自动渲染功能的技术参考。

