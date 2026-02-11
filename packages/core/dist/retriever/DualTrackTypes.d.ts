/**
 * 双轨记忆协同类型定义
 * 向量存储 + 图谱存储混合检索
 */
import { ChunkMetadata } from '../memory/types.js';
import { Triple, TripleNode } from '../samg/types.js';
/**
 * 双轨搜索配置
 */
export interface DualTrackSearchConfig {
    vectorWeight: number;
    graphWeight: number;
    keywordWeight: number;
    topK: number;
    minScore: number;
    enableSpreadingActivation: boolean;
    spreadingDepth: number;
    spreadingDecay: number;
}
/**
 * 图谱搜索结果
 */
export interface GraphSearchResult {
    entity: TripleNode;
    relatedTriples: Triple[];
    score: number;
    path?: string[];
    activationLevel?: number;
}
/**
 * 双轨搜索结果
 */
export interface DualTrackSearchResult {
    id: string;
    content: string;
    score: number;
    vectorScore?: number;
    graphScore?: number;
    keywordScore?: number;
    source: 'vector' | 'graph' | 'keyword' | 'hybrid';
    metadata?: ChunkMetadata;
    entity?: TripleNode;
    relatedTriples?: Triple[];
    highlights?: string[];
}
/**
 * 双轨搜索参数
 */
export interface DualTrackSearchParams {
    query: string;
    sessionId?: string;
    entityTypes?: string[];
    predicates?: string[];
    timeRange?: {
        start: number;
        end: number;
    };
    limit?: number;
    searchMode?: 'vector' | 'graph' | 'hybrid';
}
/**
 * 双轨搜索响应
 */
export interface DualTrackSearchResponse {
    results: DualTrackSearchResult[];
    totalCount: number;
    vectorCount: number;
    graphCount: number;
    queryTime: number;
    searchMode: 'vector' | 'graph' | 'hybrid';
}
/**
 * 扩展激活节点
 */
export interface ActivatedNode {
    id: string;
    label: string;
    type: string;
    activationLevel: number;
    depth: number;
    path: string[];
}
/**
 * 双轨记忆协同接口
 */
export interface IDualTrackMemory {
    hybridSearch(params: DualTrackSearchParams): Promise<DualTrackSearchResponse>;
    vectorSearch(query: string, limit?: number): Promise<DualTrackSearchResult[]>;
    graphSearch(query: string, limit?: number): Promise<GraphSearchResult[]>;
    spreadingActivation(seedEntities: string[], depth?: number, decay?: number): Promise<ActivatedNode[]>;
    findRelatedEntities(entityId: string, predicates?: string[]): Promise<GraphSearchResult[]>;
    findPath(fromEntityId: string, toEntityId: string, maxDepth?: number): Promise<string[][]>;
}
/**
 * 默认双轨搜索配置
 */
export declare const DEFAULT_DUAL_TRACK_CONFIG: DualTrackSearchConfig;
//# sourceMappingURL=DualTrackTypes.d.ts.map