import React from 'react';
import { ChatContainerProps } from './types';
import { ChatList } from './ChatList';
import { ChatInput } from './ChatInput';

const containerStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  height: '100%',
  width: '100%',
  backgroundColor: '#FFFFFF',
  borderRadius: '8px',
  boxShadow: '0 2px 8px rgba(0, 0, 0, 0.1)',
  overflow: 'hidden',
};

const headerStyle: React.CSSProperties = {
  padding: '16px',
  borderBottom: '1px solid #E0E0E0',
  backgroundColor: '#FAFAFA',
};

const titleStyle: React.CSSProperties = {
  margin: 0,
  fontSize: '16px',
  fontWeight: 600,
  color: '#1A1A1A',
};

const loadingStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  padding: '8px',
  color: '#666',
  fontSize: '12px',
};

export const ChatContainer: React.FC<ChatContainerProps> = ({
  messages,
  onSend,
  isLoading = false,
}) => {
  return (
    <div style={containerStyle}>
      <div style={headerStyle}>
        <h2 style={titleStyle}>CodeFlow Chat</h2>
      </div>
      <ChatList messages={messages} />
      {isLoading && (
        <div style={loadingStyle}>
          <span>AI is thinking...</span>
        </div>
      )}
      <ChatInput onSend={onSend} disabled={isLoading} placeholder="Ask anything..." />
    </div>
  );
};
