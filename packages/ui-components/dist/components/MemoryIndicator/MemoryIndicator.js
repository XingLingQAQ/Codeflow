import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * 记忆预警指示灯组件
 * 输入框边框变色 + 匹配预览
 */
import { useState, useMemo } from 'react';
import { LEVEL_COLORS, LEVEL_BORDER_COLORS, LEVEL_LABELS, } from './types';
/**
 * 指示灯组件
 */
export const MemoryIndicator = ({ level, size = 'medium', showLabel = false, animate = true, }) => {
    const sizes = {
        small: 8,
        medium: 12,
        large: 16,
    };
    const shouldAnimate = animate && (level === 'high' || level === 'critical');
    return (_jsxs("div", { style: {
            display: 'flex',
            alignItems: 'center',
            gap: 6,
        }, children: [_jsx("span", { style: {
                    display: 'inline-block',
                    width: sizes[size],
                    height: sizes[size],
                    borderRadius: '50%',
                    backgroundColor: LEVEL_COLORS[level],
                    boxShadow: shouldAnimate
                        ? `0 0 8px ${LEVEL_COLORS[level]}`
                        : undefined,
                    animation: shouldAnimate ? 'memoryPulse 1.5s infinite' : undefined,
                } }), showLabel && (_jsx("span", { style: {
                    fontSize: size === 'small' ? 10 : size === 'medium' ? 12 : 14,
                    color: LEVEL_COLORS[level] || '#666',
                }, children: LEVEL_LABELS[level] }))] }));
};
/**
 * 匹配预览弹窗
 */
export const MatchPreview = ({ matches, visible, position = 'top', onMatchClick, onClose, }) => {
    if (!visible || matches.length === 0)
        return null;
    return (_jsxs("div", { style: {
            position: 'absolute',
            [position]: '100%',
            left: 0,
            right: 0,
            marginTop: position === 'bottom' ? 8 : undefined,
            marginBottom: position === 'top' ? 8 : undefined,
            backgroundColor: '#fff',
            border: '1px solid #ddd',
            borderRadius: 8,
            boxShadow: '0 4px 16px rgba(0, 0, 0, 0.15)',
            zIndex: 1000,
            maxHeight: 200,
            overflowY: 'auto',
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 12px',
                    borderBottom: '1px solid #eee',
                    backgroundColor: '#fafafa',
                }, children: [_jsxs("span", { style: { fontSize: 12, fontWeight: 600, color: '#333' }, children: ["Related memories (", matches.length, ")"] }), onClose && (_jsx("button", { onClick: onClose, style: {
                            background: 'none',
                            border: 'none',
                            cursor: 'pointer',
                            fontSize: 14,
                            color: '#999',
                            padding: 0,
                        }, children: "\u2715" }))] }), matches.map((match) => (_jsx(MatchPreviewItemComponent, { match: match, onClick: () => onMatchClick?.(match.id) }, match.id)))] }));
};
/**
 * 匹配预览项组件
 */
const MatchPreviewItemComponent = ({ match, onClick }) => {
    const [isHovered, setIsHovered] = useState(false);
    const sourceColors = {
        vector: '#2196F3',
        graph: '#9C27B0',
        rules: '#FF9800',
    };
    return (_jsxs("div", { onClick: onClick, onMouseEnter: () => setIsHovered(true), onMouseLeave: () => setIsHovered(false), style: {
            padding: '10px 12px',
            cursor: onClick ? 'pointer' : 'default',
            backgroundColor: isHovered ? 'rgba(0, 0, 0, 0.02)' : 'transparent',
            borderBottom: '1px solid #f0f0f0',
            transition: 'background-color 0.15s',
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: 4,
                }, children: [_jsx("span", { style: { fontSize: 13, fontWeight: 500, color: '#333' }, children: match.title }), _jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8 }, children: [_jsx("span", { style: {
                                    fontSize: 10,
                                    padding: '2px 6px',
                                    borderRadius: 3,
                                    backgroundColor: sourceColors[match.source] || '#666',
                                    color: '#fff',
                                }, children: match.source }), _jsxs("span", { style: { fontSize: 11, color: '#999' }, children: [Math.round(match.score * 100), "%"] })] })] }), _jsx("div", { style: {
                    fontSize: 12,
                    color: '#666',
                    lineHeight: 1.4,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                }, children: match.preview })] }));
};
/**
 * 输入框包装器（带边框变色）
 */
export const MemoryInputWrapper = ({ children, matchLevel, matchCount = 0, showBadge = true, pulseOnHighMatch = true, className, style, onClick, }) => {
    const borderColor = LEVEL_BORDER_COLORS[matchLevel];
    const shouldPulse = pulseOnHighMatch && (matchLevel === 'high' || matchLevel === 'critical');
    const wrapperStyle = useMemo(() => ({
        position: 'relative',
        borderRadius: 8,
        border: `2px solid ${borderColor}`,
        boxShadow: shouldPulse
            ? `0 0 12px ${LEVEL_COLORS[matchLevel]}40`
            : undefined,
        animation: shouldPulse ? 'borderPulse 2s infinite' : undefined,
        transition: 'border-color 0.3s, box-shadow 0.3s',
        ...style,
    }), [borderColor, shouldPulse, matchLevel, style]);
    return (_jsxs("div", { className: className, style: wrapperStyle, onClick: onClick, children: [children, showBadge && matchCount > 0 && (_jsx("div", { style: {
                    position: 'absolute',
                    top: -8,
                    right: -8,
                    minWidth: 20,
                    height: 20,
                    borderRadius: 10,
                    backgroundColor: LEVEL_COLORS[matchLevel],
                    color: '#fff',
                    fontSize: 11,
                    fontWeight: 600,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    padding: '0 6px',
                    boxShadow: '0 2px 4px rgba(0, 0, 0, 0.2)',
                }, children: matchCount > 99 ? '99+' : matchCount })), _jsx("style", { children: `
          @keyframes memoryPulse {
            0%, 100% { opacity: 1; transform: scale(1); }
            50% { opacity: 0.7; transform: scale(1.1); }
          }
          @keyframes borderPulse {
            0%, 100% { box-shadow: 0 0 12px ${LEVEL_COLORS[matchLevel]}40; }
            50% { box-shadow: 0 0 20px ${LEVEL_COLORS[matchLevel]}60; }
          }
        ` })] }));
};
export default MemoryInputWrapper;
//# sourceMappingURL=MemoryIndicator.js.map