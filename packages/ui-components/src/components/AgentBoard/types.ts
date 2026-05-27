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
export const BOARD_AGENT_ROLE_CONFIG: Record<BoardAgentRole, { label: string; icon: string; color: string }> = {
  commander: { label: 'Commander', icon: '👑', color: '#9C27B0' },
  coder: { label: 'Coder', icon: '💻', color: '#2196F3' },
  critic: { label: 'Critic', icon: '🔍', color: '#FF9800' },
  reviewer: { label: 'Reviewer', icon: '📝', color: '#4CAF50' },
  expert: { label: 'Expert', icon: '🎓', color: '#607D8B' },
};

/**
 * Agent状态配置
 */
export const BOARD_AGENT_STATUS_CONFIG: Record<BoardAgentStatus, { label: string; color: string; animation?: string }> = {
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
export const VOTE_STATUS_CONFIG: Record<VoteStatus, { label: string; icon: string; color: string }> = {
  pending: { label: 'Pending', icon: '⏳', color: '#9E9E9E' },
  approved: { label: 'Approved', icon: '✅', color: '#4CAF50' },
  rejected: { label: 'Rejected', icon: '❌', color: '#F44336' },
  abstained: { label: 'Abstained', icon: '⏸️', color: '#FF9800' },
};

/**
 * 黑板条目类型配置
 */
export const BLACKBOARD_TYPE_CONFIG: Record<BlackboardEntry['type'], { label: string; icon: string; color: string }> = {
  state: { label: 'State', icon: '📊', color: '#2196F3' },
  proposal: { label: 'Proposal', icon: '💡', color: '#FF9800' },
  decision: { label: 'Decision', icon: '✅', color: '#4CAF50' },
  artifact: { label: 'Artifact', icon: '📦', color: '#9C27B0' },
};

/**
 * 格式化时间戳
 */
export function formatBoardTimestamp(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString();
}

/**
 * 计算投票进度百分比
 */
export function calculateVoteProgress(vote: VoteInfo): {
  approved: number;
  rejected: number;
  pending: number;
  total: number;
} {
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
