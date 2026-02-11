const DEFAULT_OPTIONS = {
    source: 'user',
    maxMemoriesPerExtraction: 5,
    minImportance: 0.2,
    asyncDelayMs: 0,
};
const MEMORY_EXTRACTOR_PROMPT = `
你是记忆提取助手。请从给定对话中提取“值得长期记忆”的信息。

提取标准：
1) 用户事实、偏好、背景、长期目标；
2) 重要决策、约束与结论；
3) 对后续对话有价值的上下文。

输出必须是 JSON 对象，不要输出 markdown，不要输出额外解释：
{
  "memories": [
    {
      "content": "完整且自包含的一句话或两句话",
      "tags": ["tag1", "tag2"],
      "importance": 0.0,
      "source": "user"
    }
  ]
}

约束：
- content 不能为空。
- tags 为简洁关键词数组，最多 5 个。
- importance 在 0~1 之间。
- source 仅可为 user/assistant/system，默认 user。
- 若无可提取内容，返回 {"memories": []}。
`;
export class MemoryExtractor {
    constructor(llmAdapter, memoryService, options = {}) {
        this.llmAdapter = llmAdapter;
        this.memoryService = memoryService;
        this.options = {
            ...DEFAULT_OPTIONS,
            ...options,
            maxMemoriesPerExtraction: Math.max(1, options.maxMemoriesPerExtraction ?? DEFAULT_OPTIONS.maxMemoriesPerExtraction),
            minImportance: this.clamp01(options.minImportance ?? DEFAULT_OPTIONS.minImportance),
            asyncDelayMs: Math.max(0, options.asyncDelayMs ?? DEFAULT_OPTIONS.asyncDelayMs),
        };
    }
    extractFromConversation(userMessage, assistantMessage, sessionId) {
        const safeSessionId = sessionId.trim();
        if (!safeSessionId) {
            return;
        }
        const safeUserMessage = userMessage.trim();
        const safeAssistantMessage = assistantMessage.trim();
        if (!safeUserMessage && !safeAssistantMessage) {
            return;
        }
        this.defer(async () => {
            const prompt = this.buildPrompt(safeUserMessage, safeAssistantMessage);
            const response = await this.llmAdapter.send(prompt, {
                temperature: 0.2,
                maxTokens: 500,
            });
            const extraction = this.parseExtractionResponse(response);
            if (extraction.length === 0) {
                return;
            }
            for (const memory of extraction) {
                await this.memoryService.add({
                    ...memory,
                    id: this.createMemoryId(),
                    timestamp: Math.floor(Date.now() / 1000),
                    sessionId: safeSessionId,
                });
            }
        });
    }
    defer(task) {
        setTimeout(() => {
            void task().catch(() => {
                // 异步提取不阻塞主流程；失败静默处理
            });
        }, this.options.asyncDelayMs);
    }
    buildPrompt(userMessage, assistantMessage) {
        return `${MEMORY_EXTRACTOR_PROMPT}\n\n用户消息：\n${userMessage || '(empty)'}\n\n助手回复：\n${assistantMessage || '(empty)'}`;
    }
    parseExtractionResponse(response) {
        const raw = response.content || '';
        const jsonCandidate = this.extractJsonObject(raw);
        if (!jsonCandidate) {
            return [];
        }
        let payload;
        try {
            payload = JSON.parse(jsonCandidate);
        }
        catch {
            return [];
        }
        const candidates = Array.isArray(payload.memories) ? payload.memories : [];
        const normalized = candidates
            .map((item) => this.normalizeCandidate(item))
            .filter((item) => item !== null)
            .filter((item) => item.importance >= this.options.minImportance)
            .slice(0, this.options.maxMemoriesPerExtraction);
        return normalized;
    }
    normalizeCandidate(candidate) {
        const content = typeof candidate.content === 'string' ? candidate.content.trim() : '';
        if (!content) {
            return null;
        }
        const tags = this.normalizeTags(candidate.tags);
        const importance = this.clamp01(typeof candidate.importance === 'number' ? candidate.importance : 0.5);
        const source = this.normalizeSource(candidate.source);
        return {
            content,
            tags,
            source,
            importance,
        };
    }
    normalizeTags(tags) {
        if (!Array.isArray(tags)) {
            return [];
        }
        const normalized = tags
            .map((item) => (typeof item === 'string' ? item.trim() : ''))
            .filter((item) => item.length > 0)
            .slice(0, 5);
        return Array.from(new Set(normalized));
    }
    normalizeSource(source) {
        if (source === 'assistant' || source === 'system' || source === 'user') {
            return source;
        }
        return this.options.source;
    }
    extractJsonObject(raw) {
        if (!raw.trim()) {
            return null;
        }
        const fenced = raw.match(/```(?:json)?\s*([\s\S]*?)\s*```/i);
        if (fenced && fenced[1]) {
            const candidate = fenced[1].trim();
            if (candidate.startsWith('{') && candidate.endsWith('}')) {
                return candidate;
            }
        }
        const start = raw.indexOf('{');
        const end = raw.lastIndexOf('}');
        if (start >= 0 && end > start) {
            return raw.slice(start, end + 1);
        }
        return null;
    }
    clamp01(value) {
        if (!Number.isFinite(value))
            return 0;
        if (value < 0)
            return 0;
        if (value > 1)
            return 1;
        return value;
    }
    createMemoryId() {
        const random = Math.random().toString(36).slice(2, 10);
        return `mem_${Date.now()}_${random}`;
    }
}
//# sourceMappingURL=MemoryExtractor.js.map