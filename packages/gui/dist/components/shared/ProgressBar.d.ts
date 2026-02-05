/**
 * ProgressBar - 进度条组件
 * 支持 0-100% 和状态颜色
 */
import React from 'react';
export type ProgressStatus = 'default' | 'success' | 'warning' | 'error' | 'info';
export interface ProgressBarProps {
    value: number;
    max?: number;
    status?: ProgressStatus;
    showLabel?: boolean;
    labelPosition?: 'inside' | 'outside' | 'none';
    size?: 'sm' | 'md' | 'lg';
    animated?: boolean;
    striped?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
export declare const ProgressBar: React.FC<ProgressBarProps>;
export interface CircularProgressProps {
    value: number;
    max?: number;
    size?: number;
    strokeWidth?: number;
    status?: ProgressStatus;
    showLabel?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
export declare const CircularProgress: React.FC<CircularProgressProps>;
export default ProgressBar;
//# sourceMappingURL=ProgressBar.d.ts.map