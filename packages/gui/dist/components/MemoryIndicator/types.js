/**
 * 记忆预警指示灯类型定义
 */
/**
 * 级别颜色映射
 */
export const LEVEL_COLORS = {
    none: 'transparent',
    low: '#90CAF9',
    medium: '#4CAF50',
    high: '#FF9800',
    critical: '#F44336',
};
/**
 * 级别边框颜色映射
 */
export const LEVEL_BORDER_COLORS = {
    none: '#ddd',
    low: '#90CAF9',
    medium: '#4CAF50',
    high: '#FF9800',
    critical: '#F44336',
};
/**
 * 级别标签映射
 */
export const LEVEL_LABELS = {
    none: 'No matches',
    low: 'Low relevance',
    medium: 'Relevant',
    high: 'Highly relevant',
    critical: 'Critical match',
};
/**
 * 根据分数计算匹配级别
 */
export function scoreToLevel(score) {
    if (score >= 0.9)
        return 'critical';
    if (score >= 0.7)
        return 'high';
    if (score >= 0.5)
        return 'medium';
    if (score >= 0.3)
        return 'low';
    return 'none';
}
//# sourceMappingURL=types.js.map