import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * ProgressBar - 进度条组件
 * 支持 0-100% 和状态颜色
 */
import { useEffect, useState } from 'react';
import { colors, spacing, borderRadius, fontSize, fontWeight, } from './tokens';
const getStatusColor = (status) => {
    switch (status) {
        case 'success':
            return colors.success.main;
        case 'warning':
            return colors.warning.main;
        case 'error':
            return colors.error.main;
        case 'info':
            return colors.info.main;
        case 'default':
        default:
            return colors.primary[500];
    }
};
const getStatusBgColor = (status) => {
    switch (status) {
        case 'success':
            return colors.success.light;
        case 'warning':
            return colors.warning.light;
        case 'error':
            return colors.error.light;
        case 'info':
            return colors.info.light;
        case 'default':
        default:
            return colors.primary[100];
    }
};
const getSizeStyles = (size) => {
    switch (size) {
        case 'sm':
            return { height: 4, fontSize: fontSize.xs };
        case 'lg':
            return { height: 16, fontSize: fontSize.sm };
        case 'md':
        default:
            return { height: 8, fontSize: fontSize.xs };
    }
};
// Inject keyframes for striped animation
const injectStripedKeyframes = () => {
    const styleId = 'progress-striped-keyframes';
    if (typeof document !== 'undefined' && !document.getElementById(styleId)) {
        const style = document.createElement('style');
        style.id = styleId;
        style.textContent = `
      @keyframes progress-stripes {
        from { background-position: 1rem 0; }
        to { background-position: 0 0; }
      }
    `;
        document.head.appendChild(style);
    }
};
export const ProgressBar = ({ value, max = 100, status = 'default', showLabel = false, labelPosition = 'outside', size = 'md', animated = false, striped = false, className, style, }) => {
    const [displayValue, setDisplayValue] = useState(0);
    const percentage = Math.min(Math.max((value / max) * 100, 0), 100);
    const sizeStyles = getSizeStyles(size);
    const statusColor = getStatusColor(status);
    const statusBgColor = getStatusBgColor(status);
    useEffect(() => {
        if (striped) {
            injectStripedKeyframes();
        }
    }, [striped]);
    // Animate value change
    useEffect(() => {
        if (animated) {
            const timer = setTimeout(() => {
                setDisplayValue(percentage);
            }, 50);
            return () => clearTimeout(timer);
        }
        else {
            setDisplayValue(percentage);
        }
    }, [percentage, animated]);
    const stripedStyle = striped
        ? {
            backgroundImage: `linear-gradient(
          45deg,
          rgba(255, 255, 255, 0.15) 25%,
          transparent 25%,
          transparent 50%,
          rgba(255, 255, 255, 0.15) 50%,
          rgba(255, 255, 255, 0.15) 75%,
          transparent 75%,
          transparent
        )`,
            backgroundSize: '1rem 1rem',
            animation: animated ? 'progress-stripes 1s linear infinite' : undefined,
        }
        : {};
    const renderLabel = () => {
        if (!showLabel || labelPosition === 'none')
            return null;
        const labelText = `${Math.round(percentage)}%`;
        if (labelPosition === 'inside' && size === 'lg') {
            return null; // Will be rendered inside the bar
        }
        return (_jsx("span", { style: {
                fontSize: sizeStyles.fontSize,
                fontWeight: fontWeight.medium,
                color: colors.slate[600],
                marginLeft: spacing[2],
                minWidth: 36,
                textAlign: 'right',
            }, children: labelText }));
    };
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            alignItems: 'center',
            width: '100%',
            ...style,
        }, children: [_jsx("div", { style: {
                    flex: 1,
                    height: sizeStyles.height,
                    backgroundColor: statusBgColor,
                    borderRadius: borderRadius.full,
                    overflow: 'hidden',
                    position: 'relative',
                }, role: "progressbar", "aria-valuenow": value, "aria-valuemin": 0, "aria-valuemax": max, "aria-label": `Progress: ${Math.round(percentage)}%`, children: _jsx("div", { style: {
                        height: '100%',
                        width: `${displayValue}%`,
                        backgroundColor: statusColor,
                        borderRadius: borderRadius.full,
                        transition: animated ? 'width 0.3s ease-out' : 'none',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'flex-end',
                        paddingRight: size === 'lg' && showLabel && labelPosition === 'inside' ? spacing[2] : 0,
                        ...stripedStyle,
                    }, children: size === 'lg' && showLabel && labelPosition === 'inside' && displayValue > 10 && (_jsxs("span", { style: {
                            fontSize: sizeStyles.fontSize,
                            fontWeight: fontWeight.bold,
                            color: '#fff',
                        }, children: [Math.round(percentage), "%"] })) }) }), labelPosition === 'outside' && renderLabel()] }));
};
export const CircularProgress = ({ value, max = 100, size = 48, strokeWidth = 4, status = 'default', showLabel = true, className, style, }) => {
    const percentage = Math.min(Math.max((value / max) * 100, 0), 100);
    const statusColor = getStatusColor(status);
    const statusBgColor = getStatusBgColor(status);
    const radius = (size - strokeWidth) / 2;
    const circumference = radius * 2 * Math.PI;
    const offset = circumference - (percentage / 100) * circumference;
    return (_jsxs("div", { className: className, style: {
            position: 'relative',
            width: size,
            height: size,
            ...style,
        }, role: "progressbar", "aria-valuenow": value, "aria-valuemin": 0, "aria-valuemax": max, children: [_jsxs("svg", { width: size, height: size, style: { transform: 'rotate(-90deg)' }, children: [_jsx("circle", { cx: size / 2, cy: size / 2, r: radius, fill: "none", stroke: statusBgColor, strokeWidth: strokeWidth }), _jsx("circle", { cx: size / 2, cy: size / 2, r: radius, fill: "none", stroke: statusColor, strokeWidth: strokeWidth, strokeDasharray: circumference, strokeDashoffset: offset, strokeLinecap: "round", style: { transition: 'stroke-dashoffset 0.3s ease-out' } })] }), showLabel && (_jsxs("div", { style: {
                    position: 'absolute',
                    top: '50%',
                    left: '50%',
                    transform: 'translate(-50%, -50%)',
                    fontSize: size > 40 ? fontSize.sm : fontSize.xs,
                    fontWeight: fontWeight.semibold,
                    color: colors.slate[700],
                }, children: [Math.round(percentage), "%"] }))] }));
};
export default ProgressBar;
//# sourceMappingURL=ProgressBar.js.map