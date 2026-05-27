/**
 * AppLayout - 主布局容器
 * 响应式切换：桌面端显示 Sidebar，移动端显示 MobileNav
 */

import React, { useState, useEffect } from 'react';
import { AppLayoutProps } from './types';
import { Sidebar } from './Sidebar';
import { MobileNav } from './MobileNav';
import {
  colors,
  spacing,
  breakpoints,
  transitions,
} from '../shared/tokens';

export const AppLayout: React.FC<AppLayoutProps> = ({
  activeMode,
  onNavigate,
  children,
  className,
  style,
}) => {
  const [isMobile, setIsMobile] = useState(false);

  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth < breakpoints.md);
    };

    // Initial check
    checkMobile();

    // Listen for resize
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        minHeight: '100vh',
        backgroundColor: colors.slate[50],
        transition: transitions.normal,
        ...style,
      }}
    >
      {/* Desktop Sidebar - hidden on mobile */}
      {!isMobile && (
        <Sidebar
          activeMode={activeMode}
          onNavigate={onNavigate}
          style={{ display: 'flex' }}
        />
      )}

      {/* Main Content Area */}
      <main
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          minWidth: 0,
          // Add bottom padding on mobile for MobileNav
          paddingBottom: isMobile ? 80 : 0,
          transition: transitions.normal,
        }}
      >
        {children}
      </main>

      {/* Mobile Navigation - shown only on mobile */}
      {isMobile && (
        <MobileNav
          activeMode={activeMode}
          onNavigate={onNavigate}
        />
      )}
    </div>
  );
};

export default AppLayout;
