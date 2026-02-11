/**
 * SAMG 图谱类型定义
 * S-P-O 三元组存储格式，符合 JSON-LD 标准
 */
/**
 * JSON-LD 上下文定义
 */
export interface JsonLdContext {
    '@vocab'?: string;
    '@base'?: string;
    [key: string]: string | Record<string, string> | undefined;
}
/**
 * S-P-O 三元组（Subject-Predicate-Object）
 */
export interface Triple {
    '@id': string;
    subject: TripleNode;
    predicate: string;
    object: TripleNode | LiteralValue;
    confidence: number;
    timestamp: number;
    source: TripleSource;
    metadata?: Record<string, unknown>;
}
/**
 * 三元组节点（实体引用）
 */
export interface TripleNode {
    '@id': string;
    '@type'?: string | string[];
    label?: string;
}
/**
 * 字面量值
 */
export interface LiteralValue {
    '@value': string | number | boolean;
    '@type'?: string;
    '@language'?: string;
}
/**
 * 三元组来源
 */
export interface TripleSource {
    sessionId: string;
    messageIndex?: number;
    agentRole?: string;
    gitCommitHash?: string;
    extractionMethod: 'llm' | 'rule' | 'user' | 'inferred';
}
/**
 * JSON-LD 文档（图谱）
 */
export interface JsonLdGraph {
    '@context': JsonLdContext;
    '@id': string;
    '@type': 'Graph';
    '@graph': Triple[];
    metadata: GraphMetadata;
}
/**
 * 图谱元数据
 */
export interface GraphMetadata {
    createdAt: number;
    updatedAt: number;
    tripleCount: number;
    entityCount: number;
    predicateCount: number;
    version: string;
}
/**
 * 实体定义
 */
export interface Entity {
    '@id': string;
    '@type': string | string[];
    label: string;
    description?: string;
    properties?: Record<string, unknown>;
    aliases?: string[];
    createdAt: number;
    updatedAt: number;
}
/**
 * 谓词定义
 */
export interface Predicate {
    '@id': string;
    label: string;
    description?: string;
    domain?: string[];
    range?: string[];
    inverse?: string;
    transitive?: boolean;
    symmetric?: boolean;
}
/**
 * 三元组查询条件
 */
export interface TripleQuery {
    subject?: string | Partial<TripleNode>;
    predicate?: string;
    object?: string | Partial<TripleNode> | Partial<LiteralValue>;
    minConfidence?: number;
    source?: Partial<TripleSource>;
    limit?: number;
    offset?: number;
}
/**
 * 三元组存储配置
 */
export interface TripleStoreConfig {
    graphId: string;
    baseUri: string;
    vocabUri: string;
    enableDeduplication: boolean;
    enableInference: boolean;
    maxTriples: number;
}
/**
 * 三元组存储接口
 */
export interface ITripleStore {
    add(triples: Triple[]): Promise<void>;
    get(id: string): Promise<Triple | null>;
    update(id: string, triple: Partial<Triple>): Promise<void>;
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
}
/**
 * 三元组提取器接口
 */
export interface ITripleExtractor {
    extract(text: string, context: {
        sessionId: string;
        messageIndex?: number;
    }): Promise<Triple[]>;
}
/**
 * 默认配置
 */
export declare const DEFAULT_TRIPLE_STORE_CONFIG: TripleStoreConfig;
/**
 * 预定义谓词
 */
export declare const PREDICATES: {
    readonly CALLS: "codeflow:calls";
    readonly IMPORTS: "codeflow:imports";
    readonly EXTENDS: "codeflow:extends";
    readonly IMPLEMENTS: "codeflow:implements";
    readonly DEFINES: "codeflow:defines";
    readonly USES: "codeflow:uses";
    readonly DEPENDS_ON: "codeflow:dependsOn";
    readonly MENTIONS: "codeflow:mentions";
    readonly REFERENCES: "codeflow:references";
    readonly DECIDES: "codeflow:decides";
    readonly CREATES: "codeflow:creates";
    readonly MODIFIES: "codeflow:modifies";
    readonly DELETES: "codeflow:deletes";
    readonly IS_A: "rdf:type";
    readonly SUBCLASS_OF: "rdfs:subClassOf";
    readonly RELATED_TO: "codeflow:relatedTo";
    readonly SAME_AS: "owl:sameAs";
    readonly DERIVED_FROM: "codeflow:derivedFrom";
};
/**
 * 预定义实体类型
 */
export declare const SAMG_ENTITY_TYPES: {
    readonly FILE: "codeflow:File";
    readonly CLASS: "codeflow:Class";
    readonly FUNCTION: "codeflow:Function";
    readonly VARIABLE: "codeflow:Variable";
    readonly MODULE: "codeflow:Module";
    readonly PACKAGE: "codeflow:Package";
    readonly DECISION: "codeflow:Decision";
    readonly REQUIREMENT: "codeflow:Requirement";
    readonly ISSUE: "codeflow:Issue";
    readonly FEATURE: "codeflow:Feature";
    readonly BUG: "codeflow:Bug";
    readonly CONCEPT: "codeflow:Concept";
    readonly TECHNOLOGY: "codeflow:Technology";
    readonly PATTERN: "codeflow:Pattern";
};
/**
 * 生成三元组 ID
 */
export declare function generateTripleId(subject: string, predicate: string, object: string): string;
/**
 * 生成实体 ID
 */
export declare function generateEntityId(type: string, label: string): string;
/**
 * 判断是否为字面量值
 */
export declare function isLiteralValue(value: TripleNode | LiteralValue): value is LiteralValue;
/**
 * 创建三元组节点
 */
export declare function createNode(id: string, type?: string, label?: string): TripleNode;
/**
 * 创建字面量值
 */
export declare function createLiteral(value: string | number | boolean, type?: string, language?: string): LiteralValue;
//# sourceMappingURL=types.d.ts.map