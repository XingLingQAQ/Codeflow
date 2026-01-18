import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
/**
 * 辩论式校验界面组件
 * Generator与Critic对抗模式可视化
 */
import React, { useState, useMemo } from 'react';
import { DEBATE_ROLE_CONFIG, ROUND_STATUS_CONFIG, CONFLICT_SEVERITY_CONFIG, formatDebateTime, countConflicts, } from './types';
/**
 * 高亮冲突文本
 */
const HighlightedText = ({ text, conflicts, onConflictClick }) => {
    if (!conflicts || conflicts.length === 0) {
        return _jsx("span", { children: text });
    }
    const sortedConflicts = [...conflicts].sort((a, b) => a.position.start - b.position.start);
    const parts = [];
    let lastEnd = 0;
    sortedConflicts.forEach((conflict, index) => {
        if (conflict.position.start > lastEnd) {
            parts.push(_jsx("span", { children: text.slice(lastEnd, conflict.position.start) }, `text-${index}`));
        }
        const config = CONFLICT_SEVERITY_CONFIG[conflict.severity];
        parts.push(_jsxs("span", { style: {
                backgroundColor: `${config.color}30`,
                borderBottom: `2px solid ${config.color}`,
                cursor: 'pointer',
                position: 'relative',
            }, onClick: () => onConflictClick?.(conflict.id), title: conflict.description, children: [text.slice(conflict.position.start, conflict.position.end), _jsx("span", { style: {
                        position: 'absolute',
                        top: -8,
                        right: -4,
                        fontSize: 10,
                    }, children: config.icon })] }, `conflict-${conflict.id}`));
        lastEnd = conflict.position.end;
    });
    if (lastEnd < text.length) {
        parts.push(_jsx("span", { children: text.slice(lastEnd) }, "text-end"));
    }
    return _jsx(_Fragment, { children: parts });
};
/**
 * 辩论消息气泡
 */
export const DebateBubble = ({ message, onConflictClick }) => {
    const config = DEBATE_ROLE_CONFIG[message.role];
    const isGenerator = message.role === 'generator';
    return (_jsxs("div", { style: {
            display: 'flex',
            flexDirection: isGenerator ? 'row' : 'row-reverse',
            gap: 12,
            marginBottom: 16,
        }, children: [_jsx("div", { style: {
                    width: 40,
                    height: 40,
                    borderRadius: '50%',
                    backgroundColor: `${config.color}20`,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    fontSize: 20,
                    flexShrink: 0,
                }, children: config.icon }), _jsxs("div", { style: {
                    flex: 1,
                    maxWidth: '80%',
                }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            gap: 8,
                            marginBottom: 6,
                            flexDirection: isGenerator ? 'row' : 'row-reverse',
                        }, children: [_jsx("span", { style: { fontSize: 12, fontWeight: 600, color: config.color }, children: config.label }), _jsx("span", { style: { fontSize: 10, color: '#999' }, children: formatDebateTime(message.timestamp) }), message.conflicts && message.conflicts.length > 0 && (_jsxs("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 10,
                                    backgroundColor: '#ffebee',
                                    color: '#F44336',
                                }, children: [message.conflicts.length, " conflicts"] }))] }), _jsx("div", { style: {
                            padding: '12px 16px',
                            backgroundColor: isGenerator ? '#e3f2fd' : '#fff3e0',
                            borderRadius: isGenerator ? '4px 16px 16px 16px' : '16px 4px 16px 16px',
                            border: `1px solid ${isGenerator ? '#bbdefb' : '#ffe0b2'}`,
                            fontSize: 13,
                            lineHeight: 1.6,
                            color: '#333',
                        }, children: _jsx(HighlightedText, { text: message.content, conflicts: message.conflicts, onConflictClick: onConflictClick }) }), message.refinedFrom && (_jsxs("div", { style: {
                            marginTop: 6,
                            fontSize: 10,
                            color: '#9C27B0',
                            display: 'flex',
                            alignItems: 'center',
                            gap: 4,
                        }, children: [_jsx("span", { children: "\uD83D\uDD04" }), _jsx("span", { children: "Refined version" })] }))] })] }));
};
/**
 * 辩论时间轴
 */
export const DebateTimeline = ({ rounds, currentRoundIndex, onRoundSelect, }) => {
    return (_jsx("div", { style: {
            display: 'flex',
            alignItems: 'center',
            gap: 4,
            padding: '12px 16px',
            backgroundColor: '#fafafa',
            borderRadius: 8,
            overflowX: 'auto',
        }, children: rounds.map((round, index) => {
            const statusConfig = ROUND_STATUS_CONFIG[round.status];
            const isActive = index === currentRoundIndex;
            const isPast = index < currentRoundIndex;
            return (_jsxs(React.Fragment, { children: [_jsxs("div", { style: {
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            cursor: 'pointer',
                        }, onClick: () => onRoundSelect?.(index), children: [_jsx("div", { style: {
                                    width: 32,
                                    height: 32,
                                    borderRadius: '50%',
                                    backgroundColor: isActive ? statusConfig.color : isPast ? '#4CAF50' : '#e0e0e0',
                                    color: '#fff',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    fontSize: 12,
                                    fontWeight: 600,
                                    border: isActive ? `3px solid ${statusConfig.color}40` : 'none',
                                }, children: isPast ? '✓' : index + 1 }), _jsxs("div", { style: {
                                    marginTop: 4,
                                    fontSize: 10,
                                    color: isActive ? statusConfig.color : '#666',
                                    fontWeight: isActive ? 600 : 400,
                                }, children: ["Round ", index + 1] })] }), index < rounds.length - 1 && (_jsx("div", { style: {
                            width: 40,
                            height: 2,
                            backgroundColor: isPast ? '#4CAF50' : '#e0e0e0',
                            marginBottom: 20,
                        } }))] }, round.id));
        }) }));
};
/**
 * 冲突详情面板
 */
export const ConflictPanel = ({ conflict, onResolve, onClose }) => {
    const [resolution, setResolution] = useState(conflict.resolution || '');
    const severityConfig = CONFLICT_SEVERITY_CONFIG[conflict.severity];
    return (_jsxs("div", { style: {
            position: 'fixed',
            top: 0,
            right: 0,
            width: 400,
            height: '100%',
            backgroundColor: '#fff',
            boxShadow: '-4px 0 20px rgba(0,0,0,0.1)',
            zIndex: 1000,
            display: 'flex',
            flexDirection: 'column',
        }, children: [_jsxs("div", { style: {
                    padding: '16px 20px',
                    borderBottom: '1px solid #e0e0e0',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: { fontSize: 18 }, children: severityConfig.icon }), _jsx("span", { style: { fontSize: 14, fontWeight: 600, color: '#333' }, children: "Conflict Details" }), _jsx("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 8px',
                                    borderRadius: 10,
                                    backgroundColor: `${severityConfig.color}20`,
                                    color: severityConfig.color,
                                }, children: severityConfig.label })] }), _jsx("button", { onClick: onClose, style: {
                            background: 'none',
                            border: 'none',
                            fontSize: 20,
                            color: '#666',
                            cursor: 'pointer',
                        }, children: "\u00D7" })] }), _jsxs("div", { style: { flex: 1, overflow: 'auto', padding: 20 }, children: [_jsxs("div", { style: { marginBottom: 20 }, children: [_jsx("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: "Description" }), _jsx("div", { style: { fontSize: 13, color: '#333', lineHeight: 1.5 }, children: conflict.description })] }), _jsxs("div", { style: { marginBottom: 20 }, children: [_jsxs("div", { style: {
                                    fontSize: 12,
                                    fontWeight: 500,
                                    color: DEBATE_ROLE_CONFIG.generator.color,
                                    marginBottom: 6,
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 4,
                                }, children: [DEBATE_ROLE_CONFIG.generator.icon, " Generator's View"] }), _jsx("div", { style: {
                                    padding: 12,
                                    backgroundColor: '#e3f2fd',
                                    borderRadius: 8,
                                    fontSize: 13,
                                    color: '#333',
                                    lineHeight: 1.5,
                                }, children: conflict.generatorText })] }), _jsxs("div", { style: { marginBottom: 20 }, children: [_jsxs("div", { style: {
                                    fontSize: 12,
                                    fontWeight: 500,
                                    color: DEBATE_ROLE_CONFIG.critic.color,
                                    marginBottom: 6,
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 4,
                                }, children: [DEBATE_ROLE_CONFIG.critic.icon, " Critic's View"] }), _jsx("div", { style: {
                                    padding: 12,
                                    backgroundColor: '#fff3e0',
                                    borderRadius: 8,
                                    fontSize: 13,
                                    color: '#333',
                                    lineHeight: 1.5,
                                }, children: conflict.criticText })] }), _jsxs("div", { children: [_jsx("div", { style: { fontSize: 12, fontWeight: 500, color: '#666', marginBottom: 6 }, children: "Resolution" }), conflict.resolved ? (_jsxs("div", { style: {
                                    padding: 12,
                                    backgroundColor: '#e8f5e9',
                                    borderRadius: 8,
                                    fontSize: 13,
                                    color: '#2e7d32',
                                    lineHeight: 1.5,
                                }, children: ["\u2705 ", conflict.resolution] })) : (_jsxs(_Fragment, { children: [_jsx("textarea", { value: resolution, onChange: (e) => setResolution(e.target.value), placeholder: "Enter resolution...", style: {
                                            width: '100%',
                                            minHeight: 100,
                                            padding: 12,
                                            border: '1px solid #e0e0e0',
                                            borderRadius: 8,
                                            fontSize: 13,
                                            resize: 'vertical',
                                        } }), _jsx("button", { onClick: () => onResolve?.(conflict.id, resolution), disabled: !resolution.trim(), style: {
                                            marginTop: 12,
                                            width: '100%',
                                            padding: '10px 16px',
                                            fontSize: 13,
                                            fontWeight: 500,
                                            border: 'none',
                                            borderRadius: 8,
                                            backgroundColor: resolution.trim() ? '#4CAF50' : '#e0e0e0',
                                            color: '#fff',
                                            cursor: resolution.trim() ? 'pointer' : 'not-allowed',
                                        }, children: "Mark as Resolved" })] }))] })] })] }));
};
/**
 * 辩论式校验界面
 */
export const DebateView = ({ session, onSelectSolution, onExportReport, onConflictResolve, className, style, }) => {
    const [selectedRoundIndex, setSelectedRoundIndex] = useState(session.currentRoundIndex);
    const [selectedConflict, setSelectedConflict] = useState(null);
    const currentRound = session.rounds[selectedRoundIndex];
    const conflictStats = useMemo(() => countConflicts(session), [session]);
    const handleConflictClick = (conflictId) => {
        const conflict = session.rounds
            .flatMap((r) => [
            ...(r.generatorMessage?.conflicts || []),
            ...(r.criticMessage?.conflicts || []),
        ])
            .find((c) => c.id === conflictId);
        if (conflict) {
            setSelectedConflict(conflict);
        }
    };
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            backgroundColor: '#f5f5f5',
            ...style,
        }, children: [_jsxs("div", { style: {
                    padding: '16px 20px',
                    backgroundColor: '#fff',
                    borderBottom: '1px solid #e0e0e0',
                }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                            marginBottom: 12,
                        }, children: [_jsxs("div", { children: [_jsx("div", { style: { fontSize: 16, fontWeight: 600, color: '#333' }, children: session.topic }), _jsx("div", { style: { fontSize: 12, color: '#666', marginTop: 4 }, children: session.description })] }), _jsx("div", { style: { display: 'flex', gap: 8 }, children: _jsx("button", { onClick: onExportReport, style: {
                                        padding: '8px 16px',
                                        fontSize: 12,
                                        border: '1px solid #e0e0e0',
                                        borderRadius: 6,
                                        backgroundColor: '#fff',
                                        color: '#333',
                                        cursor: 'pointer',
                                    }, children: "\uD83D\uDCC4 Export Report" }) })] }), _jsxs("div", { style: { display: 'flex', gap: 20 }, children: [_jsxs("div", { children: [_jsx("span", { style: { fontSize: 11, color: '#666' }, children: "Rounds: " }), _jsx("span", { style: { fontSize: 13, fontWeight: 600, color: '#333' }, children: session.rounds.length })] }), _jsxs("div", { children: [_jsx("span", { style: { fontSize: 11, color: '#666' }, children: "Conflicts: " }), _jsx("span", { style: { fontSize: 13, fontWeight: 600, color: '#F44336' }, children: conflictStats.total }), _jsxs("span", { style: { fontSize: 11, color: '#4CAF50', marginLeft: 4 }, children: ["(", conflictStats.resolved, " resolved)"] })] }), _jsxs("div", { children: [_jsx("span", { style: { fontSize: 11, color: '#666' }, children: "Status: " }), _jsx("span", { style: {
                                            fontSize: 11,
                                            padding: '2px 8px',
                                            borderRadius: 10,
                                            backgroundColor: session.status === 'active'
                                                ? '#e3f2fd'
                                                : session.status === 'completed'
                                                    ? '#e8f5e9'
                                                    : '#f5f5f5',
                                            color: session.status === 'active'
                                                ? '#1976D2'
                                                : session.status === 'completed'
                                                    ? '#2e7d32'
                                                    : '#666',
                                        }, children: session.status.toUpperCase() })] })] })] }), _jsx("div", { style: { padding: '12px 20px', backgroundColor: '#fff' }, children: _jsx(DebateTimeline, { rounds: session.rounds, currentRoundIndex: selectedRoundIndex, onRoundSelect: setSelectedRoundIndex }) }), _jsxs("div", { style: { flex: 1, display: 'flex', overflow: 'hidden' }, children: [_jsxs("div", { style: {
                            flex: 1,
                            borderRight: '1px solid #e0e0e0',
                            display: 'flex',
                            flexDirection: 'column',
                        }, children: [_jsxs("div", { style: {
                                    padding: '12px 20px',
                                    backgroundColor: `${DEBATE_ROLE_CONFIG.generator.color}10`,
                                    borderBottom: '1px solid #e0e0e0',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 8,
                                }, children: [_jsx("span", { style: { fontSize: 18 }, children: DEBATE_ROLE_CONFIG.generator.icon }), _jsx("span", { style: {
                                            fontSize: 14,
                                            fontWeight: 600,
                                            color: DEBATE_ROLE_CONFIG.generator.color,
                                        }, children: "Generator" })] }), _jsxs("div", { style: { flex: 1, overflow: 'auto', padding: 20 }, children: [currentRound?.generatorMessage ? (_jsxs(_Fragment, { children: [_jsx(DebateBubble, { message: currentRound.generatorMessage, onConflictClick: handleConflictClick }), currentRound.refinedMessage && (_jsx(DebateBubble, { message: currentRound.refinedMessage, onConflictClick: handleConflictClick }))] })) : (_jsx("div", { style: { textAlign: 'center', color: '#999', padding: 40 }, children: "Waiting for generation..." })), currentRound?.status === 'completed' && currentRound.generatorMessage && (_jsx("button", { onClick: () => onSelectSolution?.(currentRound.id, currentRound.generatorMessage.id), style: {
                                            width: '100%',
                                            padding: '10px 16px',
                                            marginTop: 12,
                                            fontSize: 12,
                                            fontWeight: 500,
                                            border: `2px solid ${DEBATE_ROLE_CONFIG.generator.color}`,
                                            borderRadius: 8,
                                            backgroundColor: session.selectedSolution === currentRound.generatorMessage.id
                                                ? DEBATE_ROLE_CONFIG.generator.color
                                                : '#fff',
                                            color: session.selectedSolution === currentRound.generatorMessage.id
                                                ? '#fff'
                                                : DEBATE_ROLE_CONFIG.generator.color,
                                            cursor: 'pointer',
                                        }, children: session.selectedSolution === currentRound.generatorMessage.id
                                            ? '✓ Selected'
                                            : 'Select This Solution' }))] })] }), _jsxs("div", { style: { flex: 1, display: 'flex', flexDirection: 'column' }, children: [_jsxs("div", { style: {
                                    padding: '12px 20px',
                                    backgroundColor: `${DEBATE_ROLE_CONFIG.critic.color}10`,
                                    borderBottom: '1px solid #e0e0e0',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 8,
                                }, children: [_jsx("span", { style: { fontSize: 18 }, children: DEBATE_ROLE_CONFIG.critic.icon }), _jsx("span", { style: {
                                            fontSize: 14,
                                            fontWeight: 600,
                                            color: DEBATE_ROLE_CONFIG.critic.color,
                                        }, children: "Critic" })] }), _jsx("div", { style: { flex: 1, overflow: 'auto', padding: 20 }, children: currentRound?.criticMessage ? (_jsx(DebateBubble, { message: currentRound.criticMessage, onConflictClick: handleConflictClick })) : (_jsx("div", { style: { textAlign: 'center', color: '#999', padding: 40 }, children: "Waiting for critique..." })) })] })] }), selectedConflict && (_jsx(ConflictPanel, { conflict: selectedConflict, onResolve: onConflictResolve, onClose: () => setSelectedConflict(null) }))] }));
};
export default DebateView;
//# sourceMappingURL=DebateView.js.map