import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Modal - 统一风格模态框组件
 */
import { useEffect, useCallback } from 'react';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from './tokens';
const sizeConfig = {
    sm: { maxWidth: 400 },
    md: { maxWidth: 560 },
    lg: { maxWidth: 720 },
    xl: { maxWidth: 960 },
    full: { maxWidth: '90vw' },
};
export const Modal = ({ isOpen, onClose, title, subtitle, icon, children, footer, size = 'md', closeOnOverlayClick = true, closeOnEscape = true, showCloseButton = true, className, style, }) => {
    const handleKeyDown = useCallback((event) => {
        if (closeOnEscape && event.key === 'Escape') {
            onClose();
        }
    }, [closeOnEscape, onClose]);
    useEffect(() => {
        if (isOpen) {
            document.addEventListener('keydown', handleKeyDown);
            document.body.style.overflow = 'hidden';
        }
        return () => {
            document.removeEventListener('keydown', handleKeyDown);
            document.body.style.overflow = '';
        };
    }, [isOpen, handleKeyDown]);
    if (!isOpen)
        return null;
    return (_jsxs("div", { style: {
            position: 'fixed',
            inset: 0,
            zIndex: 100,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: spacing[4],
        }, children: [_jsx("div", { onClick: closeOnOverlayClick ? onClose : undefined, style: {
                    position: 'absolute',
                    inset: 0,
                    backgroundColor: 'rgba(255, 255, 255, 0.2)',
                    backdropFilter: 'blur(4px)',
                    transition: transitions.normal,
                } }), _jsxs("div", { className: className, style: {
                    position: 'relative',
                    width: '100%',
                    maxWidth: sizeConfig[size].maxWidth,
                    maxHeight: '85vh',
                    backgroundColor: 'rgba(255, 255, 255, 0.95)',
                    backdropFilter: 'blur(20px)',
                    borderRadius: borderRadius['3xl'],
                    boxShadow: shadows['2xl'],
                    border: '1px solid rgba(255, 255, 255, 0.5)',
                    display: 'flex',
                    flexDirection: 'column',
                    overflow: 'hidden',
                    animation: 'modalIn 0.2s ease',
                    ...style,
                }, children: [(title || showCloseButton) && (_jsxs("div", { style: {
                            padding: `${spacing[4]}px ${spacing[6]}px`,
                            borderBottom: `1px solid ${colors.slate[200]}`,
                            backgroundColor: 'rgba(255, 255, 255, 0.5)',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                        }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: spacing[3] }, children: [icon && (_jsx("div", { style: {
                                            width: 40,
                                            height: 40,
                                            borderRadius: borderRadius.xl,
                                            backgroundColor: colors.primary[50],
                                            border: `1px solid ${colors.primary[100]}`,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            color: colors.primary[600],
                                        }, children: icon })), _jsxs("div", { children: [title && (_jsx("h3", { style: {
                                                    fontSize: fontSize.lg,
                                                    fontWeight: fontWeight.bold,
                                                    color: colors.slate[800],
                                                    margin: 0,
                                                }, children: title })), subtitle && (_jsx("p", { style: {
                                                    fontSize: fontSize.xs,
                                                    color: colors.slate[500],
                                                    margin: 0,
                                                    marginTop: 2,
                                                }, children: subtitle }))] })] }), showCloseButton && (_jsx("button", { onClick: onClose, style: {
                                    width: 32,
                                    height: 32,
                                    borderRadius: borderRadius.full,
                                    backgroundColor: 'transparent',
                                    border: 'none',
                                    cursor: 'pointer',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    color: colors.slate[400],
                                    transition: transitions.fast,
                                }, onMouseEnter: (e) => {
                                    e.currentTarget.style.backgroundColor = colors.slate[100];
                                    e.currentTarget.style.color = colors.slate[600];
                                }, onMouseLeave: (e) => {
                                    e.currentTarget.style.backgroundColor = 'transparent';
                                    e.currentTarget.style.color = colors.slate[400];
                                }, children: _jsx("span", { style: { fontSize: 18 }, children: "\u00D7" }) }))] })), _jsx("div", { style: {
                            flex: 1,
                            overflow: 'auto',
                            padding: spacing[6],
                        }, children: children }), footer && (_jsx("div", { style: {
                            padding: `${spacing[4]}px ${spacing[6]}px`,
                            borderTop: `1px solid ${colors.slate[200]}`,
                            backgroundColor: 'rgba(255, 255, 255, 0.5)',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'flex-end',
                            gap: spacing[3],
                        }, children: footer }))] }), _jsx("style", { children: `
        @keyframes modalIn {
          from {
            opacity: 0;
            transform: scale(0.95) translateY(-10px);
          }
          to {
            opacity: 1;
            transform: scale(1) translateY(0);
          }
        }
      ` })] }));
};
export default Modal;
//# sourceMappingURL=Modal.js.map