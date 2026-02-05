/**
 * HomeView - 首页视图
 * 渐变背景 + 浮动动画、模型选择器、大型输入框 + 快捷操作
 */
import React from 'react';
export interface HomeViewProps {
    onStartSession?: (prompt: string, model: string) => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const HomeView: React.FC<HomeViewProps>;
export default HomeView;
//# sourceMappingURL=HomeView.d.ts.map