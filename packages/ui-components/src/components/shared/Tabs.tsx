/**
 * Tabs - 统一风格标签页组件
 */

import React, { useState } from 'react';
import { colors, borderRadius, fontSize, fontWeight, spacing, transitions } from './tokens';

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

const sizeConfig = {
  sm: { padding: '6px 12px', fontSize: 11, gap: 4 },
  md: { padding: '8px 16px', fontSize: 12, gap: 6 },
  lg: { padding: '10px 20px', fontSize: 13, gap: 8 },
};

export const Tabs: React.FC<TabsProps> = ({
  tabs,
  activeTab,
  onChange,
  variant = 'default',
  size = 'md',
  fullWidth = false,
  className,
  style,
}) => {
  const [hoveredTab, setHoveredTab] = useState<string | null>(null);
  const sizeStyles = sizeConfig[size];

  const getTabStyles = (tab: TabItem): React.CSSProperties => {
    const isActive = tab.id === activeTab;
    const isHovered = tab.id === hoveredTab;

    const base: React.CSSProperties = {
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      gap: sizeStyles.gap,
      padding: sizeStyles.padding,
      fontSize: sizeStyles.fontSize,
      fontWeight: isActive ? fontWeight.bold : fontWeight.medium,
      cursor: tab.disabled ? 'not-allowed' : 'pointer',
      opacity: tab.disabled ? 0.5 : 1,
      transition: transitions.fast,
      flex: fullWidth ? 1 : 'none',
      border: 'none',
      outline: 'none',
      whiteSpace: 'nowrap',
    };

    switch (variant) {
      case 'pills':
        return {
          ...base,
          borderRadius: borderRadius.full,
          backgroundColor: isActive
            ? colors.primary[500]
            : isHovered
            ? colors.slate[100]
            : 'transparent',
          color: isActive ? '#fff' : colors.slate[600],
        };
      case 'underline':
        return {
          ...base,
          backgroundColor: 'transparent',
          color: isActive ? colors.primary[600] : isHovered ? colors.slate[700] : colors.slate[500],
          borderBottom: `2px solid ${isActive ? colors.primary[500] : 'transparent'}`,
          borderRadius: 0,
          marginBottom: -1,
        };
      default:
        return {
          ...base,
          borderRadius: borderRadius.lg,
          backgroundColor: isActive
            ? colors.primary[50]
            : isHovered
            ? colors.slate[50]
            : 'transparent',
          color: isActive ? colors.primary[700] : colors.slate[600],
          border: `1px solid ${isActive ? colors.primary[200] : 'transparent'}`,
        };
    }
  };

  const getContainerStyles = (): React.CSSProperties => {
    const base: React.CSSProperties = {
      display: 'flex',
      alignItems: 'center',
      gap: variant === 'underline' ? 0 : spacing[1],
    };

    switch (variant) {
      case 'underline':
        return {
          ...base,
          borderBottom: `1px solid ${colors.slate[200]}`,
        };
      default:
        return {
          ...base,
          padding: spacing[1],
          backgroundColor: colors.slate[100],
          borderRadius: borderRadius.xl,
        };
    }
  };

  return (
    <div className={className} style={{ ...getContainerStyles(), ...style }} role="tablist">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          role="tab"
          aria-selected={tab.id === activeTab}
          disabled={tab.disabled}
          onClick={() => !tab.disabled && onChange?.(tab.id)}
          onMouseEnter={() => setHoveredTab(tab.id)}
          onMouseLeave={() => setHoveredTab(null)}
          style={getTabStyles(tab)}
        >
          {tab.icon}
          <span>{tab.label}</span>
          {tab.badge !== undefined && (
            <span
              style={{
                padding: '1px 6px',
                fontSize: 9,
                fontWeight: fontWeight.bold,
                borderRadius: borderRadius.full,
                backgroundColor:
                  tab.id === activeTab ? 'rgba(255,255,255,0.3)' : colors.slate[200],
                color: tab.id === activeTab ? '#fff' : colors.slate[600],
              }}
            >
              {tab.badge}
            </span>
          )}
        </button>
      ))}
    </div>
  );
};

export const TabPanel: React.FC<{
  children: React.ReactNode;
  tabId: string;
  activeTab: string;
  style?: React.CSSProperties;
}> = ({ children, tabId, activeTab, style }) => {
  if (tabId !== activeTab) return null;
  return (
    <div role="tabpanel" style={{ padding: spacing[4], ...style }}>
      {children}
    </div>
  );
};

export default Tabs;
