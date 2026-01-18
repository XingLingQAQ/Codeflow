/**
 * 嵌套子对话渲染类型定义
 * 支持多层嵌套的子智能体对话框
 */
/**
 * 智能体角色配置
 */
export const AGENT_ROLE_CONFIG = {
    commander: { label: 'Commander', icon: '👑', color: '#9C27B0' },
    coder: { label: 'Coder', icon: '💻', color: '#2196F3' },
    critic: { label: 'Critic', icon: '🔍', color: '#FF9800' },
    sub: { label: 'Sub Agent', icon: '🤖', color: '#4CAF50' },
    expert: { label: 'Expert', icon: '🎓', color: '#607D8B' },
};
/**
 * 状态配置
 */
export const STATUS_CONFIG = {
    pending: { label: 'Pending', color: '#9E9E9E' },
    running: { label: 'Running', color: '#2196F3' },
    completed: { label: 'Completed', color: '#4CAF50' },
    failed: { label: 'Failed', color: '#F44336' },
    stopped: { label: 'Stopped', color: '#FF9800' },
};
/**
 * 消息类型配置
 */
export const MESSAGE_TYPE_CONFIG = {
    thinking: { label: 'Thinking', icon: '💭', color: '#9C27B0' },
    tool_call: { label: 'Tool Call', icon: '🔧', color: '#2196F3' },
    tool_result: { label: 'Tool Result', icon: '📋', color: '#4CAF50' },
    output: { label: 'Output', icon: '💬', color: '#333' },
    error: { label: 'Error', icon: '❌', color: '#F44336' },
};
/**
 * 最大嵌套深度
 */
export const MAX_NESTING_DEPTH = 3;
/**
 * 计算嵌套缩进
 */
export function calculateIndent(depth, baseIndent = 24) {
    return depth * baseIndent;
}
/**
 * 格式化持续时间
 */
export function formatDuration(startTime, endTime) {
    const end = endTime || Date.now();
    const duration = end - startTime;
    if (duration < 1000)
        return `${duration}ms`;
    if (duration < 60000)
        return `${(duration / 1000).toFixed(1)}s`;
    return `${Math.floor(duration / 60000)}m ${Math.floor((duration % 60000) / 1000)}s`;
}
//# sourceMappingURL=types.js.map