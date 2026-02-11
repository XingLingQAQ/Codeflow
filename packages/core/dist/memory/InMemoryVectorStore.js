/**
 * 内存向量存储实现
 * 轻量级本地向量存储，支持 Chroma 兼容接口
 */
import { DEFAULT_VECTOR_CONFIG, } from './types.js';
import { SimpleEmbeddingProvider } from './SimpleEmbeddingProvider.js';
export class InMemoryVectorStore {
    constructor(config, embeddingProvider) {
        this.chunks = new Map();
        this.embeddings = new Map();
        this.config = { ...DEFAULT_VECTOR_CONFIG, ...config };
        this.embeddingProvider = embeddingProvider || new SimpleEmbeddingProvider();
    }
    async add(chunks) {
        const texts = chunks.map((c) => c.content);
        const embeddings = await this.embeddingProvider.embedBatch(texts);
        for (let i = 0; i < chunks.length; i++) {
            const chunk = chunks[i];
            this.chunks.set(chunk.id, chunk);
            this.embeddings.set(chunk.id, embeddings[i]);
        }
    }
    async search(query, options) {
        const queryEmbedding = await this.embeddingProvider.embed(query);
        const topK = options?.topK ?? 10;
        const minScore = options?.minScore ?? 0;
        const results = [];
        for (const [id, embedding] of this.embeddings) {
            const chunk = this.chunks.get(id);
            if (!chunk)
                continue;
            // 应用过滤器
            if (options?.filter && !this.matchesFilter(chunk, options.filter)) {
                continue;
            }
            const distance = this.cosineSimilarity(queryEmbedding, embedding);
            const score = (distance + 1) / 2; // 转换为 0-1 范围
            if (score >= minScore) {
                results.push({
                    chunk: options?.includeEmbeddings ? { ...chunk, embedding } : chunk,
                    score,
                    distance,
                });
            }
        }
        // 按分数排序并返回 topK
        return results.sort((a, b) => b.score - a.score).slice(0, topK);
    }
    async delete(ids) {
        for (const id of ids) {
            this.chunks.delete(id);
            this.embeddings.delete(id);
        }
    }
    async clear() {
        this.chunks.clear();
        this.embeddings.clear();
    }
    async getBySessionId(sessionId) {
        return Array.from(this.chunks.values()).filter((chunk) => chunk.metadata.sessionId === sessionId);
    }
    async getByGitCommit(commitHash) {
        return Array.from(this.chunks.values()).filter((chunk) => chunk.metadata.gitCommitHash === commitHash);
    }
    async count() {
        return this.chunks.size;
    }
    async getCollectionInfo() {
        return {
            name: this.config.collectionName,
            count: this.chunks.size,
            dimension: this.embeddingProvider.getDimension(),
        };
    }
    // ==================== 私有方法 ====================
    cosineSimilarity(a, b) {
        if (a.length !== b.length)
            return 0;
        let dotProduct = 0;
        let normA = 0;
        let normB = 0;
        for (let i = 0; i < a.length; i++) {
            dotProduct += a[i] * b[i];
            normA += a[i] * a[i];
            normB += b[i] * b[i];
        }
        const magnitude = Math.sqrt(normA) * Math.sqrt(normB);
        return magnitude === 0 ? 0 : dotProduct / magnitude;
    }
    matchesFilter(chunk, filter) {
        for (const [key, value] of Object.entries(filter)) {
            if (value !== undefined && chunk.metadata[key] !== value) {
                return false;
            }
        }
        return true;
    }
}
//# sourceMappingURL=InMemoryVectorStore.js.map