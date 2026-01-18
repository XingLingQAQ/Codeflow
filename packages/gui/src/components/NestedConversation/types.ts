/**
 * 嵌套子对话渲染类型定义
 * 支持多层嵌套的子智能体对话框
 */

/**
 * 智能体角色
 */
export type AgentRole = 'commander' | 'coder' | 'critic' | 'sub' | 'expert';

/**
 * 对话状态
 */
export type ConversationStatus = 'pending' | 'running' | 'completed' | 'failed' | 'stopped';

/**
 * 消息类型
 */
export type MessageType = 'thinking' | 'tool_call' | 'tool_result' | 'output' | 'error';

/**
 * 工具调用
 */
export interface ToolCall {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
  result?: unknown;
  status: 'pending' | 'running' | 'completed' | 'failed';
  startTime?: number;
  endTime?: number;
}

/**
 * 子对话消息
 */
export interface SubConversationMessage {
  id: string;
  type: MessageType;
  content: string;
  timestamp: number;
  toolCall?: ToolCall;
}

/**
 * 子对话
 */
export interface SubConversation {
  id: string;
  parentId?: string;
  agentRole: AgentRole;
  agentName: string;
  status: ConversationStatus;
  messages: SubConversationMessage[];
  children: SubConversation[];
  depth: number;
  startTime: number;
  endTime?: number;
  isExpanded: boolean;
  error?: string;
}

/**
 * 嵌套对话框 Props
 */
export interface NestedConversationBoxProps {
  conversation: SubConversation;
  maxDepth?: number;
  onToggleExpand?: (conversationId: string) => void;
  onStop?: (conversationId: string) => void;
  onRetry?: (conversationId: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 子对话消息 Props
 */
export interface SubMessageProps {
  message: SubConversationMessage;
  agentRole: AgentRole;
}

/**
 * 工具调用卡片 Props
 */
export interface ToolCallCardProps {
  toolCall: ToolCall;
}

/**
 * 嵌套对话容器 Props
 */
export interface NestedConversationContainerProps {
  conversations: SubConversation[];
  maxDepth?: number;
  onToggleExpand?: (conversationId: string) => void;
  onStop?: (conversationId: string) => void;
  onRetry?: (conversationId: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 智能体角色配置
 */
export const AGENT_ROLE_CONFIG: Record<AgentRole, { label: string; icon: string; color: string }> = {
  commander: { label: 'Commander', icon: '👑', color: '#9C27B0' },
  coder: { label: 'Coder', icon: '💻', color: '#2196F3' },
  critic: { label: 'Critic', icon: '🔍', color: '#FF9800' },
  sub: { label: 'Sub Agent', icon: '🤖', color: '#4CAF50' },
  expert: { label: 'Expert', icon: '🎓', color: '#607D8B' },
};

/**
 * 状态配置
 */
export const STATUS_CONFIG: Record<ConversationStatus, { label: string; color: string }> = {
  pending: { label: 'Pending', color: '#9E9E9E' },
  running: { label: 'Running', color: '#2196F3' },
  completed: { label: 'Completed', color: '#4CAF50' },
  failed: { label: 'Failed', color: '#F44336' },
  stopped: { label: 'Stopped', color: '#FF9800' },
};

/**
 * 消息类型配置
 */
export const MESSAGE_TYPE_CONFIG: Record<MessageType, { label: string; icon: string; color: string }> = {
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
export function calculateIndent(depth: number, baseIndent = 24): number {
  return depth * baseIndent;
}

/**
 * 格式化持续时间
 */
export function formatDuration(startTime: number, endTime?: number): string {
  const end = endTime || Date.now();
  const duration = end - startTime;

  if (duration < 1000) return `${duration}ms`;
  if (duration < 60000) return `${(duration / 1000).toFixed(1)}s`;
  return `${Math.floor(duration / 60000)}m ${Math.floor((duration % 60000) / 1000)}s`;
}
