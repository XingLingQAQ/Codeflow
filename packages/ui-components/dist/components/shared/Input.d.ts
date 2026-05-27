/**
 * Input - 统一风格输入框组件
 */
import React from 'react';
export interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'size'> {
    label?: string;
    helperText?: string;
    error?: boolean;
    size?: 'sm' | 'md' | 'lg';
    variant?: 'default' | 'filled' | 'outlined';
    icon?: React.ReactNode;
    iconPosition?: 'left' | 'right';
    suffix?: React.ReactNode;
    fullWidth?: boolean;
}
export declare const Input: React.FC<InputProps>;
export default Input;
//# sourceMappingURL=Input.d.ts.map