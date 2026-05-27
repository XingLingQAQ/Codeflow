/**
 * PlanModePanel Types - Plan 模式面板类型定义
 */
// Phase 配置
export const PHASE_CONFIG = {
    vision: { icon: '💡', color: '#f59e0b', label: 'Vision' },
    constraints: { icon: '🔒', color: '#8b5cf6', label: 'Constraints' },
    architecture: { icon: '🏗️', color: '#3b82f6', label: 'Architecture' },
    research: { icon: '🔍', color: '#06b6d4', label: 'Research' },
    explore: { icon: '🧭', color: '#10b981', label: 'Explore' },
    review: { icon: '👁️', color: '#6366f1', label: 'Review' },
    implement: { icon: '⚡', color: '#ec4899', label: 'Implement' },
    qa: { icon: '✅', color: '#22c55e', label: 'QA' },
};
// Artifact 配置
export const ARTIFACT_CONFIG = {
    proposal: { icon: '📄', color: '#3b82f6', label: 'Proposal' },
    spec: { icon: '📋', color: '#8b5cf6', label: 'Specification' },
    design: { icon: '🎨', color: '#ec4899', label: 'Design' },
    task: { icon: '✅', color: '#22c55e', label: 'Tasks' },
    roadmap: { icon: '🗺️', color: '#f59e0b', label: 'Roadmap' },
    architecture: { icon: '🏛️', color: '#06b6d4', label: 'Architecture' },
};
// Constraint 类型配置
export const CONSTRAINT_TYPE_CONFIG = {
    functional: { icon: '⚙️', color: '#3b82f6', label: 'Functional' },
    technical: { icon: '🔧', color: '#8b5cf6', label: 'Technical' },
    business: { icon: '💼', color: '#f59e0b', label: 'Business' },
    security: { icon: '🔐', color: '#ef4444', label: 'Security' },
};
// 格式化时间
export const formatDuration = (ms) => {
    if (ms < 1000)
        return `${ms}ms`;
    if (ms < 60000)
        return `${(ms / 1000).toFixed(1)}s`;
    return `${(ms / 60000).toFixed(1)}m`;
};
//# sourceMappingURL=types.js.map