import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
const userBubbleStyle = {
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
const assistantBubbleStyle = {
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
const systemBubbleStyle = {
    maxWidth: '90%',
    padding: '8px 12px',
    borderRadius: '8px',
    backgroundColor: '#FFF3CD',
    color: '#856404',
    margin: '8px auto',
    textAlign: 'center',
    fontSize: '0.875rem',
};
const containerStyle = {
    display: 'flex',
    flexDirection: 'column',
    width: '100%',
};
const labelStyle = {
    fontSize: '0.75rem',
    color: '#666',
    marginBottom: '4px',
};
const timestampStyle = {
    fontSize: '0.625rem',
    color: '#999',
    marginTop: '4px',
};
const streamingIndicatorStyle = {
    display: 'inline-block',
    width: '8px',
    height: '8px',
    borderRadius: '50%',
    backgroundColor: '#007AFF',
    marginLeft: '8px',
    animation: 'pulse 1s infinite',
};
export const ChatBubble = ({ message, isStreaming }) => {
    const { role, content, timestamp, model } = message;
    const getBubbleStyle = () => {
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
    const getAlignment = () => {
        return {
            alignItems: role === 'user' ? 'flex-end' : role === 'system' ? 'center' : 'flex-start',
        };
    };
    const formatTimestamp = (ts) => {
        const date = new Date(ts);
        return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    };
    const getLabel = () => {
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
    return (_jsxs("div", { style: { ...containerStyle, ...getAlignment() }, children: [role !== 'system' && _jsx("span", { style: labelStyle, children: getLabel() }), _jsxs("div", { style: getBubbleStyle(), children: [content, isStreaming && _jsx("span", { style: streamingIndicatorStyle })] }), role !== 'system' && _jsx("span", { style: timestampStyle, children: formatTimestamp(timestamp) })] }));
};
//# sourceMappingURL=ChatBubble.js.map