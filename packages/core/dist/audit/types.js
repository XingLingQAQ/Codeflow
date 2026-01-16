/**
 * 审计与合规类型定义
 * 不可篡改审计日志 + 哈希链
 */
/**
 * 默认保留策略
 */
export const DEFAULT_RETENTION_POLICY = {
    maxAgeDays: 365,
    maxEntries: 10000000,
    archiveBeforeDelete: true,
};
/**
 * 创世块哈希（链的起点）
 */
export const GENESIS_HASH = '0000000000000000000000000000000000000000000000000000000000000000';
//# sourceMappingURL=types.js.map