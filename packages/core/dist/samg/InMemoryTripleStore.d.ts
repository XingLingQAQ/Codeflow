/**
 * 内存三元组存储实现
 * 支持 JSON-LD 格式的 S-P-O 三元组存储与查询
 */
import { Triple, TripleQuery, TripleStoreConfig, ITripleStore, Entity, JsonLdGraph, GraphMetadata } from './types.js';
export declare class InMemoryTripleStore implements ITripleStore {
    private triples;
    private entities;
    private subjectIndex;
    private predicateIndex;
    private objectIndex;
    private config;
    constructor(config?: Partial<TripleStoreConfig>);
    add(triples: Triple[]): Promise<void>;
    get(id: string): Promise<Triple | null>;
    update(id: string, updates: Partial<Triple>): Promise<void>;
    delete(ids: string[]): Promise<void>;
    clear(): Promise<void>;
    query(query: TripleQuery): Promise<Triple[]>;
    findBySubject(subjectId: string): Promise<Triple[]>;
    findByPredicate(predicate: string): Promise<Triple[]>;
    findByObject(objectId: string): Promise<Triple[]>;
    getEntity(id: string): Promise<Entity | null>;
    getEntities(): Promise<Entity[]>;
    upsertEntity(entity: Entity): Promise<void>;
    exportGraph(): Promise<JsonLdGraph>;
    importGraph(graph: JsonLdGraph): Promise<void>;
    getStats(): Promise<GraphMetadata>;
    deduplicate(): Promise<number>;
    private findDuplicate;
    private getTripleKey;
    private addToIndices;
    private removeFromIndices;
    private updateEntitiesFromTriple;
    private intersect;
}
//# sourceMappingURL=InMemoryTripleStore.d.ts.map