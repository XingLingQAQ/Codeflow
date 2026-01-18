/**
 * 辩论式校验界面类型定义
 * Generator与Critic对抗模式可视化
 */

/**
 * 参与者角色
 */
export type DebateRole = 'generator' | 'critic';

/**
 * 辩论轮次状态
 */
export type RoundStatus = 'pending' | 'generating' | 'critiquing' | 'refining' | 'completed';

/**
 * 冲突严重程度
 */
export type ConflictSeverity = 'low' | 'medium' | 'high' | 'critical';

/**
 * 冲突点
 */
export interface ConflictPoint {
  id: string;
  roundId: string;
  position: { start: number; end: number };
  generatorText: string;
  criticText: string;
  severity: ConflictSeverity;
  description: string;
  resolved: boolean;
  resolution?: string;
}

/**
 * 辩论消息
 */
export interface DebateMessage {
  id: string;
  roundId: string;
  role: DebateRole;
  content: string;
  timestamp: number;
  conflicts?: ConflictPoint[];
  refinedFrom?: string;
}

/**
 * 辩论轮次
 */
export interface DebateRound {
  id: string;
  index: number;
  status: RoundStatus;
  generatorMessage?: DebateMessage;
  criticMessage?: DebateMessage;
  refinedMessage?: DebateMessage;
  startTime: number;
  endTime?: number;
}

/**
 * 辩论会话
 */
export interface DebateSession {
  id: string;
  topic: string;
  description: string;
  rounds: DebateRound[];
  currentRoundIndex: number;
  status: 'active' | 'paused' | 'completed' | 'cancelled';
  selectedSolution?: string;
  startTime: number;
  endTime?: number;
}

/**
 * 辩论消息气泡Props
 */
export interface DebateBubbleProps {
  message: DebateMessage;
  onConflictClick?: (conflictId: string) => void;
}

/**
 * 辩论时间轴Props
 */
export interface DebateTimelineProps {
  rounds: DebateRound[];
  currentRoundIndex: number;
  onRoundSelect?: (roundIndex: number) => void;
}

/**
 * 冲突详情面板Props
 */
export interface ConflictPanelProps {
  conflict: ConflictPoint;
  onResolve?: (conflictId: string, resolution: string) => void;
  onClose?: () => void;
}

/**
 * 辩论界面Props
 */
export interface DebateViewProps {
  session: DebateSession;
  onSelectSolution?: (roundId: string, messageId: string) => void;
  onExportReport?: () => void;
  onConflictResolve?: (conflictId: string, resolution: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 角色配置
 */
export const DEBATE_ROLE_CONFIG: Record<DebateRole, { label: string; icon: string; color: string }> = {
  generator: { label: 'Generator', icon: '🎨', color: '#2196F3' },
  critic: { label: 'Critic', icon: '🔍', color: '#FF9800' },
};

/**
 * 轮次状态配置
 */
export const ROUND_STATUS_CONFIG: Record<RoundStatus, { label: string; color: string }> = {
  pending: { label: 'Pending', color: '#9E9E9E' },
  generating: { label: 'Generating', color: '#2196F3' },
  critiquing: { label: 'Critiquing', color: '#FF9800' },
  refining: { label: 'Refining', color: '#9C27B0' },
  completed: { label: 'Completed', color: '#4CAF50' },
};

/**
 * 冲突严重程度配置
 */
export const CONFLICT_SEVERITY_CONFIG: Record<ConflictSeverity, { label: string; color: string; icon: string }> = {
  low: { label: 'Low', color: '#4CAF50', icon: '⚪' },
  medium: { label: 'Medium', color: '#FF9800', icon: '🟡' },
  high: { label: 'High', color: '#F44336', icon: '🔴' },
  critical: { label: 'Critical', color: '#9C27B0', icon: '🟣' },
};

/**
 * 格式化时间
 */
export function formatDebateTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString();
}

/**
 * 计算辩论进度
 */
export function calculateDebateProgress(session: DebateSession): number {
  if (session.rounds.length === 0) return 0;
  const completedRounds = session.rounds.filter(r => r.status === 'completed').length;
  return (completedRounds / session.rounds.length) * 100;
}

/**
 * 统计冲突
 */
export function countConflicts(session: DebateSession): {
  total: number;
  resolved: number;
  bySeverity: Record<ConflictSeverity, number>;
} {
  const conflicts: ConflictPoint[] = [];
  session.rounds.forEach(round => {
    round.generatorMessage?.conflicts?.forEach(c => conflicts.push(c));
    round.criticMessage?.conflicts?.forEach(c => conflicts.push(c));
  });

  const bySeverity: Record<ConflictSeverity, number> = {
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
