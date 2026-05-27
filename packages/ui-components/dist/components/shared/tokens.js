/**
 * Design Tokens - 统一设计常量
 * 基于 codeflow_template 风格
 */
export const colors = {
    // Primary
    primary: {
        50: '#eff6ff',
        100: '#dbeafe',
        200: '#bfdbfe',
        300: '#93c5fd',
        400: '#60a5fa',
        500: '#3b82f6',
        600: '#2563eb',
        700: '#1d4ed8',
        800: '#1e40af',
        900: '#1e3a8a',
    },
    // Indigo
    indigo: {
        50: '#eef2ff',
        100: '#e0e7ff',
        200: '#c7d2fe',
        300: '#a5b4fc',
        400: '#818cf8',
        500: '#6366f1',
        600: '#4f46e5',
        700: '#4338ca',
        800: '#3730a3',
        900: '#312e81',
    },
    // Slate
    slate: {
        50: '#f8fafc',
        100: '#f1f5f9',
        200: '#e2e8f0',
        300: '#cbd5e1',
        400: '#94a3b8',
        500: '#64748b',
        600: '#475569',
        700: '#334155',
        800: '#1e293b',
        900: '#0f172a',
    },
    // Semantic
    success: {
        light: '#dcfce7',
        main: '#22c55e',
        dark: '#15803d',
    },
    warning: {
        light: '#fef3c7',
        main: '#f59e0b',
        dark: '#b45309',
    },
    error: {
        light: '#fee2e2',
        main: '#ef4444',
        dark: '#b91c1c',
    },
    info: {
        light: '#dbeafe',
        main: '#3b82f6',
        dark: '#1d4ed8',
    },
};
export const spacing = {
    0: 0,
    0.5: 2,
    1: 4,
    1.5: 6,
    2: 8,
    2.5: 10,
    3: 12,
    3.5: 14,
    4: 16,
    5: 20,
    6: 24,
    7: 28,
    8: 32,
    9: 36,
    10: 40,
    12: 48,
    14: 56,
    16: 64,
};
export const borderRadius = {
    none: 0,
    sm: 4,
    md: 6,
    lg: 8,
    xl: 12,
    '2xl': 16,
    '3xl': 24,
    full: 9999,
};
export const fontSize = {
    xs: 10,
    sm: 12,
    base: 14,
    lg: 16,
    xl: 18,
    '2xl': 20,
    '3xl': 24,
    '4xl': 30,
};
export const fontWeight = {
    normal: 400,
    medium: 500,
    semibold: 600,
    bold: 700,
};
export const shadows = {
    none: 'none',
    sm: '0 1px 2px 0 rgba(0, 0, 0, 0.05)',
    md: '0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -2px rgba(0, 0, 0, 0.1)',
    lg: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -4px rgba(0, 0, 0, 0.1)',
    xl: '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 8px 10px -6px rgba(0, 0, 0, 0.1)',
    '2xl': '0 25px 50px -12px rgba(0, 0, 0, 0.25)',
    inner: 'inset 0 2px 4px 0 rgba(0, 0, 0, 0.05)',
    blue: '0 4px 14px 0 rgba(59, 130, 246, 0.2)',
    indigo: '0 4px 14px 0 rgba(99, 102, 241, 0.2)',
};
export const transitions = {
    fast: 'all 0.15s ease',
    normal: 'all 0.2s ease',
    slow: 'all 0.3s ease',
};
// Animation keyframes
export const keyframes = {
    float: `
    @keyframes float {
      0%, 100% { transform: translateY(0px); }
      50% { transform: translateY(-10px); }
    }
  `,
    pulseSlow: `
    @keyframes pulse-slow {
      0%, 100% { opacity: 1; }
      50% { opacity: 0.5; }
    }
  `,
    ping: `
    @keyframes ping {
      75%, 100% { transform: scale(2); opacity: 0; }
    }
  `,
    slideDown: `
    @keyframes slideDown {
      from { opacity: 0; transform: translateY(-8px); }
      to { opacity: 1; transform: translateY(0); }
    }
  `,
    slideUp: `
    @keyframes slideUp {
      from { opacity: 0; transform: translateY(8px); }
      to { opacity: 1; transform: translateY(0); }
    }
  `,
    fadeIn: `
    @keyframes fadeIn {
      from { opacity: 0; }
      to { opacity: 1; }
    }
  `,
    progress: `
    @keyframes progress {
      0% { width: 0%; }
      50% { width: 60%; }
      100% { width: 100%; }
    }
  `,
};
// Animation utilities
export const animations = {
    float: 'float 3s ease-in-out infinite',
    pulseSlow: 'pulse-slow 2s ease-in-out infinite',
    ping: 'ping 1s cubic-bezier(0, 0, 0.2, 1) infinite',
    slideDown: 'slideDown 0.15s ease',
    slideUp: 'slideUp 0.15s ease',
    fadeIn: 'fadeIn 0.3s ease',
    progress: 'progress 2s ease-in-out infinite',
};
// Z-index scale
export const zIndex = {
    dropdown: 1000,
    sticky: 1020,
    fixed: 1030,
    modalBackdrop: 1040,
    modal: 1050,
    popover: 1060,
    tooltip: 1070,
    toast: 1080,
};
// Responsive breakpoints
export const breakpoints = {
    sm: 640,
    md: 768,
    lg: 1024,
    xl: 1280,
    '2xl': 1536,
};
// Media query helpers
export const mediaQueries = {
    sm: `@media (min-width: ${breakpoints.sm}px)`,
    md: `@media (min-width: ${breakpoints.md}px)`,
    lg: `@media (min-width: ${breakpoints.lg}px)`,
    xl: `@media (min-width: ${breakpoints.xl}px)`,
    '2xl': `@media (min-width: ${breakpoints['2xl']}px)`,
};
// 组件样式预设
export const componentStyles = {
    card: {
        backgroundColor: '#fff',
        borderRadius: borderRadius.xl,
        border: `1px solid ${colors.slate[200]}`,
        boxShadow: shadows.sm,
    },
    cardHover: {
        borderColor: colors.primary[300],
        boxShadow: shadows.lg,
    },
    input: {
        padding: `${spacing[2]}px ${spacing[3]}px`,
        fontSize: fontSize.sm,
        borderRadius: borderRadius.lg,
        border: `1px solid ${colors.slate[200]}`,
        backgroundColor: colors.slate[50],
        transition: transitions.fast,
    },
    inputFocus: {
        borderColor: colors.primary[400],
        backgroundColor: '#fff',
        boxShadow: `0 0 0 3px ${colors.primary[100]}`,
        outline: 'none',
    },
    button: {
        primary: {
            backgroundColor: colors.slate[900],
            color: '#fff',
            padding: `${spacing[2]}px ${spacing[4]}px`,
            borderRadius: borderRadius.xl,
            fontSize: fontSize.sm,
            fontWeight: fontWeight.bold,
            border: 'none',
            cursor: 'pointer',
            transition: transitions.fast,
        },
        secondary: {
            backgroundColor: '#fff',
            color: colors.slate[700],
            padding: `${spacing[2]}px ${spacing[4]}px`,
            borderRadius: borderRadius.xl,
            fontSize: fontSize.sm,
            fontWeight: fontWeight.medium,
            border: `1px solid ${colors.slate[200]}`,
            cursor: 'pointer',
            transition: transitions.fast,
        },
        ghost: {
            backgroundColor: 'transparent',
            color: colors.slate[600],
            padding: `${spacing[2]}px ${spacing[3]}px`,
            borderRadius: borderRadius.lg,
            fontSize: fontSize.sm,
            fontWeight: fontWeight.medium,
            border: 'none',
            cursor: 'pointer',
            transition: transitions.fast,
        },
    },
    badge: {
        padding: `${spacing[0.5]}px ${spacing[2]}px`,
        borderRadius: borderRadius.full,
        fontSize: fontSize.xs,
        fontWeight: fontWeight.bold,
    },
};
//# sourceMappingURL=tokens.js.map