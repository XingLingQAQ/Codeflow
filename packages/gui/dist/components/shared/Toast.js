import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Toast - 通知提示组件
 * 支持 success/info/warning/error 类型，3 秒自动消失
 */
import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import { colors, spacing, borderRadius, fontSize, fontWeight, shadows, transitions, zIndex, keyframes, animations, } from './tokens';
const ToastContext = createContext(null);
export const useToast = () => {
    const context = useContext(ToastContext);
    if (!context) {
        throw new Error('useToast must be used within a ToastProvider');
    }
    return context;
};
// Icons for toast types
const CheckIcon = () => (_jsx("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: _jsx("polyline", { points: "20 6 9 17 4 12" }) }));
const InfoIcon = () => (_jsxs("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "10" }), _jsx("line", { x1: "12", y1: "16", x2: "12", y2: "12" }), _jsx("line", { x1: "12", y1: "8", x2: "12.01", y2: "8" })] }));
const WarningIcon = () => (_jsxs("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("path", { d: "M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" }), _jsx("line", { x1: "12", y1: "9", x2: "12", y2: "13" }), _jsx("line", { x1: "12", y1: "17", x2: "12.01", y2: "17" })] }));
const ErrorIcon = () => (_jsxs("svg", { width: "16", height: "16", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("circle", { cx: "12", cy: "12", r: "10" }), _jsx("line", { x1: "15", y1: "9", x2: "9", y2: "15" }), _jsx("line", { x1: "9", y1: "9", x2: "15", y2: "15" })] }));
const CloseIcon = () => (_jsxs("svg", { width: "14", height: "14", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("line", { x1: "18", y1: "6", x2: "6", y2: "18" }), _jsx("line", { x1: "6", y1: "6", x2: "18", y2: "18" })] }));
const getToastIcon = (type) => {
    switch (type) {
        case 'success':
            return _jsx(CheckIcon, {});
        case 'info':
            return _jsx(InfoIcon, {});
        case 'warning':
            return _jsx(WarningIcon, {});
        case 'error':
            return _jsx(ErrorIcon, {});
    }
};
const getToastColors = (type) => {
    switch (type) {
        case 'success':
            return {
                bg: colors.success.light,
                border: colors.success.main,
                icon: colors.success.dark,
                text: colors.success.dark,
            };
        case 'info':
            return {
                bg: colors.info.light,
                border: colors.info.main,
                icon: colors.info.dark,
                text: colors.info.dark,
            };
        case 'warning':
            return {
                bg: colors.warning.light,
                border: colors.warning.main,
                icon: colors.warning.dark,
                text: colors.warning.dark,
            };
        case 'error':
            return {
                bg: colors.error.light,
                border: colors.error.main,
                icon: colors.error.dark,
                text: colors.error.dark,
            };
    }
};
// Inject keyframes into document
const injectKeyframes = () => {
    const styleId = 'toast-keyframes';
    if (typeof document !== 'undefined' && !document.getElementById(styleId)) {
        const style = document.createElement('style');
        style.id = styleId;
        style.textContent = keyframes.slideDown + keyframes.fadeIn;
        document.head.appendChild(style);
    }
};
export const ToastItem = ({ toast, onClose }) => {
    const [isExiting, setIsExiting] = useState(false);
    const toastColors = getToastColors(toast.type);
    useEffect(() => {
        injectKeyframes();
    }, []);
    useEffect(() => {
        const duration = toast.duration || 3000;
        const timer = setTimeout(() => {
            setIsExiting(true);
            setTimeout(() => onClose(toast.id), 200);
        }, duration);
        return () => clearTimeout(timer);
    }, [toast.id, toast.duration, onClose]);
    const handleClose = useCallback(() => {
        setIsExiting(true);
        setTimeout(() => onClose(toast.id), 200);
    }, [toast.id, onClose]);
    return (_jsxs("div", { style: {
            display: 'flex',
            alignItems: 'center',
            gap: spacing[3],
            padding: `${spacing[3]}px ${spacing[4]}px`,
            backgroundColor: toastColors.bg,
            border: `1px solid ${toastColors.border}`,
            borderRadius: borderRadius.xl,
            boxShadow: shadows.lg,
            animation: isExiting ? 'fadeIn 0.2s ease reverse' : animations.slideDown,
            opacity: isExiting ? 0 : 1,
            transition: transitions.fast,
            minWidth: 280,
            maxWidth: 400,
        }, role: "alert", "aria-live": "polite", children: [_jsx("span", { style: { color: toastColors.icon, flexShrink: 0 }, children: getToastIcon(toast.type) }), _jsx("span", { style: {
                    flex: 1,
                    fontSize: fontSize.sm,
                    fontWeight: fontWeight.medium,
                    color: toastColors.text,
                }, children: toast.message }), _jsx("button", { onClick: handleClose, style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    padding: spacing[1],
                    backgroundColor: 'transparent',
                    border: 'none',
                    borderRadius: borderRadius.md,
                    cursor: 'pointer',
                    color: toastColors.icon,
                    opacity: 0.6,
                    transition: transitions.fast,
                    flexShrink: 0,
                }, onMouseEnter: (e) => (e.currentTarget.style.opacity = '1'), onMouseLeave: (e) => (e.currentTarget.style.opacity = '0.6'), "aria-label": "Close notification", children: _jsx(CloseIcon, {}) })] }));
};
const getPositionStyles = (position) => {
    const base = {
        position: 'fixed',
        zIndex: zIndex.toast,
        display: 'flex',
        flexDirection: 'column',
        gap: spacing[2],
        padding: spacing[4],
    };
    switch (position) {
        case 'top-left':
            return { ...base, top: 0, left: 0 };
        case 'top-center':
            return { ...base, top: 0, left: '50%', transform: 'translateX(-50%)' };
        case 'bottom-left':
            return { ...base, bottom: 0, left: 0 };
        case 'bottom-right':
            return { ...base, bottom: 0, right: 0 };
        case 'bottom-center':
            return { ...base, bottom: 0, left: '50%', transform: 'translateX(-50%)' };
        case 'top-right':
        default:
            return { ...base, top: 0, right: 0 };
    }
};
export const ToastContainer = ({ toasts, onClose, position = 'top-right', className, style, }) => {
    if (toasts.length === 0)
        return null;
    return (_jsx("div", { className: className, style: {
            ...getPositionStyles(position),
            ...style,
        }, "aria-label": "Notifications", children: toasts.map((toast) => (_jsx(ToastItem, { toast: toast, onClose: onClose }, toast.id))) }));
};
// Toast Provider for global state management
export const ToastProvider = ({ children }) => {
    const [toasts, setToasts] = useState([]);
    const addToast = useCallback((type, message, duration = 3000) => {
        const id = `toast-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
        setToasts((prev) => [...prev, { id, type, message, duration }]);
    }, []);
    const removeToast = useCallback((id) => {
        setToasts((prev) => prev.filter((t) => t.id !== id));
    }, []);
    return (_jsxs(ToastContext.Provider, { value: { toasts, addToast, removeToast }, children: [children, _jsx(ToastContainer, { toasts: toasts, onClose: removeToast })] }));
};
export default ToastContainer;
//# sourceMappingURL=Toast.js.map