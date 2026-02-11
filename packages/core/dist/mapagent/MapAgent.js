/**
 * Map Agent 实现
 * 决策骨架提取与导图构建
 */
import { DEFAULT_MAP_AGENT_CONFIG, ENTITY_TYPES, RELATION_TYPES, } from './types.js';
export class MapAgent {
    constructor(adapter, config) {
        this.adapter = adapter;
        this.config = { ...DEFAULT_MAP_AGENT_CONFIG, ...config };
    }
    async extract(messages) {
        if (this.adapter) {
            return this.extractWithLLM(messages);
        }
        return this.extractLocally(messages);
    }
    async buildMap(messages, sessionId) {
        const extraction = await this.extract(messages);
        const nodes = [];
        const edges = [];
        const nodeIdMap = new Map();
        let nodeCounter = 0;
        const generateNodeId = () => `node_${++nodeCounter}`;
        // 创建实体节点
        if (this.config.extractEntities) {
            for (const entity of extraction.entities) {
                if (entity.importance < this.config.minImportance)
                    continue;
                const nodeId = generateNodeId();
                nodeIdMap.set(entity.name, nodeId);
                nodes.push({
                    id: nodeId,
                    type: 'entity',
                    label: entity.name,
                    content: entity.name,
                    importance: entity.importance,
                    timestamp: Date.now(),
                    metadata: { entityType: entity.type },
                });
            }
        }
        // 创建决策节点
        if (this.config.extractDecisions) {
            for (const decision of extraction.decisions) {
                if (decision.importance < this.config.minImportance)
                    continue;
                const nodeId = generateNodeId();
                const label = decision.content.slice(0, 50);
                nodeIdMap.set(label, nodeId);
                nodes.push({
                    id: nodeId,
                    type: 'decision',
                    label,
                    content: decision.content,
                    importance: decision.importance,
                    timestamp: Date.now(),
                    metadata: { context: decision.context },
                });
            }
        }
        // 创建概念节点
        for (const concept of extraction.concepts) {
            const nodeId = generateNodeId();
            nodeIdMap.set(concept.name, nodeId);
            nodes.push({
                id: nodeId,
                type: 'concept',
                label: concept.name,
                content: concept.description,
                importance: 0.5,
                timestamp: Date.now(),
            });
        }
        // 创建边
        if (this.config.extractRelations) {
            let edgeCounter = 0;
            for (const relation of extraction.relations) {
                const sourceId = nodeIdMap.get(relation.from);
                const targetId = nodeIdMap.get(relation.to);
                if (sourceId && targetId) {
                    edges.push({
                        id: `edge_${++edgeCounter}`,
                        source: sourceId,
                        target: targetId,
                        type: relation.type,
                        weight: relation.weight,
                        label: relation.type,
                    });
                }
            }
        }
        // 限制节点数量
        const sortedNodes = nodes.sort((a, b) => b.importance - a.importance);
        const limitedNodes = sortedNodes.slice(0, this.config.maxNodes);
        const limitedNodeIds = new Set(limitedNodes.map((n) => n.id));
        const limitedEdges = edges.filter((e) => limitedNodeIds.has(e.source) && limitedNodeIds.has(e.target));
        // 构建决策骨架
        const skeleton = {
            entities: extraction.entities.map((e) => e.name),
            decisions: extraction.decisions.map((d) => d.content),
            relations: extraction.relations.map((r) => ({
                from: r.from,
                to: r.to,
                type: r.type,
            })),
        };
        return {
            id: `map_${sessionId}_${Date.now()}`,
            sessionId,
            nodes: limitedNodes,
            edges: limitedEdges,
            skeleton,
            createdAt: Date.now(),
            messageRange: {
                start: 0,
                end: messages.length - 1,
            },
        };
    }
    mergeMap(existing, newMap) {
        const existingNodeLabels = new Set(existing.nodes.map((n) => n.label));
        const newNodes = newMap.nodes.filter((n) => !existingNodeLabels.has(n.label));
        const existingEdgeKeys = new Set(existing.edges.map((e) => `${e.source}-${e.target}-${e.type}`));
        const newEdges = newMap.edges.filter((e) => !existingEdgeKeys.has(`${e.source}-${e.target}-${e.type}`));
        return {
            id: existing.id,
            sessionId: existing.sessionId,
            nodes: [...existing.nodes, ...newNodes],
            edges: [...existing.edges, ...newEdges],
            skeleton: {
                entities: [...new Set([...existing.skeleton.entities, ...newMap.skeleton.entities])],
                decisions: [...existing.skeleton.decisions, ...newMap.skeleton.decisions],
                relations: [...existing.skeleton.relations, ...newMap.skeleton.relations],
            },
            createdAt: existing.createdAt,
            messageRange: {
                start: existing.messageRange.start,
                end: newMap.messageRange.end,
            },
        };
    }
    // ==================== 私有方法 ====================
    async extractWithLLM(messages) {
        if (!this.adapter) {
            return this.extractLocally(messages);
        }
        const prompt = this.buildExtractionPrompt(messages);
        try {
            const response = await this.adapter.send(prompt, { maxTokens: 2000 });
            return this.parseExtractionResponse(response.content);
        }
        catch {
            return this.extractLocally(messages);
        }
    }
    extractLocally(messages) {
        const entities = [];
        const decisions = [];
        const relations = [];
        const concepts = [];
        const entitySet = new Set();
        const decisionSet = new Set();
        for (const msg of messages) {
            const content = msg.content;
            // 提取实体（大写开头的词、代码标识符）
            const entityMatches = content.match(/\b[A-Z][a-zA-Z]+\b/g) || [];
            const codeMatches = content.match(/`([^`]+)`/g) || [];
            for (const entity of entityMatches) {
                if (!entitySet.has(entity) && entity.length > 2) {
                    entitySet.add(entity);
                    entities.push({
                        name: entity,
                        type: this.inferEntityType(entity, content),
                        importance: this.calculateImportance(entity, messages),
                    });
                }
            }
            for (const code of codeMatches) {
                const cleaned = code.replace(/`/g, '');
                if (!entitySet.has(cleaned) && cleaned.length > 1) {
                    entitySet.add(cleaned);
                    entities.push({
                        name: cleaned,
                        type: ENTITY_TYPES.FUNCTION,
                        importance: 0.6,
                    });
                }
            }
            // 提取决策
            const decisionKeywords = [
                'decide',
                'choose',
                'select',
                'implement',
                'use',
                'adopt',
                'will',
                'should',
                '决定',
                '选择',
                '采用',
                '实现',
                '使用',
            ];
            const sentences = content.split(/[.。!！?？]/);
            for (const sentence of sentences) {
                const lowerSentence = sentence.toLowerCase();
                if (decisionKeywords.some((kw) => lowerSentence.includes(kw))) {
                    const trimmed = sentence.trim();
                    if (trimmed && !decisionSet.has(trimmed) && trimmed.length > 10) {
                        decisionSet.add(trimmed);
                        decisions.push({
                            content: trimmed,
                            importance: this.calculateDecisionImportance(trimmed),
                            context: msg.role,
                        });
                    }
                }
            }
        }
        // 提取关系（基于共现）
        const entityList = Array.from(entitySet);
        for (let i = 0; i < entityList.length - 1; i++) {
            for (let j = i + 1; j < entityList.length; j++) {
                const e1 = entityList[i];
                const e2 = entityList[j];
                for (const msg of messages) {
                    if (msg.content.includes(e1) && msg.content.includes(e2)) {
                        const relationType = this.inferRelationType(msg.content, e1, e2);
                        relations.push({
                            from: e1,
                            to: e2,
                            type: relationType,
                            weight: 0.5,
                        });
                        break;
                    }
                }
            }
        }
        return {
            entities: entities.slice(0, 30),
            decisions: decisions.slice(0, 15),
            relations: relations.slice(0, 20),
            concepts,
        };
    }
    buildExtractionPrompt(messages) {
        const content = messages
            .slice(-10)
            .map((m) => `[${m.role}]: ${m.content.slice(0, 300)}`)
            .join('\n\n');
        return `Analyze the following conversation and extract:
1. Key entities (people, technologies, concepts, files, functions)
2. Important decisions made
3. Relationships between entities

Conversation:
${content}

Respond in JSON format:
{
  "entities": [{"name": "...", "type": "...", "importance": 0.0-1.0}],
  "decisions": [{"content": "...", "importance": 0.0-1.0}],
  "relations": [{"from": "...", "to": "...", "type": "...", "weight": 0.0-1.0}]
}`;
    }
    parseExtractionResponse(response) {
        try {
            const jsonMatch = response.match(/\{[\s\S]*\}/);
            if (jsonMatch) {
                const parsed = JSON.parse(jsonMatch[0]);
                return {
                    entities: parsed.entities || [],
                    decisions: parsed.decisions || [],
                    relations: parsed.relations || [],
                    concepts: parsed.concepts || [],
                };
            }
        }
        catch {
            // 解析失败，返回空结果
        }
        return { entities: [], decisions: [], relations: [], concepts: [] };
    }
    inferEntityType(entity, context) {
        const lowerContext = context.toLowerCase();
        const lowerEntity = entity.toLowerCase();
        if (lowerContext.includes(`class ${lowerEntity}`))
            return ENTITY_TYPES.CLASS;
        if (lowerContext.includes(`function ${lowerEntity}`))
            return ENTITY_TYPES.FUNCTION;
        if (lowerContext.includes(`${lowerEntity}.ts`) || lowerContext.includes(`${lowerEntity}.js`)) {
            return ENTITY_TYPES.FILE;
        }
        if (/^[A-Z][a-z]+[A-Z]/.test(entity))
            return ENTITY_TYPES.CLASS;
        return ENTITY_TYPES.CONCEPT;
    }
    calculateImportance(entity, messages) {
        let count = 0;
        for (const msg of messages) {
            const matches = msg.content.match(new RegExp(entity, 'gi'));
            if (matches)
                count += matches.length;
        }
        return Math.min(count / messages.length, 1);
    }
    calculateDecisionImportance(decision) {
        const strongKeywords = ['must', 'critical', 'important', '必须', '关键', '重要'];
        const mediumKeywords = ['should', 'recommend', '应该', '建议'];
        const lower = decision.toLowerCase();
        if (strongKeywords.some((kw) => lower.includes(kw)))
            return 0.9;
        if (mediumKeywords.some((kw) => lower.includes(kw)))
            return 0.7;
        return 0.5;
    }
    inferRelationType(context, _e1, _e2) {
        const lower = context.toLowerCase();
        if (lower.includes('uses') || lower.includes('使用'))
            return RELATION_TYPES.USES;
        if (lower.includes('depends') || lower.includes('依赖'))
            return RELATION_TYPES.DEPENDS_ON;
        if (lower.includes('implements') || lower.includes('实现'))
            return RELATION_TYPES.IMPLEMENTS;
        if (lower.includes('extends') || lower.includes('继承'))
            return RELATION_TYPES.EXTENDS;
        if (lower.includes('calls') || lower.includes('调用'))
            return RELATION_TYPES.CALLS;
        return RELATION_TYPES.REFERENCES;
    }
}
//# sourceMappingURL=MapAgent.js.map