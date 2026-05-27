import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { ChatList } from './ChatList';
import { ChatInput } from './ChatInput';
const containerStyle = {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    width: '100%',
    backgroundColor: '#FFFFFF',
    borderRadius: '8px',
    boxShadow: '0 2px 8px rgba(0, 0, 0, 0.1)',
    overflow: 'hidden',
};
const headerStyle = {
    padding: '16px',
    borderBottom: '1px solid #E0E0E0',
    backgroundColor: '#FAFAFA',
};
const titleStyle = {
    margin: 0,
    fontSize: '16px',
    fontWeight: 600,
    color: '#1A1A1A',
};
const loadingStyle = {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '8px',
    color: '#666',
    fontSize: '12px',
};
export const ChatContainer = ({ messages, onSend, isLoading = false, }) => {
    return (_jsxs("div", { style: containerStyle, children: [_jsx("div", { style: headerStyle, children: _jsx("h2", { style: titleStyle, children: "CodeFlow Chat" }) }), _jsx(ChatList, { messages: messages }), isLoading && (_jsx("div", { style: loadingStyle, children: _jsx("span", { children: "AI is thinking..." }) })), _jsx(ChatInput, { onSend: onSend, disabled: isLoading, placeholder: "Ask anything..." })] }));
};
//# sourceMappingURL=ChatContainer.js.map