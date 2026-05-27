import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { colors, borderRadius, fontWeight, spacing } from './tokens';
const sizeConfig = {
    sm: { padding: '2px 6px', fontSize: 9, dotSize: 4 },
    md: { padding: '3px 8px', fontSize: 10, dotSize: 6 },
    lg: { padding: '4px 10px', fontSize: 11, dotSize: 8 },
};
const variantConfig = {
    default: { bg: colors.slate[100], color: colors.slate[600], border: colors.slate[200] },
    primary: { bg: colors.primary[50], color: colors.primary[700], border: colors.primary[200] },
    success: { bg: colors.success.light, color: colors.success.dark, border: '#86efac' },
    warning: { bg: colors.warning.light, color: colors.warning.dark, border: '#fcd34d' },
    error: { bg: colors.error.light, color: colors.error.dark, border: '#fca5a5' },
    info: { bg: colors.info.light, color: colors.info.dark, border: colors.primary[200] },
};
export const Badge = ({ children, variant = 'default', size = 'md', dot = false, dotColor, icon, className, style, }) => {
    const sizeStyles = sizeConfig[size];
    const variantStyles = variantConfig[variant];
    return (_jsxs("span", { className: className, style: {
            display: 'inline-flex',
            alignItems: 'center',
            gap: spacing[1],
            padding: sizeStyles.padding,
            fontSize: sizeStyles.fontSize,
            fontWeight: fontWeight.bold,
            textTransform: 'uppercase',
            letterSpacing: '0.05em',
            borderRadius: borderRadius.full,
            backgroundColor: variantStyles.bg,
            color: variantStyles.color,
            border: `1px solid ${variantStyles.border}`,
            ...style,
        }, children: [dot && (_jsx("span", { style: {
                    width: sizeStyles.dotSize,
                    height: sizeStyles.dotSize,
                    borderRadius: '50%',
                    backgroundColor: dotColor || variantStyles.color,
                } })), icon, children] }));
};
export const StatusBadge = ({ status, label, size = 'md' }) => {
    const statusConfig = {
        active: { variant: 'success', label: 'Active', dot: true, animate: true },
        inactive: { variant: 'default', label: 'Inactive', dot: true, animate: false },
        pending: { variant: 'warning', label: 'Pending', dot: true, animate: true },
        completed: { variant: 'primary', label: 'Completed', dot: false, animate: false },
        error: { variant: 'error', label: 'Error', dot: true, animate: false },
    };
    const config = statusConfig[status];
    return (_jsxs(Badge, { variant: config.variant, size: size, dot: config.dot, children: [config.animate && (_jsx("span", { style: {
                    width: 6,
                    height: 6,
                    borderRadius: '50%',
                    backgroundColor: 'currentColor',
                    animation: 'pulse 1.5s infinite',
                } })), label || config.label, _jsx("style", { children: `
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }
      ` })] }));
};
export default Badge;
//# sourceMappingURL=Badge.js.map