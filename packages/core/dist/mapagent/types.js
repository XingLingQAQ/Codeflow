/**
 * 压缩前导图类型定义
 * Map Agent 调用逻辑与决策骨架提取
 */
/**
 * 默认 Map Agent 配置
 */
export const DEFAULT_MAP_AGENT_CONFIG = {
    extractEntities: true,
    extractDecisions: true,
    extractRelations: true,
    maxNodes: 50,
    minImportance: 0.3,
};
/**
 * 实体类型映射
 */
export const ENTITY_TYPES = {
    PERSON: 'person',
    ORGANIZATION: 'organization',
    LOCATION: 'location',
    TECHNOLOGY: 'technology',
    CONCEPT: 'concept',
    FILE: 'file',
    FUNCTION: 'function',
    CLASS: 'class',
    VARIABLE: 'variable',
};
/**
 * 关系类型映射
 */
export const RELATION_TYPES = {
    USES: 'uses',
    DEPENDS_ON: 'depends_on',
    IMPLEMENTS: 'implements',
    EXTENDS: 'extends',
    CALLS: 'calls',
    REFERENCES: 'references',
    DECIDES: 'decides',
    CREATES: 'creates',
    MODIFIES: 'modifies',
};
//# sourceMappingURL=types.js.map