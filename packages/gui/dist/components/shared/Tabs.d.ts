/**
 * Tabs - 统一风格标签页组件
 */
import React from 'react';
export interface TabItem {
    id: string;
    label: string;
    icon?: React.ReactNode;
    badge?: string | number;
    disabled?: boolean;
}
export interface TabsProps {
    tabs: TabItem[];
    activeTab?: string;
    onChange?: (tabId: string) => void;
    variant?: 'default' | 'pills' | 'underline';
    size?: 'sm' | 'md' | 'lg';
    fullWidth?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
export declare const Tabs: React.FC<TabsProps>;
export declare const TabPanel: React.FC<{
    children: React.ReactNode;
    tabId: string;
    activeTab: string;
    style?: React.CSSProperties;
}>;
export default Tabs;
//# sourceMappingURL=Tabs.d.ts.map