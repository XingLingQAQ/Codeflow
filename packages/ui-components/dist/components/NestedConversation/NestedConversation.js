import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * 嵌套子对话渲染组件
 * 支持多层嵌套的子智能体对话框
 */
import { useState } from 'react';
import { AGENT_ROLE_CONFIG, STATUS_CONFIG, MESSAGE_TYPE_CONFIG, MAX_NESTING_DEPTH, calculateIndent, formatDuration, } from './types';
/**
 * 工具调用卡片
 */
export const ToolCallCard = ({ toolCall }) => {
    const [isExpanded, setIsExpanded] = useState(false);
    const statusColors = {
        pending: '#9E9E9E',
        running: '#2196F3',
        completed: '#4CAF50',
        failed: '#F44336',
    };
    return (_jsxs("div", { style: {
            padding: '10px 12px',
            backgroundColor: '#f8f9fa',
            border: '1px solid #e0e0e0',
            borderRadius: 6,
            marginTop: 8,
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    cursor: 'pointer',
                }, onClick: () => setIsExpanded(!isExpanded), children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: { fontSize: 14 }, children: "\uD83D\uDD27" }), _jsx("span", { style: { fontSize: 13, fontWeight: 500, color: '#333' }, children: toolCall.name }), _jsx("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 3,
                                    backgroundColor: statusColors[toolCall.status],
                                    color: '#fff',
                                }, children: toolCall.status })] }), _jsx("span", { style: { fontSize: 12, color: '#999' }, children: isExpanded ? '▼' : '▶' })] }), isExpanded && (_jsxs("div", { style: { marginTop: 10 }, children: [_jsxs("div", { style: { marginBottom: 8 }, children: [_jsx("span", { style: { fontSize: 11, color: '#666', fontWeight: 500 }, children: "Arguments:" }), _jsx("pre", { style: {
                                    margin: '4px 0 0 0',
                                    padding: 8,
                                    backgroundColor: '#fff',
                                    border: '1px solid #e0e0e0',
                                    borderRadius: 4,
                                    fontSize: 11,
                                    overflow: 'auto',
                                    maxHeight: 100,
                                }, children: JSON.stringify(toolCall.arguments, null, 2) })] }), toolCall.result !== undefined && (_jsxs("div", { children: [_jsx("span", { style: { fontSize: 11, color: '#666', fontWeight: 500 }, children: "Result:" }), _jsx("pre", { style: {
                                    margin: '4px 0 0 0',
                                    padding: 8,
                                    backgroundColor: toolCall.status === 'failed' ? '#ffebee' : '#e8f5e9',
                                    border: `1px solid ${toolCall.status === 'failed' ? '#ffcdd2' : '#c8e6c9'}`,
                                    borderRadius: 4,
                                    fontSize: 11,
                                    overflow: 'auto',
                                    maxHeight: 150,
                                }, children: typeof toolCall.result === 'string'
                                    ? toolCall.result
                                    : JSON.stringify(toolCall.result, null, 2) })] })), toolCall.startTime && (_jsxs("div", { style: { marginTop: 8, fontSize: 10, color: '#999' }, children: ["Duration: ", formatDuration(toolCall.startTime, toolCall.endTime)] }))] }))] }));
};
/**
 * 子对话消息
 */
export const SubMessage = ({ message, agentRole }) => {
    const config = MESSAGE_TYPE_CONFIG[message.type];
    const agentConfig = AGENT_ROLE_CONFIG[agentRole];
    return (_jsxs("div", { style: {
            padding: '8px 12px',
            marginBottom: 6,
            backgroundColor: message.type === 'error' ? '#ffebee' : '#fff',
            borderLeft: `3px solid ${config.color}`,
            borderRadius: '0 6px 6px 0',
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    marginBottom: 4,
                }, children: [_jsx("span", { style: { fontSize: 12 }, children: config.icon }), _jsx("span", { style: { fontSize: 11, color: '#666', fontWeight: 500 }, children: config.label }), _jsx("span", { style: { fontSize: 10, color: '#999' }, children: new Date(message.timestamp).toLocaleTimeString() })] }), message.type !== 'tool_call' && (_jsx("div", { style: {
                    fontSize: 13,
                    color: message.type === 'error' ? '#c62828' : '#333',
                    lineHeight: 1.5,
                    whiteSpace: 'pre-wrap',
                }, children: message.content })), message.toolCall && _jsx(ToolCallCard, { toolCall: message.toolCall })] }));
};
/**
 * 嵌套对话框
 */
export const NestedConversationBox = ({ conversation, maxDepth = MAX_NESTING_DEPTH, onToggleExpand, onStop, onRetry, className, style, }) => {
    const agentConfig = AGENT_ROLE_CONFIG[conversation.agentRole];
    const statusConfig = STATUS_CONFIG[conversation.status];
    const indent = calculateIndent(conversation.depth);
    const canNest = conversation.depth < maxDepth;
    const isRunning = conversation.status === 'running';
    const isFailed = conversation.status === 'failed';
    return (_jsx("div", { className: className, style: {
            marginLeft: indent,
            marginBottom: 12,
            ...style,
        }, children: _jsxs("div", { style: {
                backgroundColor: '#fafafa',
                border: `1px solid ${agentConfig.color}20`,
                borderLeft: `4px solid ${agentConfig.color}`,
                borderRadius: '0 8px 8px 0',
                overflow: 'hidden',
            }, children: [_jsxs("div", { style: {
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        padding: '10px 14px',
                        backgroundColor: `${agentConfig.color}10`,
                        borderBottom: '1px solid #e0e0e0',
                        cursor: 'pointer',
                    }, onClick: () => onToggleExpand?.(conversation.id), children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 10 }, children: [_jsx("span", { style: {
                                        fontSize: 10,
                                        color: '#666',
                                        transform: conversation.isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                                        transition: 'transform 0.15s',
                                    }, children: "\u25B6" }), _jsx("span", { style: { fontSize: 16 }, children: agentConfig.icon }), _jsx("span", { style: { fontSize: 13, fontWeight: 600, color: '#333' }, children: conversation.agentName }), _jsx("span", { style: {
                                        fontSize: 10,
                                        padding: '2px 8px',
                                        borderRadius: 10,
                                        backgroundColor: statusConfig.color,
                                        color: '#fff',
                                    }, children: statusConfig.label }), _jsxs("span", { style: { fontSize: 11, color: '#999' }, children: [conversation.messages.length, " messages"] })] }), _jsxs("div", { style: { display: 'flex', gap: 8 }, children: [isRunning && (_jsx("button", { onClick: (e) => {
                                        e.stopPropagation();
                                        onStop?.(conversation.id);
                                    }, style: {
                                        padding: '4px 10px',
                                        fontSize: 11,
                                        border: 'none',
                                        borderRadius: 4,
                                        backgroundColor: '#ffebee',
                                        color: '#c62828',
                                        cursor: 'pointer',
                                    }, children: "\u23F9 Stop" })), isFailed && (_jsx("button", { onClick: (e) => {
                                        e.stopPropagation();
                                        onRetry?.(conversation.id);
                                    }, style: {
                                        padding: '4px 10px',
                                        fontSize: 11,
                                        border: 'none',
                                        borderRadius: 4,
                                        backgroundColor: '#e3f2fd',
                                        color: '#1976D2',
                                        cursor: 'pointer',
                                    }, children: "\uD83D\uDD04 Retry" })), _jsx("span", { style: { fontSize: 11, color: '#999' }, children: formatDuration(conversation.startTime, conversation.endTime) })] })] }), conversation.isExpanded && (_jsxs("div", { style: { padding: 12 }, children: [conversation.error && (_jsxs("div", { style: {
                                padding: '10px 12px',
                                marginBottom: 12,
                                backgroundColor: '#ffebee',
                                border: '1px solid #ffcdd2',
                                borderRadius: 6,
                                fontSize: 12,
                                color: '#c62828',
                            }, children: ["\u274C ", conversation.error] })), conversation.messages.length === 0 ? (_jsx("div", { style: {
                                padding: 20,
                                textAlign: 'center',
                                color: '#999',
                                fontSize: 13,
                            }, children: isRunning ? 'Waiting for response...' : 'No messages' })) : (conversation.messages.map((msg) => (_jsx(SubMessage, { message: msg, agentRole: conversation.agentRole }, msg.id)))), canNest && conversation.children.length > 0 && (_jsx("div", { style: { marginTop: 12 }, children: conversation.children.map((child) => (_jsx(NestedConversationBox, { conversation: child, maxDepth: maxDepth, onToggleExpand: onToggleExpand, onStop: onStop, onRetry: onRetry }, child.id))) })), !canNest && conversation.children.length > 0 && (_jsxs("div", { style: {
                                padding: '10px 12px',
                                marginTop: 12,
                                backgroundColor: '#fff3e0',
                                border: '1px solid #ffe0b2',
                                borderRadius: 6,
                                fontSize: 12,
                                color: '#e65100',
                            }, children: ["\u26A0\uFE0F ", conversation.children.length, " nested conversation(s) hidden (max depth: ", maxDepth, ")"] }))] }))] }) }));
};
/**
 * 嵌套对话容器
 */
export const NestedConversationContainer = ({ conversations, maxDepth = MAX_NESTING_DEPTH, onToggleExpand, onStop, onRetry, className, style, }) => {
    return (_jsx("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            ...style,
        }, children: conversations.length === 0 ? (_jsx("div", { style: {
                padding: 40,
                textAlign: 'center',
                color: '#999',
                fontSize: 13,
            }, children: "No sub-conversations" })) : (conversations.map((conv) => (_jsx(NestedConversationBox, { conversation: conv, maxDepth: maxDepth, onToggleExpand: onToggleExpand, onStop: onStop, onRetry: onRetry }, conv.id)))) }));
};
export default NestedConversationContainer;
//# sourceMappingURL=NestedConversation.js.map