import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Card - 统一风格卡片组件
 */
import React from 'react';
import { colors, borderRadius, shadows, spacing, transitions } from './tokens';
const paddingConfig = {
    none: 0,
    sm: spacing[3],
    md: spacing[4],
    lg: spacing[6],
};
export const Card = ({ children, variant = 'default', padding = 'md', hoverable = false, selected = false, onClick, className, style, }) => {
    const [isHovered, setIsHovered] = React.useState(false);
    const getVariantStyles = () => {
        const base = {
            borderRadius: borderRadius['2xl'],
            transition: transitions.normal,
            cursor: onClick || hoverable ? 'pointer' : 'default',
        };
        switch (variant) {
            case 'outlined':
                return {
                    ...base,
                    backgroundColor: '#fff',
                    border: `1px solid ${selected ? colors.primary[400] : colors.slate[200]}`,
                    boxShadow: selected ? `0 0 0 3px ${colors.primary[100]}` : 'none',
                };
            case 'elevated':
                return {
                    ...base,
                    backgroundColor: '#fff',
                    border: 'none',
                    boxShadow: isHovered && hoverable ? shadows.xl : shadows.lg,
                };
            case 'glass':
                return {
                    ...base,
                    backgroundColor: 'rgba(255, 255, 255, 0.8)',
                    backdropFilter: 'blur(12px)',
                    border: '1px solid rgba(255, 255, 255, 0.5)',
                    boxShadow: shadows.lg,
                };
            default:
                return {
                    ...base,
                    backgroundColor: selected ? colors.primary[50] : '#fff',
                    border: `1px solid ${selected ? colors.primary[300] : isHovered && hoverable ? colors.primary[200] : colors.slate[200]}`,
                    boxShadow: isHovered && hoverable ? shadows.lg : shadows.sm,
                };
        }
    };
    return (_jsx("div", { className: className, onClick: onClick, onMouseEnter: () => setIsHovered(true), onMouseLeave: () => setIsHovered(false), style: {
            ...getVariantStyles(),
            padding: paddingConfig[padding],
            transform: isHovered && hoverable ? 'translateY(-2px)' : 'none',
            ...style,
        }, children: children }));
};
export const CardHeader = ({ children, action, style }) => (_jsxs("div", { style: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        marginBottom: spacing[3],
        ...style,
    }, children: [children, action] }));
export const CardContent = ({ children, style }) => (_jsx("div", { style: style, children: children }));
export const CardFooter = ({ children, style }) => (_jsx("div", { style: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'flex-end',
        gap: spacing[2],
        marginTop: spacing[4],
        paddingTop: spacing[3],
        borderTop: `1px solid ${colors.slate[100]}`,
        ...style,
    }, children: children }));
export default Card;
//# sourceMappingURL=Card.js.map