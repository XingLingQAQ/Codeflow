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
  // 基础 CRUD
  add(triples: Triple[]): Promise<void>;
  get(id: string): Promise<Triple | null>;
  update(id: string, triple: Partial<Triple>): Promise<void>;
  delete(ids: string[]): Promise<void>;
  clear(): Promise<void>;

  // 查询
  query(query: TripleQuery): Promise<Triple[]>;
  findBySubject(subjectId: string): Promise<Triple[]>;
  findByPredicate(predicate: string): Promise<Triple[]>;
  findByObject(objectId: string): Promise<Triple[]>;

  // 实体操作
  getEntity(id: string): Promise<Entity | null>;
  getEntities(): Promise<Entity[]>;
  upsertEntity(entity: Entity): Promise<void>;

  // 图谱操作
  exportGraph(): Promise<JsonLdGraph>;
  importGraph(graph: JsonLdGraph): Promise<void>;
  getStats(): Promise<GraphMetadata>;

  // 去重
  deduplicate(): Promise<number>;
}

/**
 * 三元组提取器接口
 */
export interface ITripleExtractor {
  extract(
    text: string,
    context: { sessionId: string; messageIndex?: number }
  ): Promise<Triple[]>;
}

/**
 * 默认配置
 */
export const DEFAULT_TRIPLE_STORE_CONFIG: TripleStoreConfig = {
  graphId: 'codeflow:samg',
  baseUri: 'https://codeflow.ai/graph/',
  vocabUri: 'https://codeflow.ai/vocab/',
  enableDeduplication: true,
  enableInference: false,
  maxTriples: 1000000,
};

/**
 * 预定义谓词
 */
export const PREDICATES = {
  // 代码关系
  CALLS: 'codeflow:calls',
  IMPORTS: 'codeflow:imports',
  EXTENDS: 'codeflow:extends',
  IMPLEMENTS: 'codeflow:implements',
  DEFINES: 'codeflow:defines',
  USES: 'codeflow:uses',
  DEPENDS_ON: 'codeflow:dependsOn',

  // 对话关系
  MENTIONS: 'codeflow:mentions',
  REFERENCES: 'codeflow:references',
  DECIDES: 'codeflow:decides',
  CREATES: 'codeflow:creates',
  MODIFIES: 'codeflow:modifies',
  DELETES: 'codeflow:deletes',

  // 知识关系
  IS_A: 'rdf:type',
  SUBCLASS_OF: 'rdfs:subClassOf',
  RELATED_TO: 'codeflow:relatedTo',
  SAME_AS: 'owl:sameAs',
  DERIVED_FROM: 'codeflow:derivedFrom',
} as const;

/**
 * 预定义实体类型
 */
export const SAMG_ENTITY_TYPES = {
  // 代码实体
  FILE: 'codeflow:File',
  CLASS: 'codeflow:Class',
  FUNCTION: 'codeflow:Function',
  VARIABLE: 'codeflow:Variable',
  MODULE: 'codeflow:Module',
  PACKAGE: 'codeflow:Package',

  // 对话实体
  DECISION: 'codeflow:Decision',
  REQUIREMENT: 'codeflow:Requirement',
  ISSUE: 'codeflow:Issue',
  FEATURE: 'codeflow:Feature',
  BUG: 'codeflow:Bug',

  // 概念实体
  CONCEPT: 'codeflow:Concept',
  TECHNOLOGY: 'codeflow:Technology',
  PATTERN: 'codeflow:Pattern',
} as const;

/**
 * 生成三元组 ID
 */
export function generateTripleId(
  subject: string,
  predicate: string,
  object: string
): string {
  const hash = simpleHash(`${subject}|${predicate}|${object}`);
  return `triple:${hash}`;
}

/**
 * 生成实体 ID
 */
export function generateEntityId(type: string, label: string): string {
  const hash = simpleHash(`${type}|${label}`);
  return `entity:${hash}`;
}

/**
 * 简单哈希函数
 */
function simpleHash(str: string): string {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    const char = str.charCodeAt(i);
    hash = (hash << 5) - hash + char;
    hash = hash & hash;
  }
  return Math.abs(hash).toString(36);
}

/**
 * 判断是否为字面量值
 */
export function isLiteralValue(
  value: TripleNode | LiteralValue
): value is LiteralValue {
  return '@value' in value;
}

/**
 * 创建三元组节点
 */
export function createNode(id: string, type?: string, label?: string): TripleNode {
  const node: TripleNode = { '@id': id };
  if (type) node['@type'] = type;
  if (label) node.label = label;
  return node;
}

/**
 * 创建字面量值
 */
export function createLiteral(
  value: string | number | boolean,
  type?: string,
  language?: string
): LiteralValue {
  const literal: LiteralValue = { '@value': value };
  if (type) literal['@type'] = type;
  if (language) literal['@language'] = language;
  return literal;
}
