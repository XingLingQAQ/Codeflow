import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Input - 统一风格输入框组件
 */
import { useState } from 'react';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from './tokens';
const sizeConfig = {
    sm: { padding: '6px 10px', fontSize: 12, iconSize: 14 },
    md: { padding: '8px 12px', fontSize: 13, iconSize: 16 },
    lg: { padding: '10px 14px', fontSize: 14, iconSize: 18 },
};
export const Input = ({ label, helperText, error = false, size = 'md', variant = 'default', icon, iconPosition = 'left', suffix, fullWidth = false, disabled, style, ...props }) => {
    const [isFocused, setIsFocused] = useState(false);
    const sizeStyles = sizeConfig[size];
    const getVariantStyles = () => {
        const base = {
            padding: sizeStyles.padding,
            paddingLeft: icon && iconPosition === 'left' ? 36 : sizeStyles.padding.split(' ')[1],
            paddingRight: icon && iconPosition === 'right' || suffix ? 36 : sizeStyles.padding.split(' ')[1],
            fontSize: sizeStyles.fontSize,
            fontWeight: fontWeight.medium,
            borderRadius: borderRadius.xl,
            transition: transitions.fast,
            width: fullWidth ? '100%' : 'auto',
            outline: 'none',
        };
        switch (variant) {
            case 'filled':
                return {
                    ...base,
                    backgroundColor: isFocused ? '#fff' : colors.slate[100],
                    border: `1px solid ${error ? colors.error.main : isFocused ? colors.primary[400] : 'transparent'}`,
                    boxShadow: isFocused ? `0 0 0 3px ${colors.primary[100]}` : 'none',
                };
            case 'outlined':
                return {
                    ...base,
                    backgroundColor: 'transparent',
                    border: `1px solid ${error ? colors.error.main : isFocused ? colors.primary[400] : colors.slate[300]}`,
                    boxShadow: isFocused ? `0 0 0 3px ${colors.primary[100]}` : 'none',
                };
            default:
                return {
                    ...base,
                    backgroundColor: isFocused ? '#fff' : colors.slate[50],
                    border: `1px solid ${error ? colors.error.main : isFocused ? colors.primary[400] : colors.slate[200]}`,
                    boxShadow: isFocused ? `0 0 0 3px ${colors.primary[100]}` : shadows.sm,
                };
        }
    };
    return (_jsxs("div", { style: { display: fullWidth ? 'block' : 'inline-block', width: fullWidth ? '100%' : 'auto' }, children: [label && (_jsx("label", { style: {
                    display: 'block',
                    marginBottom: spacing[1.5],
                    fontSize: fontSize.xs,
                    fontWeight: fontWeight.bold,
                    color: colors.slate[700],
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                }, children: label })), _jsxs("div", { style: { position: 'relative' }, children: [icon && iconPosition === 'left' && (_jsx("span", { style: {
                            position: 'absolute',
                            left: 12,
                            top: '50%',
                            transform: 'translateY(-50%)',
                            color: isFocused ? colors.primary[500] : colors.slate[400],
                            transition: transitions.fast,
                            display: 'flex',
                            alignItems: 'center',
                        }, children: icon })), _jsx("input", { ...props, disabled: disabled, onFocus: (e) => {
                            setIsFocused(true);
                            props.onFocus?.(e);
                        }, onBlur: (e) => {
                            setIsFocused(false);
                            props.onBlur?.(e);
                        }, style: {
                            ...getVariantStyles(),
                            opacity: disabled ? 0.6 : 1,
                            cursor: disabled ? 'not-allowed' : 'text',
                            ...style,
                        } }), (icon && iconPosition === 'right') || suffix ? (_jsx("span", { style: {
                            position: 'absolute',
                            right: 12,
                            top: '50%',
                            transform: 'translateY(-50%)',
                            color: colors.slate[400],
                            display: 'flex',
                            alignItems: 'center',
                        }, children: suffix || icon })) : null] }), helperText && (_jsx("span", { style: {
                    display: 'block',
                    marginTop: spacing[1],
                    fontSize: fontSize.xs,
                    color: error ? colors.error.main : colors.slate[500],
                }, children: helperText }))] }));
};
export default Input;
//# sourceMappingURL=Input.js.map