import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * MobileNav - 移动端底部导航
 * 基于 codeflow_template 风格
 */
import { useState, useCallback } from 'react';
import { ViewMode } from './types';
import { colors, spacing, fontSize, fontWeight, shadows, transitions, zIndex, } from '../shared/tokens';
// 简单的图标组件（使用 SVG）
const HomeIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("path", { d: "M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" }), _jsx("polyline", { points: "9 22 9 12 15 12 15 22" })] }));
const FolderIcon = () => (_jsx("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("path", { d: "M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" }) }));
const MessageIcon = () => (_jsx("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("path", { d: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" }) }));
const UsersIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("path", { d: "M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" }), _jsx("circle", { cx: "9", cy: "7", r: "4" }), _jsx("path", { d: "M23 21v-2a4 4 0 0 0-3-3.87" }), _jsx("path", { d: "M16 3.13a4 4 0 0 1 0 7.75" })] }));
const SettingsIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "3" }), _jsx("path", { d: "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" })] }));
const getMobileNavItems = () => [
    { id: ViewMode.HOME, label: 'Home', icon: _jsx(HomeIcon, {}) },
    { id: ViewMode.PROJECTS, label: 'Projects', icon: _jsx(FolderIcon, {}) },
    { id: ViewMode.SESSIONS, label: 'Sessions', icon: _jsx(MessageIcon, {}) },
    { id: ViewMode.AGENTS, label: 'Agents', icon: _jsx(UsersIcon, {}) },
    { id: ViewMode.SETTINGS, label: 'Settings', icon: _jsx(SettingsIcon, {}) },
];
export const MobileNav = ({ activeMode, onNavigate, className, style, }) => {
    const [pressedItem, setPressedItem] = useState(null);
    const navItems = getMobileNavItems();
    const handleKeyDown = useCallback((event, mode) => {
        if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            onNavigate(mode);
        }
    }, [onNavigate]);
    const getNavItemStyle = (item) => {
        const isActive = activeMode === item.id;
        const isPressed = pressedItem === item.id;
        return {
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            gap: spacing[1],
            flex: 1,
            padding: `${spacing[2]}px ${spacing[1]}px`,
            backgroundColor: 'transparent',
            border: 'none',
            cursor: 'pointer',
            transition: transitions.fast,
            transform: isPressed ? 'scale(0.95)' : 'scale(1)',
            color: isActive ? colors.primary[600] : colors.slate[400],
            position: 'relative',
        };
    };
    const getIconStyle = (item) => {
        const isActive = activeMode === item.id;
        return {
            color: isActive ? colors.primary[600] : colors.slate[400],
            transition: transitions.fast,
        };
    };
    const getLabelStyle = (item) => {
        const isActive = activeMode === item.id;
        return {
            fontSize: fontSize.xs,
            fontWeight: isActive ? fontWeight.semibold : fontWeight.medium,
            color: isActive ? colors.primary[600] : colors.slate[400],
            transition: transitions.fast,
        };
    };
    return (_jsx("nav", { className: className, style: {
            display: 'flex',
            position: 'fixed',
            bottom: 0,
            left: 0,
            right: 0,
            backgroundColor: 'rgba(255, 255, 255, 0.95)',
            backdropFilter: 'blur(12px)',
            borderTop: `1px solid ${colors.slate[200]}`,
            zIndex: zIndex.fixed,
            // safe-area-bottom support
            paddingBottom: 'env(safe-area-inset-bottom, 0px)',
            boxShadow: shadows.lg,
            ...style,
        }, role: "navigation", "aria-label": "Mobile navigation", children: navItems.map((item) => (_jsxs("button", { onClick: () => onNavigate(item.id), onKeyDown: (e) => handleKeyDown(e, item.id), onMouseDown: () => setPressedItem(item.id), onMouseUp: () => setPressedItem(null), onMouseLeave: () => setPressedItem(null), onTouchStart: () => setPressedItem(item.id), onTouchEnd: () => setPressedItem(null), style: getNavItemStyle(item), "aria-label": item.label, "aria-current": activeMode === item.id ? 'page' : undefined, children: [activeMode === item.id && (_jsx("span", { style: {
                        position: 'absolute',
                        top: 0,
                        left: '50%',
                        transform: 'translateX(-50%)',
                        width: 24,
                        height: 3,
                        backgroundColor: colors.primary[500],
                        borderRadius: '0 0 4px 4px',
                    } })), _jsx("span", { style: getIconStyle(item), children: item.icon }), _jsx("span", { style: getLabelStyle(item), children: item.label })] }, item.id))) }));
};
export default MobileNav;
//# sourceMappingURL=MobileNav.js.map