/**
 * 双轨记忆协同实现
 * 融合向量存储和图谱存储的混合检索
 */
import { IDualTrackMemory, DualTrackSearchConfig, DualTrackSearchParams, DualTrackSearchResponse, DualTrackSearchResult, GraphSearchResult, ActivatedNode } from './DualTrackTypes.js';
import { IVectorStore } from '../memory/types.js';
import { ITripleStore } from '../samg/types.js';
import { ISemanticRetriever } from './types.js';
export declare class DualTrackMemory implements IDualTrackMemory {
    private config;
    private vectorStore?;
    private tripleStore?;
    private semanticRetriever?;
    constructor(config?: Partial<DualTrackSearchConfig>);
    setVectorStore(store: IVectorStore): void;
    setTripleStore(store: ITripleStore): void;
    setSemanticRetriever(retriever: ISemanticRetriever): void;
    hybridSearch(params: DualTrackSearchParams): Promise<DualTrackSearchResponse>;
    vectorSearch(query: string, limit?: number): Promise<DualTrackSearchResult[]>;
    graphSearch(query: string, limit?: number): Promise<GraphSearchResult[]>;
    spreadingActivation(seedEntities: string[], depth?: number, decay?: number): Promise<ActivatedNode[]>;
    findRelatedEntities(entityId: string, predicates?: string[]): Promise<GraphSearchResult[]>;
    findPath(fromEntityId: string, toEntityId: string, maxDepth?: number): Promise<string[][]>;
    private mergeResults;
    private buildContentFromGraph;
    private applyFilters;
    private tokenize;
}
//# sourceMappingURL=DualTrackMemory.d.ts.map