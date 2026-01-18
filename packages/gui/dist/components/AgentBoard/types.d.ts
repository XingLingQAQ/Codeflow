/**
 * 多智能体协作看板类型定义
 * 黑板模式协作空间
 */
/**
 * Agent状态
 */
export type BoardAgentStatus = 'idle' | 'thinking' | 'executing' | 'voting' | 'completed' | 'error';
/**
 * Agent角色
 */
export type BoardAgentRole = 'commander' | 'coder' | 'critic' | 'reviewer' | 'expert';
/**
 * 投票状态
 */
export type VoteStatus = 'pending' | 'approved' | 'rejected' | 'abstained';
/**
 * Agent信息
 */
export interface AgentInfo {
    id: string;
    name: string;
    role: BoardAgentRole;
    status: BoardAgentStatus;
    avatar?: string;
    currentTask?: string;
    lastActivity: number;
    logs: AgentLog[];
    isExpanded?: boolean;
}
/**
 * Agent日志
 */
export interface AgentLog {
    id: string;
    timestamp: number;
    type: 'info' | 'action' | 'decision' | 'error';
    message: string;
    details?: Record<string, unknown>;
}
/**
 * 黑板条目
 */
export interface BlackboardEntry {
    id: string;
    key: string;
    value: unknown;
    author: string;
    timestamp: number;
    version: number;
    type: 'state' | 'proposal' | 'decision' | 'artifact';
}
/**
 * 投票信息
 */
export interface VoteInfo {
    id: string;
    proposal: string;
    description: string;
    initiator: string;
    startTime: number;
    endTime?: number;
    status: 'active' | 'passed' | 'rejected' | 'timeout';
    votes: AgentVote[];
    requiredApprovals: number;
    totalVoters: number;
}
/**
 * Agent投票
 */
export interface AgentVote {
    agentId: string;
    agentName: string;
    status: VoteStatus;
    timestamp?: number;
    reason?: string;
}
/**
 * Agent卡片Props
 */
export interface AgentCardProps {
    agent: AgentInfo;
    isSelected?: boolean;
    onSelect?: (agentId: string) => void;
    onToggleExpand?: (agentId: string) => void;
}
/**
 * 黑板区域Props
 */
export interface BlackboardAreaProps {
    entries: BlackboardEntry[];
    onEntryClick?: (entryId: string) => void;
}
/**
 * 投票进度Props
 */
export interface VotingProgressProps {
    vote: VoteInfo;
    onVoteAction?: (voteId: string, action: 'approve' | 'reject') => void;
}
/**
 * Agent看板Props
 */
export interface AgentBoardProps {
    agents: AgentInfo[];
    blackboard: BlackboardEntry[];
    currentVote?: VoteInfo;
    onAgentSelect?: (agentId: string) => void;
    onAgentToggleExpand?: (agentId: string) => void;
    onBlackboardEntryClick?: (entryId: string) => void;
    onVoteAction?: (voteId: string, action: 'approve' | 'reject') => void;
    className?: string;
    style?: React.CSSProperties;
}
/**
 * Agent角色配置
 */
export declare const BOARD_AGENT_ROLE_CONFIG: Record<BoardAgentRole, {
    label: string;
    icon: string;
    color: string;
}>;
/**
 * Agent状态配置
 */
export declare const BOARD_AGENT_STATUS_CONFIG: Record<BoardAgentStatus, {
    label: string;
    color: string;
    animation?: string;
}>;
/**
 * 投票状态配置
 */
export declare const VOTE_STATUS_CONFIG: Record<VoteStatus, {
    label: string;
    icon: string;
    color: string;
}>;
/**
 * 黑板条目类型配置
 */
export declare const BLACKBOARD_TYPE_CONFIG: Record<BlackboardEntry['type'], {
    label: string;
    icon: string;
    color: string;
}>;
/**
 * 格式化时间戳
 */
export declare function formatBoardTimestamp(timestamp: number): string;
/**
 * 计算投票进度百分比
 */
export declare function calculateVoteProgress(vote: VoteInfo): {
    approved: number;
    rejected: number;
    pending: number;
    total: number;
};
//# sourceMappingURL=types.d.ts.map