/**
 * SAMG 图谱类型定义
 * S-P-O 三元组存储格式，符合 JSON-LD 标准
 */
/**
 * 默认配置
 */
export const DEFAULT_TRIPLE_STORE_CONFIG = {
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
};
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
};
/**
 * 生成三元组 ID
 */
export function generateTripleId(subject, predicate, object) {
    const hash = simpleHash(`${subject}|${predicate}|${object}`);
    return `triple:${hash}`;
}
/**
 * 生成实体 ID
 */
export function generateEntityId(type, label) {
    const hash = simpleHash(`${type}|${label}`);
    return `entity:${hash}`;
}
/**
 * 简单哈希函数
 */
function simpleHash(str) {
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
export function isLiteralValue(value) {
    return '@value' in value;
}
/**
 * 创建三元组节点
 */
export function createNode(id, type, label) {
    const node = { '@id': id };
    if (type)
        node['@type'] = type;
    if (label)
        node.label = label;
    return node;
}
/**
 * 创建字面量值
 */
export function createLiteral(value, type, language) {
    const literal = { '@value': value };
    if (type)
        literal['@type'] = type;
    if (language)
        literal['@language'] = language;
    return literal;
}
//# sourceMappingURL=types.js.map