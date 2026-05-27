/**
 * Chat 组件类型定义
 */
export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'system';
    content: string;
    timestamp: number;
    status?: 'pending' | 'streaming' | 'complete' | 'error';
    model?: string;
}
export interface ChatBubbleProps {
    message: ChatMessage;
    isStreaming?: boolean;
}
export interface ChatInputProps {
    onSend: (content: string) => void;
    disabled?: boolean;
    placeholder?: string;
}
export interface ChatListProps {
    messages: ChatMessage[];
    onRetry?: (messageId: string) => void;
}
export interface ChatContainerProps {
    messages: ChatMessage[];
    onSend: (content: string) => void;
    isLoading?: boolean;
}
//# sourceMappingURL=types.d.ts.map