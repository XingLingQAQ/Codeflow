import React from 'react';
import { ChatBubbleProps } from './types';

const userBubbleStyle: React.CSSProperties = {
  maxWidth: '80%',
  padding: '12px 16px',
  borderRadius: '18px 18px 4px 18px',
  backgroundColor: '#007AFF',
  color: '#FFFFFF',
  marginLeft: 'auto',
  marginBottom: '8px',
  wordBreak: 'break-word',
  whiteSpace: 'pre-wrap',
};

const assistantBubbleStyle: React.CSSProperties = {
  maxWidth: '80%',
  padding: '12px 16px',
  borderRadius: '18px 18px 18px 4px',
  backgroundColor: '#F0F0F0',
  color: '#1A1A1A',
  marginRight: 'auto',
  marginBottom: '8px',
  wordBreak: 'break-word',
  whiteSpace: 'pre-wrap',
};

const systemBubbleStyle: React.CSSProperties = {
  maxWidth: '90%',
  padding: '8px 12px',
  borderRadius: '8px',
  backgroundColor: '#FFF3CD',
  color: '#856404',
  margin: '8px auto',
  textAlign: 'center',
  fontSize: '0.875rem',
};

const containerStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  width: '100%',
};

const labelStyle: React.CSSProperties = {
  fontSize: '0.75rem',
  color: '#666',
  marginBottom: '4px',
};

const timestampStyle: React.CSSProperties = {
  fontSize: '0.625rem',
  color: '#999',
  marginTop: '4px',
};

const streamingIndicatorStyle: React.CSSProperties = {
  display: 'inline-block',
  width: '8px',
  height: '8px',
  borderRadius: '50%',
  backgroundColor: '#007AFF',
  marginLeft: '8px',
  animation: 'pulse 1s infinite',
};

export const ChatBubble: React.FC<ChatBubbleProps> = ({ message, isStreaming }) => {
  const { role, content, timestamp, model } = message;

  const getBubbleStyle = (): React.CSSProperties => {
    switch (role) {
      case 'user':
        return userBubbleStyle;
      case 'assistant':
        return assistantBubbleStyle;
      case 'system':
        return systemBubbleStyle;
      default:
        return assistantBubbleStyle;
    }
  };

  const getAlignment = (): React.CSSProperties => {
    return {
      alignItems: role === 'user' ? 'flex-end' : role === 'system' ? 'center' : 'flex-start',
    };
  };

  const formatTimestamp = (ts: number): string => {
    const date = new Date(ts);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const getLabel = (): string => {
    switch (role) {
      case 'user':
        return 'You';
      case 'assistant':
        return model || 'AI';
      case 'system':
        return 'System';
      default:
        return '';
    }
  };

  return (
    <div style={{ ...containerStyle, ...getAlignment() }}>
      {role !== 'system' && <span style={labelStyle}>{getLabel()}</span>}
      <div style={getBubbleStyle()}>
        {content}
        {isStreaming && <span style={streamingIndicatorStyle} />}
      </div>
      {role !== 'system' && <span style={timestampStyle}>{formatTimestamp(timestamp)}</span>}
    </div>
  );
};
