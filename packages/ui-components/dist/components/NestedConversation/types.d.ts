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
export declare const AGENT_ROLE_CONFIG: Record<AgentRole, {
    label: string;
    icon: string;
    color: string;
}>;
/**
 * 状态配置
 */
export declare const STATUS_CONFIG: Record<ConversationStatus, {
    label: string;
    color: string;
}>;
/**
 * 消息类型配置
 */
export declare const MESSAGE_TYPE_CONFIG: Record<MessageType, {
    label: string;
    icon: string;
    color: string;
}>;
/**
 * 最大嵌套深度
 */
export declare const MAX_NESTING_DEPTH = 3;
/**
 * 计算嵌套缩进
 */
export declare function calculateIndent(depth: number, baseIndent?: number): number;
/**
 * 格式化持续时间
 */
export declare function formatDuration(startTime: number, endTime?: number): string;
//# sourceMappingURL=types.d.ts.map