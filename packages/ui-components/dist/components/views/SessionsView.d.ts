/**
 * SessionsView - 会话视图
 * 分栏布局、移动端视图切换、消息气泡样式
 */
import React from 'react';
export interface Message {
    id: string;
    role: 'user' | 'assistant';
    content: string;
    timestamp: Date;
}
export interface Session {
    id: string;
    title: string;
    lastMessage: string;
    timestamp: Date;
    messages: Message[];
}
export interface SessionsViewProps {
    sessions?: Session[];
    activeSessionId?: string;
    onSelectSession?: (sessionId: string) => void;
    onSendMessage?: (sessionId: string, message: string) => void;
    onNewSession?: () => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const SessionsView: React.FC<SessionsViewProps>;
export default SessionsView;
//# sourceMappingURL=SessionsView.d.ts.map