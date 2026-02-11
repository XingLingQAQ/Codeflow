/**
 * 压缩前导图类型定义
 * Map Agent 调用逻辑与决策骨架提取
 */
import { Message, DecisionSkeleton } from '../hooks/types.js';
import { ICliAdapter } from '../adapters/types.js';
/**
 * 导图节点类型
 */
export type MapNodeType = 'entity' | 'decision' | 'action' | 'concept' | 'reference';
/**
 * 导图节点
 */
export interface MapNode {
    id: string;
    type: MapNodeType;
    label: string;
    content: string;
    importance: number;
    timestamp: number;
    metadata?: Record<string, unknown>;
}
/**
 * 导图边
 */
export interface MapEdge {
    id: string;
    source: string;
    target: string;
    type: string;
    weight: number;
    label?: string;
}
/**
 * 压缩前导图
 */
export interface CompressionMap {
    id: string;
    sessionId: string;
    nodes: MapNode[];
    edges: MapEdge[];
    skeleton: DecisionSkeleton;
    createdAt: number;
    messageRange: {
        start: number;
        end: number;
    };
    metadata?: Record<string, unknown>;
}
/**
 * Map Agent 配置
 */
export interface MapAgentConfig {
    adapter?: ICliAdapter;
    extractEntities: boolean;
    extractDecisions: boolean;
    extractRelations: boolean;
    maxNodes: number;
    minImportance: number;
}
/**
 * 提取结果
 */
export interface ExtractionResult {
    entities: Array<{
        name: string;
        type: string;
        importance: number;
    }>;
    decisions: Array<{
        content: string;
        importance: number;
        context?: string;
    }>;
    relations: Array<{
        from: string;
        to: string;
        type: string;
        weight: number;
    }>;
    concepts: Array<{
        name: string;
        description: string;
    }>;
}
/**
 * Map Agent 接口
 */
export interface IMapAgent {
    extract(messages: Message[]): Promise<ExtractionResult>;
    buildMap(messages: Message[], sessionId: string): Promise<CompressionMap>;
    mergeMap(existing: CompressionMap, newMap: CompressionMap): CompressionMap;
}
/**
 * 导图存储接口
 */
export interface IMapStorage {
    save(map: CompressionMap): Promise<void>;
    load(id: string): Promise<CompressionMap | null>;
    loadBySession(sessionId: string): Promise<CompressionMap[]>;
    delete(id: string): Promise<void>;
    list(): Promise<Array<{
        id: string;
        sessionId: string;
        createdAt: number;
    }>>;
}
/**
 * 默认 Map Agent 配置
 */
export declare const DEFAULT_MAP_AGENT_CONFIG: MapAgentConfig;
/**
 * 实体类型映射
 */
export declare const ENTITY_TYPES: Record<string, string>;
/**
 * 关系类型映射
 */
export declare const RELATION_TYPES: Record<string, string>;
//# sourceMappingURL=types.d.ts.map