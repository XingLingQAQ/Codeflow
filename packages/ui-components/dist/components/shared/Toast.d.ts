/**
 * Toast - 通知提示组件
 * 支持 success/info/warning/error 类型，3 秒自动消失
 */
import React from 'react';
export type ToastType = 'success' | 'info' | 'warning' | 'error';
export interface Toast {
    id: string;
    type: ToastType;
    message: string;
    duration?: number;
}
export interface ToastProps {
    toast: Toast;
    onClose: (id: string) => void;
}
export interface ToastContainerProps {
    toasts: Toast[];
    onClose: (id: string) => void;
    position?: 'top-right' | 'top-left' | 'bottom-right' | 'bottom-left' | 'top-center' | 'bottom-center';
    className?: string;
    style?: React.CSSProperties;
}
interface ToastContextValue {
    toasts: Toast[];
    addToast: (type: ToastType, message: string, duration?: number) => void;
    removeToast: (id: string) => void;
}
export declare const useToast: () => ToastContextValue;
export declare const ToastItem: React.FC<ToastProps>;
export declare const ToastContainer: React.FC<ToastContainerProps>;
export declare const ToastProvider: React.FC<{
    children: React.ReactNode;
}>;
export default ToastContainer;
//# sourceMappingURL=Toast.d.ts.map