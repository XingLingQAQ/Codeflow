/**
 * PassiveRAGService - MS-220 被动 RAG 自动检索
 *
 * 在发送请求前自动检索相关记忆，按时间局部性 + 语义相关性排序，
 * 格式化为上下文注入到对话中。
 */
const DEFAULT_CONFIG = {
    topN: 5,
    threshold: 0.3,
    timeLocalityWeight: 0.3,
    semanticWeight: 0.7,
};
export class PassiveRAGService {
    constructor(memoryService, config = {}) {
        this.memoryService = memoryService;
        this.config = { ...DEFAULT_CONFIG, ...config };
    }
    /**
     * 检索与查询相关的记忆，按综合评分排序
     */
    async retrieve(query, sessionId) {
        const trimmedQuery = query.trim();
        if (!trimmedQuery) {
            return [];
        }
        const searchResults = await this.memoryService.search(trimmedQuery, {
            limit: this.config.topN * 3,
            offset: 0,
            sessionId,
        });
        if (searchResults.length === 0) {
            return [];
        }
        const now = Math.floor(Date.now() / 1000);
        const scored = searchResults.map((memory, index) => {
            const semanticScore = this.computeSemanticScore(index, searchResults.length);
            const timeScore = this.computeTimeScore(memory.timestamp, now);
            const combinedScore = this.config.semanticWeight * semanticScore +
                this.config.timeLocalityWeight * timeScore;
            return {
                memory,
                score: combinedScore,
                timeScore,
                semanticScore,
            };
        });
        return scored
            .filter((item) => item.score >= this.config.threshold)
            .sort((a, b) => b.score - a.score)
            .slice(0, this.config.topN);
    }
    /**
     * 将检索到的记忆格式化为可注入的上下文字符串
     */
    formatForInjection(memories) {
        if (memories.length === 0) {
            return '';
        }
        const lines = ['[相关记忆上下文]'];
        for (let i = 0; i < memories.length; i++) {
            const { memory, score } = memories[i];
            const time = new Date(memory.timestamp * 1000).toISOString();
            const tags = memory.tags.length > 0 ? ` [${memory.tags.join(', ')}]` : '';
            lines.push(`${i + 1}. (${time}${tags}, 相关度: ${score.toFixed(2)}) ${memory.content}`);
        }
        return lines.join('\n');
    }
    /**
     * 基于搜索结果排名计算语义评分
     * 排名越靠前，评分越高
     */
    computeSemanticScore(rank, total) {
        if (total <= 1)
            return 1;
        return 1 - rank / total;
    }
    /**
     * 基于时间衰减计算时间局部性评分
     * 越近的记忆评分越高
     */
    computeTimeScore(timestamp, now) {
        const ageSeconds = Math.max(0, now - timestamp);
        const oneDay = 86400;
        return Math.exp(-ageSeconds / (7 * oneDay));
    }
}
//# sourceMappingURL=PassiveRAG.js.map