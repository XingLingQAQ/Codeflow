import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * ModelCard - 模型卡片组件
 * 显示模型名称、提供商、成本和能力
 */
import { useState } from 'react';
import { PROVIDER_ICONS, CAPABILITY_NAMES, CAPABILITY_COLORS, formatCost, formatContextWindow, } from './types';
export const ModelCard = ({ model, isSelected = false, onClick, showCost = true, showCapabilities = true, maxCapabilitiesToShow = 3, }) => {
    const [isHovered, setIsHovered] = useState(false);
    const displayCapabilities = model.capabilities.slice(0, maxCapabilitiesToShow);
    const remainingCount = model.capabilities.length - maxCapabilitiesToShow;
    return (_jsxs("div", { onClick: onClick, onMouseEnter: () => setIsHovered(true), onMouseLeave: () => setIsHovered(false), style: {
            display: 'flex',
            flexDirection: 'column',
            padding: '12px 14px',
            cursor: onClick ? 'pointer' : 'default',
            backgroundColor: isSelected
                ? 'rgba(33, 150, 243, 0.08)'
                : isHovered
                    ? 'rgba(0, 0, 0, 0.03)'
                    : 'transparent',
            borderLeft: isSelected ? '3px solid #2196F3' : '3px solid transparent',
            borderBottom: '1px solid #f0f0f0',
            transition: 'background-color 0.15s',
            opacity: model.deprecated ? 0.6 : 1,
        }, children: [_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                }, children: [_jsx("span", { style: { fontSize: 18 }, children: PROVIDER_ICONS[model.provider] }), _jsx("div", { style: { flex: 1, minWidth: 0 }, children: _jsxs("div", { style: {
                                fontSize: 14,
                                fontWeight: isSelected ? 600 : 500,
                                color: '#333',
                                whiteSpace: 'nowrap',
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                            }, children: [model.name, model.deprecated && (_jsx("span", { style: {
                                        marginLeft: 6,
                                        fontSize: 10,
                                        color: '#999',
                                        fontWeight: 400,
                                    }, children: "(deprecated)" }))] }) }), showCost && (_jsxs("div", { style: {
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'flex-end',
                            fontSize: 11,
                            color: '#666',
                        }, children: [_jsx("span", { style: { fontWeight: 500 }, children: formatCost(model.cost) }), _jsxs("span", { children: [formatContextWindow(model.contextWindow), " ctx"] })] })), isSelected && (_jsx("span", { style: { color: '#2196F3', fontSize: 16, marginLeft: 4 }, children: "\u2713" }))] }), model.description && (_jsx("div", { style: {
                    fontSize: 12,
                    color: '#888',
                    marginTop: 6,
                    marginLeft: 28,
                    lineHeight: 1.4,
                }, children: model.description })), showCapabilities && model.capabilities.length > 0 && (_jsxs("div", { style: {
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 4,
                    marginTop: 8,
                    marginLeft: 28,
                }, children: [displayCapabilities.map(cap => (_jsx("span", { style: {
                            fontSize: 10,
                            padding: '2px 6px',
                            borderRadius: 3,
                            backgroundColor: `${CAPABILITY_COLORS[cap]}15`,
                            color: CAPABILITY_COLORS[cap],
                            fontWeight: 500,
                        }, children: CAPABILITY_NAMES[cap] }, cap))), remainingCount > 0 && (_jsxs("span", { style: {
                            fontSize: 10,
                            padding: '2px 6px',
                            borderRadius: 3,
                            backgroundColor: '#f0f0f0',
                            color: '#666',
                        }, children: ["+", remainingCount] }))] }))] }));
};
export default ModelCard;
//# sourceMappingURL=ModelCard.js.map