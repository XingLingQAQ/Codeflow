import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * Avatar - 头像组件
 * 支持 size（sm/md/lg）和 stack 模式
 */
import React from 'react';
import { colors, borderRadius, fontSize, fontWeight, transitions, } from './tokens';
const getSizeStyles = (size) => {
    switch (size) {
        case 'sm':
            return {
                width: 24,
                height: 24,
                fontSize: fontSize.xs,
            };
        case 'lg':
            return {
                width: 48,
                height: 48,
                fontSize: fontSize.lg,
            };
        case 'md':
        default:
            return {
                width: 32,
                height: 32,
                fontSize: fontSize.sm,
            };
    }
};
const getInitials = (name) => {
    const parts = name.trim().split(/\s+/);
    if (parts.length === 1) {
        return parts[0].charAt(0).toUpperCase();
    }
    return (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase();
};
const getColorFromName = (name) => {
    const colorPalette = [
        colors.primary[500],
        colors.indigo[500],
        colors.success.main,
        colors.warning.main,
        colors.error.main,
        colors.info.main,
    ];
    let hash = 0;
    for (let i = 0; i < name.length; i++) {
        hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }
    return colorPalette[Math.abs(hash) % colorPalette.length];
};
export const Avatar = ({ src, alt, name, size = 'md', className, style, }) => {
    const sizeStyles = getSizeStyles(size);
    const [imageError, setImageError] = React.useState(false);
    const showFallback = !src || imageError;
    const displayName = name || alt || 'User';
    const initials = getInitials(displayName);
    const bgColor = getColorFromName(displayName);
    return (_jsx("div", { className: className, style: {
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: sizeStyles.width,
            height: sizeStyles.height,
            borderRadius: borderRadius.full,
            backgroundColor: showFallback ? bgColor : 'transparent',
            color: '#fff',
            fontSize: sizeStyles.fontSize,
            fontWeight: fontWeight.semibold,
            overflow: 'hidden',
            flexShrink: 0,
            boxShadow: `0 0 0 2px #fff`,
            transition: transitions.fast,
            ...style,
        }, role: "img", "aria-label": alt || name || 'Avatar', children: showFallback ? (_jsx("span", { children: initials })) : (_jsx("img", { src: src, alt: alt || name || 'Avatar', onError: () => setImageError(true), style: {
                width: '100%',
                height: '100%',
                objectFit: 'cover',
            } })) }));
};
export const AvatarStack = ({ avatars, size = 'md', max = 3, className, style, }) => {
    const sizeStyles = getSizeStyles(size);
    const visibleAvatars = avatars.slice(0, max);
    const remainingCount = avatars.length - max;
    const overlap = sizeStyles.width * 0.3;
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            alignItems: 'center',
            ...style,
        }, children: [visibleAvatars.map((avatar, index) => (_jsx("div", { style: {
                    marginLeft: index === 0 ? 0 : -overlap,
                    zIndex: visibleAvatars.length - index,
                    position: 'relative',
                }, children: _jsx(Avatar, { src: avatar.src, alt: avatar.alt, name: avatar.name, size: size }) }, index))), remainingCount > 0 && (_jsxs("div", { style: {
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: sizeStyles.width,
                    height: sizeStyles.height,
                    borderRadius: borderRadius.full,
                    backgroundColor: colors.slate[200],
                    color: colors.slate[600],
                    fontSize: sizeStyles.fontSize,
                    fontWeight: fontWeight.semibold,
                    marginLeft: -overlap,
                    zIndex: 0,
                    boxShadow: `0 0 0 2px #fff`,
                }, "aria-label": `${remainingCount} more`, children: ["+", remainingCount] }))] }));
};
export default Avatar;
//# sourceMappingURL=Avatar.js.map