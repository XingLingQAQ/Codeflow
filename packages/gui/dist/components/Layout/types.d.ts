/**
 * Layout 组件类型定义
 */
export declare enum ViewMode {
    HOME = "HOME",
    PROJECTS = "PROJECTS",
    SESSIONS = "SESSIONS",
    MEMORY = "MEMORY",
    AGENTS = "AGENTS",
    PLAN = "PLAN",
    SETTINGS = "SETTINGS"
}
export interface NavItem {
    id: ViewMode;
    label: string;
    icon: React.ReactNode;
}
export interface SidebarProps {
    activeMode: ViewMode;
    onNavigate: (mode: ViewMode) => void;
    className?: string;
    style?: React.CSSProperties;
}
export interface MobileNavProps {
    activeMode: ViewMode;
    onNavigate: (mode: ViewMode) => void;
    className?: string;
    style?: React.CSSProperties;
}
export interface AppLayoutProps {
    activeMode: ViewMode;
    onNavigate: (mode: ViewMode) => void;
    children: React.ReactNode;
    className?: string;
    style?: React.CSSProperties;
}
//# sourceMappingURL=types.d.ts.map