/**
 * 多智能体协作看板类型定义
 * 黑板模式协作空间
 */
/**
 * Agent角色配置
 */
export const BOARD_AGENT_ROLE_CONFIG = {
    commander: { label: 'Commander', icon: '👑', color: '#9C27B0' },
    coder: { label: 'Coder', icon: '💻', color: '#2196F3' },
    critic: { label: 'Critic', icon: '🔍', color: '#FF9800' },
    reviewer: { label: 'Reviewer', icon: '📝', color: '#4CAF50' },
    expert: { label: 'Expert', icon: '🎓', color: '#607D8B' },
};
/**
 * Agent状态配置
 */
export const BOARD_AGENT_STATUS_CONFIG = {
    idle: { label: 'Idle', color: '#9E9E9E' },
    thinking: { label: 'Thinking', color: '#9C27B0', animation: 'pulse' },
    executing: { label: 'Executing', color: '#2196F3', animation: 'pulse' },
    voting: { label: 'Voting', color: '#FF9800' },
    completed: { label: 'Completed', color: '#4CAF50' },
    error: { label: 'Error', color: '#F44336' },
};
/**
 * 投票状态配置
 */
export const VOTE_STATUS_CONFIG = {
    pending: { label: 'Pending', icon: '⏳', color: '#9E9E9E' },
    approved: { label: 'Approved', icon: '✅', color: '#4CAF50' },
    rejected: { label: 'Rejected', icon: '❌', color: '#F44336' },
    abstained: { label: 'Abstained', icon: '⏸️', color: '#FF9800' },
};
/**
 * 黑板条目类型配置
 */
export const BLACKBOARD_TYPE_CONFIG = {
    state: { label: 'State', icon: '📊', color: '#2196F3' },
    proposal: { label: 'Proposal', icon: '💡', color: '#FF9800' },
    decision: { label: 'Decision', icon: '✅', color: '#4CAF50' },
    artifact: { label: 'Artifact', icon: '📦', color: '#9C27B0' },
};
/**
 * 格式化时间戳
 */
export function formatBoardTimestamp(timestamp) {
    return new Date(timestamp).toLocaleTimeString();
}
/**
 * 计算投票进度百分比
 */
export function calculateVoteProgress(vote) {
    const approved = vote.votes.filter(v => v.status === 'approved').length;
    const rejected = vote.votes.filter(v => v.status === 'rejected').length;
    const pending = vote.votes.filter(v => v.status === 'pending').length;
    const total = vote.totalVoters;
    return {
        approved: (approved / total) * 100,
        rejected: (rejected / total) * 100,
        pending: (pending / total) * 100,
        total,
    };
}
//# sourceMappingURL=types.js.map