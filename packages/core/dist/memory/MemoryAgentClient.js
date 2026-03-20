/**
 * MemoryAgentClient - 统一记忆调度层客户端
 *
 * 对接后端 MemoryAgent API，提供 Ingest/Retrieve/AssembleContext 三个核心操作。
 */
export class MemoryAgentClient {
    constructor(baseUrl = 'http://localhost:8080', timeoutMs = 8000) {
        this.baseUrl = baseUrl;
        this.timeoutMs = timeoutMs;
    }
    /**
     * 统一写入：同时归档到 Raw Archive + Atomic Memory
     */
    async ingest(req) {
        return this.post('/api/v1/memory/agent/ingest', req);
    }
    /**
     * 统一检索：从 Atomic Memory 语义搜索
     */
    async retrieve(req) {
        return this.post('/api/v1/memory/agent/retrieve', req);
    }
    /**
     * 上下文组装：为 AI 请求构建记忆上下文块
     */
    async assembleContext(req) {
        return this.post('/api/v1/memory/agent/context', req);
    }
    /**
     * 代码变更记忆写入：将 CodeChangeEvent 摘要沉淀到统一记忆主线。
     */
    async ingestCodeChange(req) {
        return this.ingest({
            content: req.summary,
            type: 'code_diff',
            session_id: req.session_id,
            source: 'system',
            tags: ['code-change-event', req.event_type ?? 'code_diff'].filter(Boolean),
            metadata: {
                task_id: req.task_id,
                agent_id: req.agent_id,
                snapshot_id: req.snapshot_id,
                files: req.files,
                event_type: req.event_type,
                ...req.metadata,
            },
        });
    }
    async post(path, body) {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), this.timeoutMs);
        try {
            const res = await fetch(`${this.baseUrl}${path}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
                signal: controller.signal,
            });
            const json = (await res.json());
            if (!json.success || !json.data) {
                throw new Error(json.error || 'MemoryAgent request failed');
            }
            return json.data;
        }
        finally {
            clearTimeout(timer);
        }
    }
}
//# sourceMappingURL=MemoryAgentClient.js.map