/**
 * 辩论式校验界面类型定义
 * Generator与Critic对抗模式可视化
 */
/**
 * 角色配置
 */
export const DEBATE_ROLE_CONFIG = {
    generator: { label: 'Generator', icon: '🎨', color: '#2196F3' },
    critic: { label: 'Critic', icon: '🔍', color: '#FF9800' },
};
/**
 * 轮次状态配置
 */
export const ROUND_STATUS_CONFIG = {
    pending: { label: 'Pending', color: '#9E9E9E' },
    generating: { label: 'Generating', color: '#2196F3' },
    critiquing: { label: 'Critiquing', color: '#FF9800' },
    refining: { label: 'Refining', color: '#9C27B0' },
    completed: { label: 'Completed', color: '#4CAF50' },
};
/**
 * 冲突严重程度配置
 */
export const CONFLICT_SEVERITY_CONFIG = {
    low: { label: 'Low', color: '#4CAF50', icon: '⚪' },
    medium: { label: 'Medium', color: '#FF9800', icon: '🟡' },
    high: { label: 'High', color: '#F44336', icon: '🔴' },
    critical: { label: 'Critical', color: '#9C27B0', icon: '🟣' },
};
/**
 * 格式化时间
 */
export function formatDebateTime(timestamp) {
    return new Date(timestamp).toLocaleTimeString();
}
/**
 * 计算辩论进度
 */
export function calculateDebateProgress(session) {
    if (session.rounds.length === 0)
        return 0;
    const completedRounds = session.rounds.filter(r => r.status === 'completed').length;
    return (completedRounds / session.rounds.length) * 100;
}
/**
 * 统计冲突
 */
export function countConflicts(session) {
    const conflicts = [];
    session.rounds.forEach(round => {
        round.generatorMessage?.conflicts?.forEach(c => conflicts.push(c));
        round.criticMessage?.conflicts?.forEach(c => conflicts.push(c));
    });
    const bySeverity = {
        low: 0,
        medium: 0,
        high: 0,
        critical: 0,
    };
    conflicts.forEach(c => {
        bySeverity[c.severity]++;
    });
    return {
        total: conflicts.length,
        resolved: conflicts.filter(c => c.resolved).length,
        bySeverity,
    };
}
//# sourceMappingURL=types.js.map