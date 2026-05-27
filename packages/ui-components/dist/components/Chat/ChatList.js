import { jsx as _jsx } from "react/jsx-runtime";
import { useRef, useEffect, useCallback } from 'react';
import { ChatBubble } from './ChatBubble';
const containerStyle = {
    flex: 1,
    overflowY: 'auto',
    padding: '16px',
    display: 'flex',
    flexDirection: 'column',
    gap: '4px',
};
const emptyStyle = {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    color: '#999',
    fontSize: '14px',
};
export const ChatList = ({ messages, onRetry }) => {
    const containerRef = useRef(null);
    const lastMessageRef = useRef(null);
    // 自动滚动到底部
    const scrollToBottom = useCallback(() => {
        if (lastMessageRef.current) {
            lastMessageRef.current.scrollIntoView({ behavior: 'smooth' });
        }
    }, []);
    // 当消息列表变化时滚动到底部
    useEffect(() => {
        scrollToBottom();
    }, [messages.length, scrollToBottom]);
    // 检测最后一条消息是否正在流式输出
    const isLastMessageStreaming = useCallback((index, message) => {
        return index === messages.length - 1 && message.status === 'streaming';
    }, [messages.length]);
    if (messages.length === 0) {
        return (_jsx("div", { style: containerStyle, children: _jsx("div", { style: emptyStyle, children: "Start a conversation..." }) }));
    }
    return (_jsx("div", { ref: containerRef, style: containerStyle, children: messages.map((message, index) => (_jsx("div", { ref: index === messages.length - 1 ? lastMessageRef : undefined, children: _jsx(ChatBubble, { message: message, isStreaming: isLastMessageStreaming(index, message) }) }, message.id))) }));
};
//# sourceMappingURL=ChatList.js.map