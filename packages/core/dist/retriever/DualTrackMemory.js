/**
 * 双轨记忆协同实现
 * 融合向量存储和图谱存储的混合检索
 */
import { DEFAULT_DUAL_TRACK_CONFIG, } from './DualTrackTypes.js';
import { isLiteralValue } from '../samg/types.js';
export class DualTrackMemory {
    constructor(config = {}) {
        this.config = { ...DEFAULT_DUAL_TRACK_CONFIG, ...config };
    }
    setVectorStore(store) {
        this.vectorStore = store;
    }
    setTripleStore(store) {
        this.tripleStore = store;
    }
    setSemanticRetriever(retriever) {
        this.semanticRetriever = retriever;
    }
    async hybridSearch(params) {
        const startTime = Date.now();
        const limit = params.limit || this.config.topK;
        const searchMode = params.searchMode || 'hybrid';
        let vectorResults = [];
        let graphResults = [];
        // 根据搜索模式执行搜索
        if (searchMode === 'vector' || searchMode === 'hybrid') {
            vectorResults = await this.vectorSearch(params.query, limit * 2);
        }
        if (searchMode === 'graph' || searchMode === 'hybrid') {
            graphResults = await this.graphSearch(params.query, limit * 2);
        }
        // 合并结果
        const merged = this.mergeResults(vectorResults, graphResults, params);
        // 应用过滤器
        const filtered = this.applyFilters(merged, params);
        // 排序并截取
        const sorted = filtered
            .sort((a, b) => b.score - a.score)
            .slice(0, limit);
        return {
            results: sorted,
            totalCount: sorted.length,
            vectorCount: vectorResults.length,
            graphCount: graphResults.length,
            queryTime: Date.now() - startTime,
            searchMode,
        };
    }
    async vectorSearch(query, limit = 10) {
        if (!this.vectorStore)
            return [];
        try {
            const results = await this.vectorStore.search(query, {
                topK: limit,
                minScore: this.config.minScore,
            });
            return results.map((r) => ({
                id: r.chunk.id,
                content: r.chunk.content,
                score: r.score * this.config.vectorWeight,
                vectorScore: r.score,
                source: 'vector',
                metadata: r.chunk.metadata,
            }));
        }
        catch {
            return [];
        }
    }
    async graphSearch(query, limit = 10) {
        if (!this.tripleStore)
            return [];
        try {
            // 从查询中提取关键实体
            const queryTokens = this.tokenize(query);
            const results = [];
            // 搜索匹配的实体
            const entities = await this.tripleStore.getEntities();
            const matchedEntities = entities.filter(entity => {
                const label = entity.label.toLowerCase();
                return queryTokens.some(token => label.includes(token));
            });
            // 为每个匹配的实体获取相关三元组
            for (const entity of matchedEntities.slice(0, limit)) {
                const relatedTriples = await this.tripleStore.findBySubject(entity['@id']);
                const objectTriples = await this.tripleStore.findByObject(entity['@id']);
                // 计算相关性分数
                const labelMatch = queryTokens.filter(t => entity.label.toLowerCase().includes(t)).length / queryTokens.length;
                const tripleCount = relatedTriples.length + objectTriples.length;
                const score = labelMatch * 0.6 + Math.min(tripleCount / 10, 0.4);
                results.push({
                    entity: {
                        '@id': entity['@id'],
                        '@type': entity['@type'],
                        label: entity.label,
                    },
                    relatedTriples: [...relatedTriples, ...objectTriples].slice(0, 10),
                    score,
                });
            }
            // 如果启用扩展激活，增强结果
            if (this.config.enableSpreadingActivation && matchedEntities.length > 0) {
                const seedIds = matchedEntities.slice(0, 3).map(e => e['@id']);
                const activated = await this.spreadingActivation(seedIds, this.config.spreadingDepth, this.config.spreadingDecay);
                // 将激活的节点添加到结果中
                for (const node of activated) {
                    if (!results.some(r => r.entity['@id'] === node.id)) {
                        const triples = await this.tripleStore.findBySubject(node.id);
                        results.push({
                            entity: {
                                '@id': node.id,
                                '@type': node.type,
                                label: node.label,
                            },
                            relatedTriples: triples.slice(0, 5),
                            score: node.activationLevel * this.config.graphWeight,
                            activationLevel: node.activationLevel,
                            path: node.path,
                        });
                    }
                }
            }
            return results.sort((a, b) => b.score - a.score).slice(0, limit);
        }
        catch {
            return [];
        }
    }
    async spreadingActivation(seedEntities, depth = 2, decay = 0.5) {
        if (!this.tripleStore)
            return [];
        const activated = new Map();
        const visited = new Set();
        // 初始化种子节点
        for (const seedId of seedEntities) {
            const entity = await this.tripleStore.getEntity(seedId);
            if (entity) {
                activated.set(seedId, {
                    id: seedId,
                    label: entity.label,
                    type: Array.isArray(entity['@type']) ? entity['@type'][0] : entity['@type'],
                    activationLevel: 1.0,
                    depth: 0,
                    path: [seedId],
                });
            }
        }
        // 扩展激活
        for (let d = 0; d < depth; d++) {
            const currentLevel = Array.from(activated.values()).filter(n => n.depth === d);
            for (const node of currentLevel) {
                if (visited.has(node.id))
                    continue;
                visited.add(node.id);
                // 获取相邻节点
                const outgoing = await this.tripleStore.findBySubject(node.id);
                const incoming = await this.tripleStore.findByObject(node.id);
                const neighbors = [];
                for (const triple of outgoing) {
                    if (!isLiteralValue(triple.object)) {
                        neighbors.push({
                            id: triple.object['@id'],
                            weight: triple.confidence,
                        });
                    }
                }
                for (const triple of incoming) {
                    neighbors.push({
                        id: triple.subject['@id'],
                        weight: triple.confidence,
                    });
                }
                // 传播激活
                for (const neighbor of neighbors) {
                    if (visited.has(neighbor.id))
                        continue;
                    const newActivation = node.activationLevel * decay * neighbor.weight;
                    if (newActivation < 0.1)
                        continue; // 阈值过滤
                    const existing = activated.get(neighbor.id);
                    if (!existing || existing.activationLevel < newActivation) {
                        const entity = await this.tripleStore.getEntity(neighbor.id);
                        if (entity) {
                            activated.set(neighbor.id, {
                                id: neighbor.id,
                                label: entity.label,
                                type: Array.isArray(entity['@type']) ? entity['@type'][0] : entity['@type'],
                                activationLevel: newActivation,
                                depth: d + 1,
                                path: [...node.path, neighbor.id],
                            });
                        }
                    }
                }
            }
        }
        return Array.from(activated.values())
            .filter(n => n.depth > 0) // 排除种子节点
            .sort((a, b) => b.activationLevel - a.activationLevel);
    }
    async findRelatedEntities(entityId, predicates) {
        if (!this.tripleStore)
            return [];
        const results = [];
        const outgoing = await this.tripleStore.findBySubject(entityId);
        const incoming = await this.tripleStore.findByObject(entityId);
        const allTriples = [...outgoing, ...incoming];
        const filtered = predicates
            ? allTriples.filter(t => predicates.includes(t.predicate))
            : allTriples;
        // 按相关实体分组
        const entityMap = new Map();
        for (const triple of filtered) {
            const relatedId = triple.subject['@id'] === entityId
                ? (isLiteralValue(triple.object) ? null : triple.object['@id'])
                : triple.subject['@id'];
            if (relatedId) {
                if (!entityMap.has(relatedId)) {
                    entityMap.set(relatedId, []);
                }
                entityMap.get(relatedId).push(triple);
            }
        }
        for (const [relatedId, triples] of entityMap) {
            const entity = await this.tripleStore.getEntity(relatedId);
            if (entity) {
                const avgConfidence = triples.reduce((sum, t) => sum + t.confidence, 0) / triples.length;
                results.push({
                    entity: {
                        '@id': entity['@id'],
                        '@type': entity['@type'],
                        label: entity.label,
                    },
                    relatedTriples: triples,
                    score: avgConfidence,
                });
            }
        }
        return results.sort((a, b) => b.score - a.score);
    }
    async findPath(fromEntityId, toEntityId, maxDepth = 4) {
        if (!this.tripleStore)
            return [];
        const paths = [];
        const queue = [
            { id: fromEntityId, path: [fromEntityId] },
        ];
        const visited = new Set();
        while (queue.length > 0) {
            const current = queue.shift();
            if (current.path.length > maxDepth)
                continue;
            if (visited.has(current.id))
                continue;
            visited.add(current.id);
            if (current.id === toEntityId) {
                paths.push(current.path);
                continue;
            }
            // 获取相邻节点
            const outgoing = await this.tripleStore.findBySubject(current.id);
            const incoming = await this.tripleStore.findByObject(current.id);
            for (const triple of outgoing) {
                if (!isLiteralValue(triple.object)) {
                    const nextId = triple.object['@id'];
                    if (!visited.has(nextId)) {
                        queue.push({
                            id: nextId,
                            path: [...current.path, nextId],
                        });
                    }
                }
            }
            for (const triple of incoming) {
                const nextId = triple.subject['@id'];
                if (!visited.has(nextId)) {
                    queue.push({
                        id: nextId,
                        path: [...current.path, nextId],
                    });
                }
            }
        }
        return paths.sort((a, b) => a.length - b.length);
    }
    // ==================== Private Methods ====================
    mergeResults(vectorResults, graphResults, params) {
        const merged = new Map();
        // 添加向量结果
        for (const vr of vectorResults) {
            merged.set(vr.id, vr);
        }
        // 添加图谱结果
        for (const gr of graphResults) {
            const id = gr.entity['@id'];
            const existing = merged.get(id);
            if (existing) {
                // 合并分数
                existing.score += gr.score * this.config.graphWeight;
                existing.graphScore = gr.score;
                existing.source = 'hybrid';
                existing.entity = gr.entity;
                existing.relatedTriples = gr.relatedTriples;
            }
            else {
                // 从图谱结果构建内容
                const content = this.buildContentFromGraph(gr);
                merged.set(id, {
                    id,
                    content,
                    score: gr.score * this.config.graphWeight,
                    graphScore: gr.score,
                    source: 'graph',
                    entity: gr.entity,
                    relatedTriples: gr.relatedTriples,
                });
            }
        }
        return Array.from(merged.values());
    }
    buildContentFromGraph(gr) {
        const parts = [];
        parts.push(`Entity: ${gr.entity.label} (${gr.entity['@type']})`);
        if (gr.relatedTriples.length > 0) {
            parts.push('Relations:');
            for (const triple of gr.relatedTriples.slice(0, 5)) {
                const objectLabel = isLiteralValue(triple.object)
                    ? String(triple.object['@value'])
                    : triple.object.label || triple.object['@id'];
                parts.push(`  - ${triple.subject.label} ${triple.predicate} ${objectLabel}`);
            }
        }
        return parts.join('\n');
    }
    applyFilters(results, params) {
        return results.filter(r => {
            // 会话过滤
            if (params.sessionId && r.metadata?.sessionId !== params.sessionId) {
                return false;
            }
            // 实体类型过滤
            if (params.entityTypes && r.entity) {
                const entityType = Array.isArray(r.entity['@type'])
                    ? r.entity['@type'][0]
                    : r.entity['@type'];
                if (!params.entityTypes.includes(entityType || '')) {
                    return false;
                }
            }
            // 时间范围过滤
            if (params.timeRange && r.metadata) {
                const timestamp = r.metadata.timestamp;
                if (timestamp < params.timeRange.start || timestamp > params.timeRange.end) {
                    return false;
                }
            }
            return true;
        });
    }
    tokenize(text) {
        return text
            .toLowerCase()
            .replace(/[^\w\s\u4e00-\u9fff]/g, ' ')
            .split(/\s+/)
            .filter(t => t.length > 1);
    }
}
//# sourceMappingURL=DualTrackMemory.js.map