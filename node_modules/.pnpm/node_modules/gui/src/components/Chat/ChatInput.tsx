import React, { useState, useCallback, KeyboardEvent } from 'react';
import { ChatInputProps } from './types';

const containerStyle: React.CSSProperties = {
  display: 'flex',
  gap: '8px',
  padding: '12px',
  borderTop: '1px solid #E0E0E0',
  backgroundColor: '#FFFFFF',
};

const textareaStyle: React.CSSProperties = {
  flex: 1,
  minHeight: '40px',
  maxHeight: '120px',
  padding: '10px 14px',
  border: '1px solid #E0E0E0',
  borderRadius: '20px',
  fontSize: '14px',
  fontFamily: 'inherit',
  resize: 'none',
  outline: 'none',
  lineHeight: '1.4',
};

const buttonStyle: React.CSSProperties = {
  padding: '10px 20px',
  backgroundColor: '#007AFF',
  color: '#FFFFFF',
  border: 'none',
  borderRadius: '20px',
  fontSize: '14px',
  fontWeight: 500,
  cursor: 'pointer',
  transition: 'background-color 0.2s',
};

const buttonDisabledStyle: React.CSSProperties = {
  ...buttonStyle,
  backgroundColor: '#CCCCCC',
  cursor: 'not-allowed',
};

export const ChatInput: React.FC<ChatInputProps> = ({
  onSend,
  disabled = false,
  placeholder = 'Type a message...',
}) => {
  const [value, setValue] = useState('');

  const handleSend = useCallback(() => {
    const trimmed = value.trim();
    if (trimmed && !disabled) {
      onSend(trimmed);
      setValue('');
    }
  }, [value, disabled, onSend]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend]
  );

  const handleChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setValue(e.target.value);
  }, []);

  return (
    <div style={containerStyle}>
      <textarea
        style={textareaStyle}
        value={value}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        disabled={disabled}
        rows={1}
      />
      <button
        style={disabled || !value.trim() ? buttonDisabledStyle : buttonStyle}
        onClick={handleSend}
        disabled={disabled || !value.trim()}
      >
        Send
      </button>
    </div>
  );
};
