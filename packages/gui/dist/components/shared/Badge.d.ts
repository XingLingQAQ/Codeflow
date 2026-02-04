/**
 * Badge - 统一风格徽章组件
 */
import React from 'react';
export interface BadgeProps {
    children: React.ReactNode;
    variant?: 'default' | 'primary' | 'success' | 'warning' | 'error' | 'info';
    size?: 'sm' | 'md' | 'lg';
    dot?: boolean;
    dotColor?: string;
    icon?: React.ReactNode;
    className?: string;
    style?: React.CSSProperties;
}
export declare const Badge: React.FC<BadgeProps>;
export declare const StatusBadge: React.FC<{
    status: 'active' | 'inactive' | 'pending' | 'completed' | 'error';
    label?: string;
    size?: 'sm' | 'md' | 'lg';
}>;
export default Badge;
//# sourceMappingURL=Badge.d.ts.map