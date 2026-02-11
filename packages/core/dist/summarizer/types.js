/**
 * 自动总结机制类型定义
 * Token 计数器 + 80/20 压缩策略
 */
/**
 * 默认压缩配置
 */
export const DEFAULT_COMPRESSION_CONFIG = {
    maxTokens: 4000,
    targetRatio: 0.2,
    preserveSystemPrompt: true,
    preserveRecentMessages: 3,
    extractDecisionSkeleton: true,
};
/**
 * Token 估算常量
 */
export const TOKEN_ESTIMATION = {
    CHARS_PER_TOKEN_EN: 4,
    CHARS_PER_TOKEN_ZH: 1.5,
    OVERHEAD_PER_MESSAGE: 4,
};
//# sourceMappingURL=types.js.map