/**
 * Plan模式任务看板类型定义
 * 任务列表+详情面板支持拖拽排序与批量模型切换
 */
/**
 * 任务状态配置
 */
export const TASK_STATUS_CONFIG = {
    pending: { label: 'Pending', color: '#9E9E9E', icon: '⏳' },
    in_progress: { label: 'In Progress', color: '#2196F3', icon: '🔄' },
    completed: { label: 'Completed', color: '#4CAF50', icon: '✅' },
    blocked: { label: 'Blocked', color: '#F44336', icon: '🚫' },
    cancelled: { label: 'Cancelled', color: '#757575', icon: '❌' },
};
/**
 * 优先级配置
 */
export const TASK_PRIORITY_CONFIG = {
    P0: { label: 'Critical', color: '#F44336' },
    P1: { label: 'High', color: '#FF9800' },
    P2: { label: 'Medium', color: '#2196F3' },
    P3: { label: 'Low', color: '#9E9E9E' },
};
/**
 * 模型配置
 */
export const MODEL_CONFIG = {
    'claude-opus': { label: 'Claude Opus', icon: '🎭', color: '#9C27B0' },
    'claude-sonnet': { label: 'Claude Sonnet', icon: '🎵', color: '#673AB7' },
    'gpt-4': { label: 'GPT-4', icon: '🤖', color: '#10a37f' },
    'gemini-pro': { label: 'Gemini Pro', icon: '💎', color: '#4285f4' },
    codex: { label: 'Codex', icon: '💻', color: '#FF6B6B' },
};
/**
 * 格式化时间
 */
export function formatTaskTime(minutes) {
    if (!minutes)
        return '-';
    if (minutes < 60)
        return `${minutes}m`;
    const hours = Math.floor(minutes / 60);
    const mins = minutes % 60;
    return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}
/**
 * 计算任务进度
 */
export function calculateTaskProgress(tasks) {
    const total = tasks.length;
    const completed = tasks.filter(t => t.status === 'completed').length;
    const inProgress = tasks.filter(t => t.status === 'in_progress').length;
    const blocked = tasks.filter(t => t.status === 'blocked').length;
    const percentage = total > 0 ? (completed / total) * 100 : 0;
    return { total, completed, inProgress, blocked, percentage };
}
/**
 * 按优先级排序
 */
export function sortByPriority(tasks) {
    const priorityOrder = { P0: 0, P1: 1, P2: 2, P3: 3 };
    return [...tasks].sort((a, b) => priorityOrder[a.priority] - priorityOrder[b.priority]);
}
//# sourceMappingURL=types.js.map