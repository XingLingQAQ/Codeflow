/**
 * Chroma 向量数据库适配器
 * 支持真实 Chroma 服务器连接与加密向量存储
 */
import { DEFAULT_VECTOR_CONFIG, } from './types.js';
import { SimpleEmbeddingProvider } from './SimpleEmbeddingProvider.js';
/**
 * Chroma 向量存储实现
 */
export class ChromaVectorStore {
    constructor(config, embeddingProvider) {
        this.collectionId = null;
        this.connected = false;
        this.config = {
            ...DEFAULT_VECTOR_CONFIG,
            host: 'localhost',
            port: 8000,
            ssl: false,
            ...config,
        };
        this.embeddingProvider = embeddingProvider || new SimpleEmbeddingProvider();
        const protocol = this.config.ssl ? 'https' : 'http';
        this.baseUrl = `${protocol}://${this.config.host}:${this.config.port}`;
    }
    /**
     * 连接到 Chroma 服务器并获取/创建集合
     */
    async connect() {
        try {
            // 检查服务器健康状态
            const healthResponse = await this.fetch('/api/v1/heartbeat');
            if (!healthResponse.ok) {
                throw new Error(`Chroma server not healthy: ${healthResponse.status}`);
            }
            // 获取或创建集合
            await this.getOrCreateCollection();
            this.connected = true;
        }
        catch (error) {
            this.connected = false;
            throw new Error(`Failed to connect to Chroma: ${error.message}`);
        }
    }
    /**
     * 断开连接
     */
    async disconnect() {
        this.connected = false;
        this.collectionId = null;
    }
    /**
     * 添加文档块
     */
    async add(chunks) {
        this.ensureConnected();
        if (chunks.length === 0)
            return;
        const ids = chunks.map((c) => c.id);
        const documents = chunks.map((c) => c.content);
        const metadatas = chunks.map((c) => this.serializeMetadata(c.metadata));
        // 生成嵌入向量
        const embeddings = await this.embeddingProvider.embedBatch(documents);
        await this.fetch(`/api/v1/collections/${this.collectionId}/add`, {
            method: 'POST',
            body: JSON.stringify({
                ids,
                embeddings,
                documents,
                metadatas,
            }),
        });
    }
    /**
     * 搜索相似文档
     */
    async search(query, options) {
        this.ensureConnected();
        const topK = options?.topK ?? 10;
        const queryEmbedding = await this.embeddingProvider.embed(query);
        const whereFilter = options?.filter ? this.buildWhereFilter(options.filter) : undefined;
        const response = await this.fetch(`/api/v1/collections/${this.collectionId}/query`, {
            method: 'POST',
            body: JSON.stringify({
                query_embeddings: [queryEmbedding],
                n_results: topK,
                include: ['documents', 'metadatas', 'distances', ...(options?.includeEmbeddings ? ['embeddings'] : [])],
                where: whereFilter,
            }),
        });
        const result = await response.json();
        if (!result.ids || result.ids.length === 0 || result.ids[0].length === 0) {
            return [];
        }
        const results = [];
        const ids = result.ids[0];
        const documents = result.documents?.[0] || [];
        const metadatas = result.metadatas?.[0] || [];
        const distances = result.distances?.[0] || [];
        const embeddings = result.embeddings?.[0];
        for (let i = 0; i < ids.length; i++) {
            const distance = distances[i] || 0;
            const score = 1 / (1 + distance); // 转换距离为相似度分数
            if (options?.minScore && score < options.minScore)
                continue;
            const chunk = {
                id: ids[i],
                content: documents[i] || '',
                metadata: this.deserializeMetadata(metadatas[i]),
                ...(options?.includeEmbeddings && embeddings ? { embedding: embeddings[i] } : {}),
            };
            results.push({ chunk, score, distance });
        }
        return results;
    }
    /**
     * 删除文档
     */
    async delete(ids) {
        this.ensureConnected();
        if (ids.length === 0)
            return;
        await this.fetch(`/api/v1/collections/${this.collectionId}/delete`, {
            method: 'POST',
            body: JSON.stringify({ ids }),
        });
    }
    /**
     * 清空集合
     */
    async clear() {
        this.ensureConnected();
        // Chroma 没有直接的 clear API，需要删除并重建集合
        await this.fetch(`/api/v1/collections/${this.collectionId}`, {
            method: 'DELETE',
        });
        await this.getOrCreateCollection();
    }
    /**
     * 按会话 ID 获取文档
     */
    async getBySessionId(sessionId) {
        return this.getByFilter({ sessionId });
    }
    /**
     * 按 Git 提交获取文档
     */
    async getByGitCommit(commitHash) {
        return this.getByFilter({ gitCommitHash: commitHash });
    }
    /**
     * 获取文档数量
     */
    async count() {
        this.ensureConnected();
        const response = await this.fetch(`/api/v1/collections/${this.collectionId}/count`);
        return response.json();
    }
    /**
     * 获取集合信息
     */
    async getCollectionInfo() {
        this.ensureConnected();
        const countValue = await this.count();
        return {
            name: this.config.collectionName,
            count: countValue,
            dimension: this.embeddingProvider.getDimension(),
            metadata: {
                host: this.config.host,
                port: this.config.port,
                connected: this.connected,
            },
        };
    }
    /**
     * 检查连接状态
     */
    isConnected() {
        return this.connected;
    }
    // ==================== 私有方法 ====================
    async getOrCreateCollection() {
        // 尝试获取现有集合
        const listResponse = await this.fetch('/api/v1/collections');
        const collections = await listResponse.json();
        const existing = collections.find((c) => c.name === this.config.collectionName);
        if (existing) {
            // 获取集合 ID
            const getResponse = await this.fetch(`/api/v1/collections/${this.config.collectionName}`);
            const collection = await getResponse.json();
            this.collectionId = collection.id;
        }
        else {
            // 创建新集合
            const createResponse = await this.fetch('/api/v1/collections', {
                method: 'POST',
                body: JSON.stringify({
                    name: this.config.collectionName,
                    metadata: { dimension: this.embeddingProvider.getDimension() },
                }),
            });
            const collection = await createResponse.json();
            this.collectionId = collection.id;
        }
    }
    async getByFilter(filter) {
        this.ensureConnected();
        const whereFilter = this.buildWhereFilter(filter);
        const response = await this.fetch(`/api/v1/collections/${this.collectionId}/get`, {
            method: 'POST',
            body: JSON.stringify({
                where: whereFilter,
                include: ['documents', 'metadatas'],
            }),
        });
        const result = await response.json();
        if (!result.ids || result.ids.length === 0) {
            return [];
        }
        return result.ids.map((id, i) => ({
            id,
            content: result.documents?.[i] || '',
            metadata: this.deserializeMetadata(result.metadatas?.[i]),
        }));
    }
    buildWhereFilter(filter) {
        const conditions = [];
        for (const [key, value] of Object.entries(filter)) {
            if (value !== undefined) {
                conditions.push({ [key]: { $eq: value } });
            }
        }
        if (conditions.length === 0)
            return undefined;
        if (conditions.length === 1)
            return conditions[0];
        return { $and: conditions };
    }
    serializeMetadata(metadata) {
        return {
            sessionId: metadata.sessionId,
            agentRole: metadata.agentRole,
            gitCommitHash: metadata.gitCommitHash || '',
            messageIndex: metadata.messageIndex,
            chunkIndex: metadata.chunkIndex,
            timestamp: metadata.timestamp,
            source: metadata.source,
        };
    }
    deserializeMetadata(raw) {
        if (!raw) {
            return {
                sessionId: '',
                agentRole: '',
                messageIndex: 0,
                chunkIndex: 0,
                timestamp: Date.now(),
                source: 'system',
            };
        }
        return {
            sessionId: String(raw.sessionId || ''),
            agentRole: String(raw.agentRole || ''),
            gitCommitHash: raw.gitCommitHash ? String(raw.gitCommitHash) : undefined,
            messageIndex: Number(raw.messageIndex || 0),
            chunkIndex: Number(raw.chunkIndex || 0),
            timestamp: Number(raw.timestamp || Date.now()),
            source: raw.source || 'system',
        };
    }
    ensureConnected() {
        if (!this.connected || !this.collectionId) {
            throw new Error('Not connected to Chroma. Call connect() first.');
        }
    }
    async fetch(path, options) {
        const url = `${this.baseUrl}${path}`;
        const headers = {
            'Content-Type': 'application/json',
        };
        if (this.config.authToken) {
            headers['Authorization'] = `Bearer ${this.config.authToken}`;
        }
        const response = await fetch(url, {
            ...options,
            headers: { ...headers, ...options?.headers },
        });
        if (!response.ok) {
            const errorText = await response.text().catch(() => 'Unknown error');
            throw new Error(`Chroma API error: ${response.status} - ${errorText}`);
        }
        return response;
    }
}
/**
 * 创建 Chroma 向量存储实例并连接
 */
export async function createChromaStore(config, embeddingProvider) {
    const store = new ChromaVectorStore(config, embeddingProvider);
    await store.connect();
    return store;
}
//# sourceMappingURL=ChromaVectorStore.js.map