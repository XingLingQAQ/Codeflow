/**
 * Toggle - 统一风格开关组件
 */
import React from 'react';
export interface ToggleProps {
    checked?: boolean;
    onChange?: (checked: boolean) => void;
    disabled?: boolean;
    size?: 'sm' | 'md' | 'lg';
    label?: string;
    labelPosition?: 'left' | 'right';
    className?: string;
    style?: React.CSSProperties;
}
export declare const Toggle: React.FC<ToggleProps>;
export default Toggle;
//# sourceMappingURL=Toggle.d.ts.map