/**
 * Card - 统一风格卡片组件
 */
import React from 'react';
export interface CardProps {
    children: React.ReactNode;
    variant?: 'default' | 'outlined' | 'elevated' | 'glass';
    padding?: 'none' | 'sm' | 'md' | 'lg';
    hoverable?: boolean;
    selected?: boolean;
    onClick?: () => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const Card: React.FC<CardProps>;
export declare const CardHeader: React.FC<{
    children: React.ReactNode;
    action?: React.ReactNode;
    style?: React.CSSProperties;
}>;
export declare const CardContent: React.FC<{
    children: React.ReactNode;
    style?: React.CSSProperties;
}>;
export declare const CardFooter: React.FC<{
    children: React.ReactNode;
    style?: React.CSSProperties;
}>;
export default Card;
//# sourceMappingURL=Card.d.ts.map