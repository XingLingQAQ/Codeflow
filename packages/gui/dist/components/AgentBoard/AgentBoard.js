import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * 多智能体协作看板组件
 * 黑板模式协作空间
 */
import { useState, useMemo } from 'react';
import { BOARD_AGENT_ROLE_CONFIG, BOARD_AGENT_STATUS_CONFIG, VOTE_STATUS_CONFIG, BLACKBOARD_TYPE_CONFIG, formatBoardTimestamp, calculateVoteProgress, } from './types';
/**
 * Agent卡片组件
 */
export const AgentCard = ({ agent, isSelected, onSelect, onToggleExpand, }) => {
    const roleConfig = BOARD_AGENT_ROLE_CONFIG[agent.role];
    const statusConfig = BOARD_AGENT_STATUS_CONFIG[agent.status];
    return (_jsxs("div", { style: {
            width: 160,
            backgroundColor: isSelected ? `${roleConfig.color}15` : '#fff',
            border: `2px solid ${isSelected ? roleConfig.color : '#e0e0e0'}`,
            borderRadius: 12,
            overflow: 'hidden',
            cursor: 'pointer',
            transition: 'all 0.2s',
        }, onClick: () => onSelect?.(agent.id), children: [_jsx("div", { style: {
                    padding: '12px 10px',
                    backgroundColor: `${roleConfig.color}10`,
                    borderBottom: '1px solid #e0e0e0',
                }, children: _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: { fontSize: 24 }, children: roleConfig.icon }), _jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsx("div", { style: {
                                        fontSize: 13,
                                        fontWeight: 600,
                                        color: '#333',
                                        overflow: 'hidden',
                                        textOverflow: 'ellipsis',
                                        whiteSpace: 'nowrap',
                                    }, children: agent.name }), _jsx("div", { style: { fontSize: 10, color: '#666' }, children: roleConfig.label })] })] }) }), _jsxs("div", { style: { padding: '10px' }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }, children: [_jsx("span", { style: {
                                    width: 8,
                                    height: 8,
                                    borderRadius: '50%',
                                    backgroundColor: statusConfig.color,
                                    animation: statusConfig.animation === 'pulse' ? 'pulse 1.5s infinite' : 'none',
                                } }), _jsx("span", { style: { fontSize: 11, color: statusConfig.color, fontWeight: 500 }, children: statusConfig.label })] }), agent.currentTask && (_jsx("div", { style: {
                            fontSize: 11,
                            color: '#666',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            marginBottom: 6,
                        }, children: agent.currentTask })), _jsxs("div", { style: { fontSize: 10, color: '#999' }, children: ["Last: ", formatBoardTimestamp(agent.lastActivity)] })] }), _jsx("div", { style: {
                    padding: '8px 10px',
                    borderTop: '1px solid #e0e0e0',
                    backgroundColor: '#fafafa',
                    textAlign: 'center',
                }, onClick: (e) => {
                    e.stopPropagation();
                    onToggleExpand?.(agent.id);
                }, children: _jsxs("span", { style: { fontSize: 11, color: '#666' }, children: [agent.isExpanded ? '▲ Hide Logs' : '▼ Show Logs', " (", agent.logs.length, ")"] }) }), agent.isExpanded && (_jsx("div", { style: {
                    maxHeight: 200,
                    overflow: 'auto',
                    borderTop: '1px solid #e0e0e0',
                    backgroundColor: '#f5f5f5',
                }, children: agent.logs.length === 0 ? (_jsx("div", { style: { padding: 12, textAlign: 'center', color: '#999', fontSize: 11 }, children: "No logs" })) : (agent.logs.slice(-10).map((log) => (_jsxs("div", { style: {
                        padding: '6px 10px',
                        borderBottom: '1px solid #e0e0e0',
                        fontSize: 10,
                    }, children: [_jsx("div", { style: { color: '#999', marginBottom: 2 }, children: formatBoardTimestamp(log.timestamp) }), _jsx("div", { style: {
                                color: log.type === 'error' ? '#F44336' : '#333',
                            }, children: log.message })] }, log.id)))) }))] }));
};
/**
 * 黑板区域组件
 */
export const BlackboardArea = ({ entries, onEntryClick }) => {
    const groupedEntries = useMemo(() => {
        const groups = {
            state: [],
            proposal: [],
            decision: [],
            artifact: [],
        };
        entries.forEach((entry) => {
            groups[entry.type].push(entry);
        });
        return groups;
    }, [entries]);
    return (_jsxs("div", { style: {
            flex: 1,
            backgroundColor: '#1a1a2e',
            borderRadius: 12,
            padding: 16,
            overflow: 'auto',
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    marginBottom: 16,
                    paddingBottom: 12,
                    borderBottom: '1px solid #333',
                }, children: [_jsx("span", { style: { fontSize: 20 }, children: "\uD83D\uDCCB" }), _jsx("span", { style: { fontSize: 16, fontWeight: 600, color: '#fff' }, children: "Blackboard" }), _jsxs("span", { style: { fontSize: 12, color: '#888' }, children: ["(", entries.length, " entries)"] })] }), _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 12 }, children: Object.entries(groupedEntries).map(([type, items]) => {
                    const config = BLACKBOARD_TYPE_CONFIG[type];
                    return (_jsxs("div", { style: {
                            backgroundColor: '#252540',
                            borderRadius: 8,
                            padding: 12,
                            minHeight: 100,
                        }, children: [_jsxs("div", { style: {
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 6,
                                    marginBottom: 10,
                                }, children: [_jsx("span", { style: { fontSize: 14 }, children: config.icon }), _jsx("span", { style: { fontSize: 12, fontWeight: 500, color: config.color }, children: config.label }), _jsxs("span", { style: { fontSize: 10, color: '#666' }, children: ["(", items.length, ")"] })] }), items.length === 0 ? (_jsxs("div", { style: { color: '#555', fontSize: 11, textAlign: 'center', padding: 10 }, children: ["No ", config.label.toLowerCase(), "s"] })) : (items.slice(-5).map((entry) => (_jsxs("div", { style: {
                                    padding: '8px 10px',
                                    marginBottom: 6,
                                    backgroundColor: '#1a1a2e',
                                    borderRadius: 6,
                                    borderLeft: `3px solid ${config.color}`,
                                    cursor: 'pointer',
                                }, onClick: () => onEntryClick?.(entry.id), children: [_jsx("div", { style: {
                                            fontSize: 11,
                                            fontWeight: 500,
                                            color: '#ddd',
                                            marginBottom: 4,
                                        }, children: entry.key }), _jsx("div", { style: {
                                            fontSize: 10,
                                            color: '#888',
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                            whiteSpace: 'nowrap',
                                        }, children: typeof entry.value === 'string'
                                            ? entry.value
                                            : JSON.stringify(entry.value) }), _jsxs("div", { style: { fontSize: 9, color: '#555', marginTop: 4 }, children: ["by ", entry.author, " \u2022 v", entry.version] })] }, entry.id))))] }, type));
                }) })] }));
};
/**
 * 投票进度环形图
 */
const VotingRing = ({ progress, }) => {
    const size = 80;
    const strokeWidth = 8;
    const radius = (size - strokeWidth) / 2;
    const circumference = 2 * Math.PI * radius;
    const approvedOffset = circumference * (1 - progress.approved / 100);
    const rejectedOffset = circumference * (1 - (progress.approved + progress.rejected) / 100);
    return (_jsxs("svg", { width: size, height: size, style: { transform: 'rotate(-90deg)' }, children: [_jsx("circle", { cx: size / 2, cy: size / 2, r: radius, fill: "none", stroke: "#e0e0e0", strokeWidth: strokeWidth }), _jsx("circle", { cx: size / 2, cy: size / 2, r: radius, fill: "none", stroke: "#F44336", strokeWidth: strokeWidth, strokeDasharray: circumference, strokeDashoffset: rejectedOffset, style: { transition: 'stroke-dashoffset 0.3s' } }), _jsx("circle", { cx: size / 2, cy: size / 2, r: radius, fill: "none", stroke: "#4CAF50", strokeWidth: strokeWidth, strokeDasharray: circumference, strokeDashoffset: approvedOffset, style: { transition: 'stroke-dashoffset 0.3s' } })] }));
};
/**
 * 投票进度组件
 */
export const VotingProgress = ({ vote, onVoteAction }) => {
    const progress = calculateVoteProgress(vote);
    const approvedCount = vote.votes.filter((v) => v.status === 'approved').length;
    const rejectedCount = vote.votes.filter((v) => v.status === 'rejected').length;
    return (_jsxs("div", { style: {
            backgroundColor: '#fff',
            border: '1px solid #e0e0e0',
            borderRadius: 12,
            padding: 16,
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: 12,
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: { fontSize: 18 }, children: "\uD83D\uDDF3\uFE0F" }), _jsx("span", { style: { fontSize: 14, fontWeight: 600, color: '#333' }, children: "BFT Consensus" })] }), _jsx("span", { style: {
                            fontSize: 10,
                            padding: '3px 8px',
                            borderRadius: 10,
                            backgroundColor: vote.status === 'active' ? '#e3f2fd' : '#f5f5f5',
                            color: vote.status === 'active' ? '#1976D2' : '#666',
                        }, children: vote.status.toUpperCase() })] }), _jsxs("div", { style: { marginBottom: 16 }, children: [_jsx("div", { style: { fontSize: 13, fontWeight: 500, color: '#333', marginBottom: 4 }, children: vote.proposal }), _jsx("div", { style: { fontSize: 11, color: '#666' }, children: vote.description })] }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 20 }, children: [_jsxs("div", { style: { position: 'relative' }, children: [_jsx(VotingRing, { progress: progress }), _jsxs("div", { style: {
                                    position: 'absolute',
                                    top: '50%',
                                    left: '50%',
                                    transform: 'translate(-50%, -50%)',
                                    textAlign: 'center',
                                }, children: [_jsxs("div", { style: { fontSize: 16, fontWeight: 600, color: '#333' }, children: [approvedCount, "/", vote.requiredApprovals] }), _jsx("div", { style: { fontSize: 9, color: '#999' }, children: "Required" })] })] }), _jsxs("div", { style: { flex: 1 }, children: [_jsxs("div", { style: { display: 'flex', gap: 16, marginBottom: 12 }, children: [_jsxs("div", { children: [_jsx("div", { style: { fontSize: 18, fontWeight: 600, color: '#4CAF50' }, children: approvedCount }), _jsx("div", { style: { fontSize: 10, color: '#666' }, children: "Approved" })] }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: 18, fontWeight: 600, color: '#F44336' }, children: rejectedCount }), _jsx("div", { style: { fontSize: 10, color: '#666' }, children: "Rejected" })] }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: 18, fontWeight: 600, color: '#9E9E9E' }, children: vote.votes.filter((v) => v.status === 'pending').length }), _jsx("div", { style: { fontSize: 10, color: '#666' }, children: "Pending" })] })] }), _jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: 4 }, children: vote.votes.map((v) => {
                                    const config = VOTE_STATUS_CONFIG[v.status];
                                    return (_jsxs("span", { style: {
                                            fontSize: 10,
                                            padding: '2px 6px',
                                            borderRadius: 4,
                                            backgroundColor: `${config.color}20`,
                                            color: config.color,
                                        }, title: v.reason || v.agentName, children: [config.icon, " ", v.agentName] }, v.agentId));
                                }) })] })] }), vote.status === 'active' && onVoteAction && (_jsxs("div", { style: {
                    display: 'flex',
                    gap: 8,
                    marginTop: 16,
                    paddingTop: 12,
                    borderTop: '1px solid #e0e0e0',
                }, children: [_jsx("button", { onClick: () => onVoteAction(vote.id, 'approve'), style: {
                            flex: 1,
                            padding: '8px 16px',
                            fontSize: 12,
                            fontWeight: 500,
                            border: 'none',
                            borderRadius: 6,
                            backgroundColor: '#4CAF50',
                            color: '#fff',
                            cursor: 'pointer',
                        }, children: "\u2705 Approve" }), _jsx("button", { onClick: () => onVoteAction(vote.id, 'reject'), style: {
                            flex: 1,
                            padding: '8px 16px',
                            fontSize: 12,
                            fontWeight: 500,
                            border: 'none',
                            borderRadius: 6,
                            backgroundColor: '#F44336',
                            color: '#fff',
                            cursor: 'pointer',
                        }, children: "\u274C Reject" })] }))] }));
};
/**
 * 多智能体协作看板
 */
export const AgentBoard = ({ agents, blackboard, currentVote, onAgentSelect, onAgentToggleExpand, onBlackboardEntryClick, onVoteAction, className, style, }) => {
    const [selectedAgentId, setSelectedAgentId] = useState(null);
    const handleAgentSelect = (agentId) => {
        setSelectedAgentId(agentId);
        onAgentSelect?.(agentId);
    };
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: '#f5f5f5',
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: 16,
                    backgroundColor: '#fff',
                    borderBottom: '1px solid #e0e0e0',
                }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            gap: 8,
                            marginBottom: 12,
                        }, children: [_jsx("span", { style: { fontSize: 18 }, children: "\uD83E\uDD16" }), _jsx("span", { style: { fontSize: 14, fontWeight: 600, color: '#333' }, children: "Active Agents" }), _jsxs("span", { style: { fontSize: 12, color: '#666' }, children: ["(", agents.length, ")"] })] }), _jsx("div", { style: {
                            display: 'flex',
                            gap: 12,
                            overflowX: 'auto',
                            paddingBottom: 8,
                        }, children: agents.length === 0 ? (_jsx("div", { style: { color: '#999', fontSize: 13, padding: 20 }, children: "No active agents" })) : (agents.map((agent) => (_jsx(AgentCard, { agent: agent, isSelected: selectedAgentId === agent.id, onSelect: handleAgentSelect, onToggleExpand: onAgentToggleExpand }, agent.id)))) })] }), _jsx("div", { style: { flex: 1, padding: 16, overflow: 'hidden', display: 'flex' }, children: _jsx(BlackboardArea, { entries: blackboard, onEntryClick: onBlackboardEntryClick }) }), currentVote && (_jsx("div", { style: { padding: 16, paddingTop: 0 }, children: _jsx(VotingProgress, { vote: currentVote, onVoteAction: onVoteAction }) })), _jsx("style", { children: `
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }
      ` })] }));
};
export default AgentBoard;
//# sourceMappingURL=AgentBoard.js.map