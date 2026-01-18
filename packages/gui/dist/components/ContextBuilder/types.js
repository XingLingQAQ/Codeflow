/**
 * 动态上下文构建器类型定义
 * 左侧文件树 + 右侧AST树 + 底部Token预算
 */
/**
 * AST节点类型图标映射
 */
export const AST_TYPE_ICONS = {
    function: 'ƒ',
    class: 'C',
    variable: 'V',
    interface: 'I',
    type: 'T',
    import: '→',
    export: '←',
    method: 'M',
    property: 'P',
};
/**
 * AST节点类型颜色映射
 */
export const AST_TYPE_COLORS = {
    function: '#2196F3',
    class: '#9C27B0',
    variable: '#4CAF50',
    interface: '#FF9800',
    type: '#00BCD4',
    import: '#607D8B',
    export: '#795548',
    method: '#3F51B5',
    property: '#8BC34A',
};
/**
 * Token预算分类颜色
 */
export const BUDGET_COLORS = {
    systemPrompt: '#2196F3',
    recentDialog: '#4CAF50',
    toolSchema: '#FF9800',
    outputSpace: '#9C27B0',
    contextSelection: '#00BCD4',
    available: '#E0E0E0',
};
/**
 * 计算Token使用百分比
 */
export function calculateUsagePercent(budget) {
    return budget.total > 0 ? (budget.used / budget.total) * 100 : 0;
}
/**
 * 判断是否超出预算
 */
export function isOverBudget(budget) {
    return budget.used > budget.total;
}
/**
 * 判断是否接近预算上限
 */
export function isNearBudget(budget, threshold = 0.9) {
    return budget.used >= budget.total * threshold;
}
//# sourceMappingURL=types.js.map