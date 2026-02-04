import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Tooltip - 统一风格提示组件
 */
import { useState, useRef, useEffect } from 'react';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from './tokens';
export const Tooltip = ({ content, children, position = 'top', delay = 200, disabled = false, className, style, }) => {
    const [isVisible, setIsVisible] = useState(false);
    const timeoutRef = useRef(null);
    const containerRef = useRef(null);
    const showTooltip = () => {
        if (disabled)
            return;
        timeoutRef.current = setTimeout(() => setIsVisible(true), delay);
    };
    const hideTooltip = () => {
        if (timeoutRef.current) {
            clearTimeout(timeoutRef.current);
        }
        setIsVisible(false);
    };
    useEffect(() => {
        return () => {
            if (timeoutRef.current) {
                clearTimeout(timeoutRef.current);
            }
        };
    }, []);
    const getPositionStyles = () => {
        const base = {
            position: 'absolute',
            zIndex: 1000,
            padding: `${spacing[1.5]}px ${spacing[2.5]}px`,
            backgroundColor: colors.slate[800],
            color: '#fff',
            fontSize: fontSize.xs,
            fontWeight: fontWeight.medium,
            borderRadius: borderRadius.lg,
            boxShadow: shadows.lg,
            whiteSpace: 'nowrap',
            pointerEvents: 'none',
            opacity: isVisible ? 1 : 0,
            transition: transitions.fast,
        };
        switch (position) {
            case 'bottom':
                return { ...base, top: '100%', left: '50%', transform: 'translateX(-50%)', marginTop: 8 };
            case 'left':
                return { ...base, right: '100%', top: '50%', transform: 'translateY(-50%)', marginRight: 8 };
            case 'right':
                return { ...base, left: '100%', top: '50%', transform: 'translateY(-50%)', marginLeft: 8 };
            default:
                return { ...base, bottom: '100%', left: '50%', transform: 'translateX(-50%)', marginBottom: 8 };
        }
    };
    return (_jsxs("div", { ref: containerRef, className: className, style: { position: 'relative', display: 'inline-block', ...style }, onMouseEnter: showTooltip, onMouseLeave: hideTooltip, onFocus: showTooltip, onBlur: hideTooltip, children: [children, isVisible && _jsx("div", { style: getPositionStyles(), children: content })] }));
};
export default Tooltip;
//# sourceMappingURL=Tooltip.js.map