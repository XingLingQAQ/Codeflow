/**
 * 记忆管理仪表盘类型定义
 * 双栏布局：STM（短期记忆池）+ LTM（长期规则库）
 */
/**
 * 惊喜度颜色映射
 */
export const SURPRISE_COLORS = {
    low: '#4CAF50', // 绿色 0-0.3
    medium: '#FF9800', // 橙色 0.3-0.7
    high: '#F44336', // 红色 0.7-1.0
};
/**
 * 根据惊喜度获取颜色
 */
export function getSurpriseColor(value) {
    if (value >= 0.7)
        return SURPRISE_COLORS.high;
    if (value >= 0.3)
        return SURPRISE_COLORS.medium;
    return SURPRISE_COLORS.low;
}
/**
 * 热度颜色映射
 */
export const HEAT_COLORS = {
    cold: '#90CAF9', // 冷 0-0.3
    warm: '#FFB74D', // 温 0.3-0.7
    hot: '#EF5350', // 热 0.7-1.0
};
/**
 * 根据热度获取颜色
 */
export function getHeatColor(value) {
    if (value >= 0.7)
        return HEAT_COLORS.hot;
    if (value >= 0.3)
        return HEAT_COLORS.warm;
    return HEAT_COLORS.cold;
}
/**
 * 排序选项
 */
export const SORT_OPTIONS = [
    { field: 'timestamp', label: 'Time' },
    { field: 'heat', label: 'Heat' },
    { field: 'surprise', label: 'Surprise' },
];
//# sourceMappingURL=types.js.map