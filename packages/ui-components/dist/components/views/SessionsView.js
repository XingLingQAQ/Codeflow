import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
/**
 * SessionsView - 会话视图
 * 分栏布局、移动端视图切换、消息气泡样式
 */
import { useState, useEffect, useRef, useCallback } from 'react';
import { colors, spacing, borderRadius, fontSize, fontWeight, shadows, transitions, breakpoints, } from '../shared/tokens';
import { Button } from '../shared/Button';
import { Avatar } from '../shared/Avatar';
// Icons
const PlusIcon = () => (_jsxs("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("line", { x1: "12", y1: "5", x2: "12", y2: "19" }), _jsx("line", { x1: "5", y1: "12", x2: "19", y2: "12" })] }));
const SendIcon = () => (_jsxs("svg", { width: "18", height: "18", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("line", { x1: "22", y1: "2", x2: "11", y2: "13" }), _jsx("polygon", { points: "22 2 15 22 11 13 2 9 22 2" })] }));
const BackIcon = () => (_jsx("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("polyline", { points: "15 18 9 12 15 6" }) }));
const MessageIcon = () => (_jsx("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("path", { d: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" }) }));
// Demo data
const demoSessions = [
    {
        id: '1',
        title: 'React Performance',
        lastMessage: 'How can I optimize re-renders?',
        timestamp: new Date(Date.now() - 1000 * 60 * 5),
        messages: [
            { id: 'm1', role: 'user', content: 'How can I optimize re-renders in React?', timestamp: new Date(Date.now() - 1000 * 60 * 10) },
            { id: 'm2', role: 'assistant', content: 'There are several strategies to optimize re-renders in React:\n\n1. **Use React.memo()** for functional components\n2. **useMemo()** for expensive calculations\n3. **useCallback()** for callback functions\n4. **Proper key usage** in lists\n5. **State colocation** - keep state close to where it\'s used', timestamp: new Date(Date.now() - 1000 * 60 * 5) },
        ],
    },
    {
        id: '2',
        title: 'TypeScript Generics',
        lastMessage: 'Explain generic constraints',
        timestamp: new Date(Date.now() - 1000 * 60 * 60),
        messages: [
            { id: 'm3', role: 'user', content: 'Can you explain TypeScript generic constraints?', timestamp: new Date(Date.now() - 1000 * 60 * 65) },
            { id: 'm4', role: 'assistant', content: 'Generic constraints in TypeScript allow you to limit the types that can be used with a generic. Use the `extends` keyword:\n\n```typescript\nfunction getProperty<T extends object, K extends keyof T>(obj: T, key: K) {\n  return obj[key];\n}\n```', timestamp: new Date(Date.now() - 1000 * 60 * 60) },
        ],
    },
    {
        id: '3',
        title: 'API Design',
        lastMessage: 'REST vs GraphQL comparison',
        timestamp: new Date(Date.now() - 1000 * 60 * 60 * 24),
        messages: [],
    },
];
const formatTime = (date) => {
    const now = new Date();
    const diff = now.getTime() - date.getTime();
    const minutes = Math.floor(diff / (1000 * 60));
    const hours = Math.floor(diff / (1000 * 60 * 60));
    const days = Math.floor(diff / (1000 * 60 * 60 * 24));
    if (minutes < 1)
        return 'Just now';
    if (minutes < 60)
        return `${minutes}m ago`;
    if (hours < 24)
        return `${hours}h ago`;
    return `${days}d ago`;
};
// Session List Item
const SessionListItem = ({ session, isActive, onClick }) => (_jsxs("button", { onClick: onClick, style: {
        display: 'flex',
        alignItems: 'flex-start',
        gap: spacing[3],
        width: '100%',
        padding: `${spacing[3]}px ${spacing[4]}px`,
        backgroundColor: isActive ? colors.primary[50] : 'transparent',
        border: 'none',
        borderLeft: isActive ? `3px solid ${colors.primary[500]}` : '3px solid transparent',
        cursor: 'pointer',
        transition: transitions.fast,
        textAlign: 'left',
    }, onMouseEnter: (e) => {
        if (!isActive)
            e.currentTarget.style.backgroundColor = colors.slate[50];
    }, onMouseLeave: (e) => {
        if (!isActive)
            e.currentTarget.style.backgroundColor = 'transparent';
    }, children: [_jsx("div", { style: {
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 32,
                height: 32,
                backgroundColor: isActive ? colors.primary[100] : colors.slate[100],
                borderRadius: borderRadius.lg,
                color: isActive ? colors.primary[600] : colors.slate[500],
                flexShrink: 0,
            }, children: _jsx(MessageIcon, {}) }), _jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsx("div", { style: {
                        fontSize: fontSize.sm,
                        fontWeight: fontWeight.semibold,
                        color: isActive ? colors.primary[700] : colors.slate[700],
                        marginBottom: spacing[0.5],
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                    }, children: session.title }), _jsx("div", { style: {
                        fontSize: fontSize.xs,
                        color: colors.slate[500],
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                    }, children: session.lastMessage })] }), _jsx("span", { style: {
                fontSize: fontSize.xs,
                color: colors.slate[400],
                flexShrink: 0,
            }, children: formatTime(session.timestamp) })] }));
// Message Bubble
const MessageBubble = ({ message }) => {
    const isUser = message.role === 'user';
    return (_jsxs("div", { style: {
            display: 'flex',
            flexDirection: isUser ? 'row-reverse' : 'row',
            gap: spacing[3],
            marginBottom: spacing[4],
        }, children: [_jsx(Avatar, { name: isUser ? 'You' : 'AI', size: "sm", style: {
                    backgroundColor: isUser ? colors.primary[500] : colors.slate[700],
                } }), _jsx("div", { style: {
                    maxWidth: '70%',
                    padding: `${spacing[3]}px ${spacing[4]}px`,
                    backgroundColor: isUser ? colors.primary[500] : '#fff',
                    color: isUser ? '#fff' : colors.slate[700],
                    borderRadius: borderRadius.xl,
                    borderTopRightRadius: isUser ? borderRadius.sm : borderRadius.xl,
                    borderTopLeftRadius: isUser ? borderRadius.xl : borderRadius.sm,
                    boxShadow: isUser ? 'none' : shadows.sm,
                    fontSize: fontSize.sm,
                    lineHeight: 1.6,
                    whiteSpace: 'pre-wrap',
                }, children: message.content })] }));
};
export const SessionsView = ({ sessions = demoSessions, activeSessionId, onSelectSession, onSendMessage, onNewSession, className, style, }) => {
    const [isMobile, setIsMobile] = useState(false);
    const [showChat, setShowChat] = useState(false);
    const [inputValue, setInputValue] = useState('');
    const [currentSessionId, setCurrentSessionId] = useState(activeSessionId || sessions[0]?.id);
    const messagesEndRef = useRef(null);
    const currentSession = sessions.find((s) => s.id === currentSessionId);
    useEffect(() => {
        const checkMobile = () => {
            const mobile = window.innerWidth < breakpoints.md;
            setIsMobile(mobile);
            if (!mobile)
                setShowChat(false);
        };
        checkMobile();
        window.addEventListener('resize', checkMobile);
        return () => window.removeEventListener('resize', checkMobile);
    }, []);
    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }, [currentSession?.messages]);
    const handleSelectSession = useCallback((sessionId) => {
        setCurrentSessionId(sessionId);
        onSelectSession?.(sessionId);
        if (isMobile)
            setShowChat(true);
    }, [isMobile, onSelectSession]);
    const handleSendMessage = useCallback(() => {
        if (inputValue.trim() && currentSessionId) {
            onSendMessage?.(currentSessionId, inputValue.trim());
            setInputValue('');
        }
    }, [inputValue, currentSessionId, onSendMessage]);
    const handleKeyDown = useCallback((e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSendMessage();
        }
    }, [handleSendMessage]);
    // Session list panel
    const SessionList = (_jsxs("div", { style: {
            width: isMobile ? '100%' : 320,
            height: '100%',
            backgroundColor: '#fff',
            borderRight: isMobile ? 'none' : `1px solid ${colors.slate[200]}`,
            display: 'flex',
            flexDirection: 'column',
            flexShrink: 0,
        }, children: [_jsxs("div", { style: {
                    padding: `${spacing[4]}px ${spacing[4]}px`,
                    borderBottom: `1px solid ${colors.slate[100]}`,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }, children: [_jsx("h2", { style: {
                            fontSize: fontSize.lg,
                            fontWeight: fontWeight.bold,
                            color: colors.slate[800],
                        }, children: "Sessions" }), _jsxs(Button, { variant: "ghost", size: "sm", onClick: onNewSession, style: { display: 'flex', alignItems: 'center', gap: spacing[1] }, children: [_jsx(PlusIcon, {}), _jsx("span", { children: "New" })] })] }), _jsx("div", { style: { flex: 1, overflowY: 'auto' }, children: sessions.map((session) => (_jsx(SessionListItem, { session: session, isActive: session.id === currentSessionId, onClick: () => handleSelectSession(session.id) }, session.id))) })] }));
    // Chat panel
    const ChatPanel = (_jsxs("div", { style: {
            flex: 1,
            display: 'flex',
            flexDirection: 'column',
            backgroundColor: colors.slate[50],
            minWidth: 0,
        }, children: [_jsxs("div", { style: {
                    padding: `${spacing[3]}px ${spacing[4]}px`,
                    backgroundColor: '#fff',
                    borderBottom: `1px solid ${colors.slate[200]}`,
                    display: 'flex',
                    alignItems: 'center',
                    gap: spacing[3],
                }, children: [isMobile && (_jsx("button", { onClick: () => setShowChat(false), style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            padding: spacing[1],
                            backgroundColor: 'transparent',
                            border: 'none',
                            cursor: 'pointer',
                            color: colors.slate[600],
                        }, children: _jsx(BackIcon, {}) })), _jsx("h3", { style: {
                            fontSize: fontSize.base,
                            fontWeight: fontWeight.semibold,
                            color: colors.slate[800],
                        }, children: currentSession?.title || 'Select a session' })] }), _jsxs("div", { style: {
                    flex: 1,
                    overflowY: 'auto',
                    padding: spacing[4],
                }, children: [currentSession?.messages.map((message) => (_jsx(MessageBubble, { message: message }, message.id))), _jsx("div", { ref: messagesEndRef })] }), _jsx("div", { style: {
                    padding: spacing[4],
                    backgroundColor: '#fff',
                    borderTop: `1px solid ${colors.slate[200]}`,
                }, children: _jsxs("div", { style: {
                        display: 'flex',
                        gap: spacing[2],
                        alignItems: 'flex-end',
                    }, children: [_jsx("textarea", { value: inputValue, onChange: (e) => setInputValue(e.target.value), onKeyDown: handleKeyDown, placeholder: "Type a message...", style: {
                                flex: 1,
                                minHeight: 44,
                                maxHeight: 120,
                                padding: `${spacing[2]}px ${spacing[3]}px`,
                                fontSize: fontSize.sm,
                                fontFamily: 'inherit',
                                border: `1px solid ${colors.slate[200]}`,
                                borderRadius: borderRadius.xl,
                                backgroundColor: colors.slate[50],
                                resize: 'none',
                                outline: 'none',
                                transition: transitions.fast,
                            }, onFocus: (e) => {
                                e.target.style.borderColor = colors.primary[400];
                                e.target.style.backgroundColor = '#fff';
                            }, onBlur: (e) => {
                                e.target.style.borderColor = colors.slate[200];
                                e.target.style.backgroundColor = colors.slate[50];
                            } }), _jsx(Button, { variant: "primary", onClick: handleSendMessage, disabled: !inputValue.trim(), style: {
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                width: 44,
                                height: 44,
                                padding: 0,
                                borderRadius: borderRadius.full,
                            }, children: _jsx(SendIcon, {}) })] }) })] }));
    return (_jsx("div", { className: className, style: {
            display: 'flex',
            height: '100%',
            ...style,
        }, children: isMobile ? (showChat ? (ChatPanel) : (SessionList)) : (_jsxs(_Fragment, { children: [SessionList, ChatPanel] })) }));
};
export default SessionsView;
//# sourceMappingURL=SessionsView.js.map