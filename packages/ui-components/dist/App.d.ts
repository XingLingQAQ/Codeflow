/**
 * App - 主入口组件
 * 路由状态管理、全局 Context、视图切换
 */
import React from 'react';
import { ViewMode } from './components';
export type ThemeMode = 'light' | 'dark' | 'system';
interface NavigationContextValue {
    currentView: ViewMode;
    navigate: (view: ViewMode) => void;
    goBack: () => void;
    history: ViewMode[];
}
export declare const useNavigation: () => NavigationContextValue;
interface ThemeContextValue {
    theme: ThemeMode;
    setTheme: (theme: ThemeMode) => void;
    isDark: boolean;
}
export declare const useTheme: () => ThemeContextValue;
export interface AppProps {
    initialView?: ViewMode;
    initialTheme?: ThemeMode;
}
export declare const App: React.FC<AppProps>;
export default App;
//# sourceMappingURL=App.d.ts.map