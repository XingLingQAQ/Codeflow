import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * AppLayout - 主布局容器
 * 响应式切换：桌面端显示 Sidebar，移动端显示 MobileNav
 */
import { useState, useEffect } from 'react';
import { Sidebar } from './Sidebar';
import { MobileNav } from './MobileNav';
import { colors, breakpoints, transitions, } from '../shared/tokens';
export const AppLayout = ({ activeMode, onNavigate, children, className, style, }) => {
    const [isMobile, setIsMobile] = useState(false);
    useEffect(() => {
        const checkMobile = () => {
            setIsMobile(window.innerWidth < breakpoints.md);
        };
        // Initial check
        checkMobile();
        // Listen for resize
        window.addEventListener('resize', checkMobile);
        return () => window.removeEventListener('resize', checkMobile);
    }, []);
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            minHeight: '100vh',
            backgroundColor: colors.slate[50],
            transition: transitions.normal,
            ...style,
        }, children: [!isMobile && (_jsx(Sidebar, { activeMode: activeMode, onNavigate: onNavigate, style: { display: 'flex' } })), _jsx("main", { style: {
                    flex: 1,
                    display: 'flex',
                    flexDirection: 'column',
                    minWidth: 0,
                    // Add bottom padding on mobile for MobileNav
                    paddingBottom: isMobile ? 80 : 0,
                    transition: transitions.normal,
                }, children: children }), isMobile && (_jsx(MobileNav, { activeMode: activeMode, onNavigate: onNavigate }))] }));
};
export default AppLayout;
//# sourceMappingURL=AppLayout.js.map