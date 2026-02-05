import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Sidebar - 桌面端侧边栏导航
 * 基于 codeflow_template 风格
 */
import { useState, useCallback } from 'react';
import { ViewMode } from './types';
import { colors, spacing, borderRadius, fontSize, fontWeight, shadows, transitions, zIndex, } from '../shared/tokens';
// 简单的图标组件（使用 SVG）
const HomeIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("path", { d: "M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" }), _jsx("polyline", { points: "9 22 9 12 15 12 15 22" })] }));
const FolderIcon = () => (_jsx("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("path", { d: "M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" }) }));
const MessageIcon = () => (_jsx("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("path", { d: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" }) }));
const LayoutIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("rect", { x: "3", y: "3", width: "7", height: "7" }), _jsx("rect", { x: "14", y: "3", width: "7", height: "7" }), _jsx("rect", { x: "14", y: "14", width: "7", height: "7" }), _jsx("rect", { x: "3", y: "14", width: "7", height: "7" })] }));
const UsersIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("path", { d: "M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" }), _jsx("circle", { cx: "9", cy: "7", r: "4" }), _jsx("path", { d: "M23 21v-2a4 4 0 0 0-3-3.87" }), _jsx("path", { d: "M16 3.13a4 4 0 0 1 0 7.75" })] }));
const DatabaseIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("ellipse", { cx: "12", cy: "5", rx: "9", ry: "3" }), _jsx("path", { d: "M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" }), _jsx("path", { d: "M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" })] }));
const SettingsIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "3" }), _jsx("path", { d: "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" })] }));
const TerminalIcon = () => (_jsxs("svg", { width: "18", height: "18", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("polyline", { points: "4 17 10 11 4 5" }), _jsx("line", { x1: "12", y1: "19", x2: "20", y2: "19" })] }));
const getNavItems = () => [
    { id: ViewMode.HOME, label: 'Home', icon: _jsx(HomeIcon, {}) },
    { id: ViewMode.PROJECTS, label: 'Projects', icon: _jsx(FolderIcon, {}) },
    { id: ViewMode.SESSIONS, label: 'Sessions', icon: _jsx(MessageIcon, {}) },
    { id: ViewMode.PLAN, label: 'Plan', icon: _jsx(LayoutIcon, {}) },
    { id: ViewMode.AGENTS, label: 'Agents', icon: _jsx(UsersIcon, {}) },
    { id: ViewMode.MEMORY, label: 'Memory', icon: _jsx(DatabaseIcon, {}) },
];
export const Sidebar = ({ activeMode, onNavigate, className, style, }) => {
    const [hoveredItem, setHoveredItem] = useState(null);
    const navItems = getNavItems();
    const handleKeyDown = useCallback((event, mode) => {
        if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            onNavigate(mode);
        }
    }, [onNavigate]);
    const getNavItemStyle = (item) => {
        const isActive = activeMode === item.id;
        const isHovered = hoveredItem === item.id;
        return {
            display: 'flex',
            alignItems: 'center',
            gap: spacing[3],
            padding: `${spacing[3]}px ${spacing[4]}px`,
            borderRadius: borderRadius.xl,
            transition: transitions.normal,
            cursor: 'pointer',
            border: isActive ? `1px solid ${colors.primary[100]}` : '1px solid transparent',
            backgroundColor: isActive
                ? colors.primary[50]
                : isHovered
                    ? colors.slate[50]
                    : 'transparent',
            color: isActive ? colors.primary[600] : colors.slate[500],
            transform: isActive || isHovered ? 'translateX(4px)' : 'translateX(0)',
            boxShadow: isActive ? shadows.sm : 'none',
        };
    };
    const getIconStyle = (item) => {
        const isActive = activeMode === item.id;
        const isHovered = hoveredItem === item.id;
        return {
            color: isActive
                ? colors.primary[600]
                : isHovered
                    ? colors.slate[600]
                    : colors.slate[400],
            transition: transitions.normal,
        };
    };
    return (_jsxs("aside", { className: className, style: {
            display: 'none',
            width: 256,
            backgroundColor: 'rgba(255, 255, 255, 0.9)',
            backdropFilter: 'blur(12px)',
            borderRight: `1px solid ${colors.slate[200]}`,
            flexDirection: 'column',
            padding: `${spacing[6]}px 0`,
            zIndex: zIndex.fixed,
            flexShrink: 0,
            height: '100%',
            position: 'relative',
            boxShadow: shadows.sm,
            transition: transitions.slow,
            ...style,
            // 响应式：md 及以上显示
            '@media (min-width: 768px)': {
                display: 'flex',
            },
        }, role: "navigation", "aria-label": "Main navigation", children: [_jsxs("div", { style: {
                    padding: `0 ${spacing[6]}px`,
                    marginBottom: spacing[8],
                    display: 'flex',
                    alignItems: 'center',
                    gap: spacing[3],
                }, children: [_jsx("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            width: 32,
                            height: 32,
                            backgroundColor: colors.slate[900],
                            borderRadius: borderRadius.lg,
                            color: '#fff',
                            boxShadow: shadows.md,
                            transition: transitions.fast,
                        }, children: _jsx(TerminalIcon, {}) }), _jsx("h2", { style: {
                            color: colors.slate[800],
                            fontSize: fontSize.lg,
                            fontWeight: fontWeight.bold,
                            letterSpacing: '-0.025em',
                            cursor: 'default',
                        }, children: "CodeFlow" })] }), _jsx("nav", { style: {
                    display: 'flex',
                    flexDirection: 'column',
                    gap: spacing[1.5],
                    width: '100%',
                    padding: `0 ${spacing[3]}px`,
                    flex: 1,
                }, children: navItems.map((item) => (_jsxs("button", { onClick: () => onNavigate(item.id), onKeyDown: (e) => handleKeyDown(e, item.id), onMouseEnter: () => setHoveredItem(item.id), onMouseLeave: () => setHoveredItem(null), style: getNavItemStyle(item), "aria-label": item.label, "aria-current": activeMode === item.id ? 'page' : undefined, children: [_jsx("span", { style: getIconStyle(item), children: item.icon }), _jsx("span", { style: {
                                fontSize: fontSize.sm,
                                fontWeight: fontWeight.medium,
                            }, children: item.label }), activeMode === item.id && (_jsx("span", { style: {
                                marginLeft: 'auto',
                                width: 6,
                                height: 6,
                                backgroundColor: colors.primary[500],
                                borderRadius: borderRadius.full,
                                boxShadow: `0 0 8px ${colors.primary[500]}`,
                            } }))] }, item.id))) }), _jsxs("div", { style: {
                    marginTop: 'auto',
                    width: '100%',
                    padding: `0 ${spacing[3]}px`,
                }, children: [_jsxs("button", { onClick: () => onNavigate(ViewMode.SETTINGS), onKeyDown: (e) => handleKeyDown(e, ViewMode.SETTINGS), onMouseEnter: () => setHoveredItem(ViewMode.SETTINGS), onMouseLeave: () => setHoveredItem(null), style: getNavItemStyle({ id: ViewMode.SETTINGS, label: 'Settings', icon: _jsx(SettingsIcon, {}) }), "aria-label": "Settings", "aria-current": activeMode === ViewMode.SETTINGS ? 'page' : undefined, children: [_jsx("span", { style: getIconStyle({ id: ViewMode.SETTINGS, label: 'Settings', icon: _jsx(SettingsIcon, {}) }), children: _jsx(SettingsIcon, {}) }), _jsx("span", { style: {
                                    fontSize: fontSize.sm,
                                    fontWeight: fontWeight.medium,
                                }, children: "Settings" })] }), _jsxs("div", { onClick: () => onNavigate(ViewMode.SETTINGS), style: {
                            marginTop: spacing[4],
                            paddingTop: spacing[4],
                            borderTop: `1px solid ${colors.slate[100]}`,
                            display: 'flex',
                            alignItems: 'center',
                            gap: spacing[3],
                            padding: spacing[2],
                            cursor: 'pointer',
                            borderRadius: borderRadius.xl,
                            transition: transitions.fast,
                        }, role: "button", tabIndex: 0, onKeyDown: (e) => handleKeyDown(e, ViewMode.SETTINGS), children: [_jsx("div", { style: {
                                    width: 32,
                                    height: 32,
                                    borderRadius: borderRadius.full,
                                    background: `linear-gradient(135deg, ${colors.indigo[500]}, ${colors.primary[500]})`,
                                    boxShadow: `0 0 0 2px #fff`,
                                } }), _jsxs("div", { style: { display: 'flex', flexDirection: 'column', textAlign: 'left' }, children: [_jsx("span", { style: {
                                            fontSize: fontSize.xs,
                                            fontWeight: fontWeight.semibold,
                                            color: colors.slate[700],
                                        }, children: "Alex Designer" }), _jsx("span", { style: {
                                            fontSize: 10,
                                            color: colors.slate[400],
                                        }, children: "Pro Plan" })] })] })] })] }));
};
export default Sidebar;
//# sourceMappingURL=Sidebar.js.map