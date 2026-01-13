import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState, useCallback } from 'react';
const containerStyle = {
    display: 'flex',
    gap: '8px',
    padding: '12px',
    borderTop: '1px solid #E0E0E0',
    backgroundColor: '#FFFFFF',
};
const textareaStyle = {
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
const buttonStyle = {
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
const buttonDisabledStyle = {
    ...buttonStyle,
    backgroundColor: '#CCCCCC',
    cursor: 'not-allowed',
};
export const ChatInput = ({ onSend, disabled = false, placeholder = 'Type a message...', }) => {
    const [value, setValue] = useState('');
    const handleSend = useCallback(() => {
        const trimmed = value.trim();
        if (trimmed && !disabled) {
            onSend(trimmed);
            setValue('');
        }
    }, [value, disabled, onSend]);
    const handleKeyDown = useCallback((e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSend();
        }
    }, [handleSend]);
    const handleChange = useCallback((e) => {
        setValue(e.target.value);
    }, []);
    return (_jsxs("div", { style: containerStyle, children: [_jsx("textarea", { style: textareaStyle, value: value, onChange: handleChange, onKeyDown: handleKeyDown, placeholder: placeholder, disabled: disabled, rows: 1 }), _jsx("button", { style: disabled || !value.trim() ? buttonDisabledStyle : buttonStyle, onClick: handleSend, disabled: disabled || !value.trim(), children: "Send" })] }));
};
//# sourceMappingURL=ChatInput.js.map