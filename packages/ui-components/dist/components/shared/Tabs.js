import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Tabs - 统一风格标签页组件
 */
import { useState } from 'react';
import { colors, borderRadius, fontWeight, spacing, transitions } from './tokens';
const sizeConfig = {
    sm: { padding: '6px 12px', fontSize: 11, gap: 4 },
    md: { padding: '8px 16px', fontSize: 12, gap: 6 },
    lg: { padding: '10px 20px', fontSize: 13, gap: 8 },
};
export const Tabs = ({ tabs, activeTab, onChange, variant = 'default', size = 'md', fullWidth = false, className, style, }) => {
    const [hoveredTab, setHoveredTab] = useState(null);
    const sizeStyles = sizeConfig[size];
    const getTabStyles = (tab) => {
        const isActive = tab.id === activeTab;
        const isHovered = tab.id === hoveredTab;
        const base = {
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: sizeStyles.gap,
            padding: sizeStyles.padding,
            fontSize: sizeStyles.fontSize,
            fontWeight: isActive ? fontWeight.bold : fontWeight.medium,
            cursor: tab.disabled ? 'not-allowed' : 'pointer',
            opacity: tab.disabled ? 0.5 : 1,
            transition: transitions.fast,
            flex: fullWidth ? 1 : 'none',
            border: 'none',
            outline: 'none',
            whiteSpace: 'nowrap',
        };
        switch (variant) {
            case 'pills':
                return {
                    ...base,
                    borderRadius: borderRadius.full,
                    backgroundColor: isActive
                        ? colors.primary[500]
                        : isHovered
                            ? colors.slate[100]
                            : 'transparent',
                    color: isActive ? '#fff' : colors.slate[600],
                };
            case 'underline':
                return {
                    ...base,
                    backgroundColor: 'transparent',
                    color: isActive ? colors.primary[600] : isHovered ? colors.slate[700] : colors.slate[500],
                    borderBottom: `2px solid ${isActive ? colors.primary[500] : 'transparent'}`,
                    borderRadius: 0,
                    marginBottom: -1,
                };
            default:
                return {
                    ...base,
                    borderRadius: borderRadius.lg,
                    backgroundColor: isActive
                        ? colors.primary[50]
                        : isHovered
                            ? colors.slate[50]
                            : 'transparent',
                    color: isActive ? colors.primary[700] : colors.slate[600],
                    border: `1px solid ${isActive ? colors.primary[200] : 'transparent'}`,
                };
        }
    };
    const getContainerStyles = () => {
        const base = {
            display: 'flex',
            alignItems: 'center',
            gap: variant === 'underline' ? 0 : spacing[1],
        };
        switch (variant) {
            case 'underline':
                return {
                    ...base,
                    borderBottom: `1px solid ${colors.slate[200]}`,
                };
            default:
                return {
                    ...base,
                    padding: spacing[1],
                    backgroundColor: colors.slate[100],
                    borderRadius: borderRadius.xl,
                };
        }
    };
    return (_jsx("div", { className: className, style: { ...getContainerStyles(), ...style }, role: "tablist", children: tabs.map((tab) => (_jsxs("button", { role: "tab", "aria-selected": tab.id === activeTab, disabled: tab.disabled, onClick: () => !tab.disabled && onChange?.(tab.id), onMouseEnter: () => setHoveredTab(tab.id), onMouseLeave: () => setHoveredTab(null), style: getTabStyles(tab), children: [tab.icon, _jsx("span", { children: tab.label }), tab.badge !== undefined && (_jsx("span", { style: {
                        padding: '1px 6px',
                        fontSize: 9,
                        fontWeight: fontWeight.bold,
                        borderRadius: borderRadius.full,
                        backgroundColor: tab.id === activeTab ? 'rgba(255,255,255,0.3)' : colors.slate[200],
                        color: tab.id === activeTab ? '#fff' : colors.slate[600],
                    }, children: tab.badge }))] }, tab.id))) }));
};
export const TabPanel = ({ children, tabId, activeTab, style }) => {
    if (tabId !== activeTab)
        return null;
    return (_jsx("div", { role: "tabpanel", style: { padding: spacing[4], ...style }, children: children }));
};
export default Tabs;
//# sourceMappingURL=Tabs.js.map