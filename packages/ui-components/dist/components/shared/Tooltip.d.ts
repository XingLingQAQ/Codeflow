/**
 * Tooltip - 统一风格提示组件
 */
import React from 'react';
export interface TooltipProps {
    content: React.ReactNode;
    children: React.ReactNode;
    position?: 'top' | 'bottom' | 'left' | 'right';
    delay?: number;
    disabled?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
export declare const Tooltip: React.FC<TooltipProps>;
export default Tooltip;
//# sourceMappingURL=Tooltip.d.ts.map