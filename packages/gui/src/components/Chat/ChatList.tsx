import React, { useRef, useEffect, useCallback } from 'react';
import { ChatListProps, ChatMessage } from './types';
import { ChatBubble } from './ChatBubble';

const containerStyle: React.CSSProperties = {
  flex: 1,
  overflowY: 'auto',
  padding: '16px',
  display: 'flex',
  flexDirection: 'column',
  gap: '4px',
};

const emptyStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  height: '100%',
  color: '#999',
  fontSize: '14px',
};

export const ChatList: React.FC<ChatListProps> = ({ messages, onRetry }) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const lastMessageRef = useRef<HTMLDivElement>(null);

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
  const isLastMessageStreaming = useCallback(
    (index: number, message: ChatMessage): boolean => {
      return index === messages.length - 1 && message.status === 'streaming';
    },
    [messages.length]
  );

  if (messages.length === 0) {
    return (
      <div style={containerStyle}>
        <div style={emptyStyle}>Start a conversation...</div>
      </div>
    );
  }

  return (
    <div ref={containerRef} style={containerStyle}>
      {messages.map((message, index) => (
        <div key={message.id} ref={index === messages.length - 1 ? lastMessageRef : undefined}>
          <ChatBubble message={message} isStreaming={isLastMessageStreaming(index, message)} />
        </div>
      ))}
    </div>
  );
};
