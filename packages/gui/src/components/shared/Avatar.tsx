/**
 * Avatar - 头像组件
 * 支持 size（sm/md/lg）和 stack 模式
 */

import React from 'react';
import {
  colors,
  spacing,
  borderRadius,
  fontSize,
  fontWeight,
  shadows,
  transitions,
} from './tokens';

export type AvatarSize = 'sm' | 'md' | 'lg';

export interface AvatarProps {
  src?: string;
  alt?: string;
  name?: string;
  size?: AvatarSize;
  className?: string;
  style?: React.CSSProperties;
}

export interface AvatarStackProps {
  avatars: Array<{ src?: string; alt?: string; name?: string }>;
  size?: AvatarSize;
  max?: number;
  className?: string;
  style?: React.CSSProperties;
}

const getSizeStyles = (size: AvatarSize) => {
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

const getInitials = (name: string): string => {
  const parts = name.trim().split(/\s+/);
  if (parts.length === 1) {
    return parts[0].charAt(0).toUpperCase();
  }
  return (parts[0].charAt(0) + parts[parts.length - 1].charAt(0)).toUpperCase();
};

const getColorFromName = (name: string): string => {
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

export const Avatar: React.FC<AvatarProps> = ({
  src,
  alt,
  name,
  size = 'md',
  className,
  style,
}) => {
  const sizeStyles = getSizeStyles(size);
  const [imageError, setImageError] = React.useState(false);

  const showFallback = !src || imageError;
  const displayName = name || alt || 'User';
  const initials = getInitials(displayName);
  const bgColor = getColorFromName(displayName);

  return (
    <div
      className={className}
      style={{
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
      }}
      role="img"
      aria-label={alt || name || 'Avatar'}
    >
      {showFallback ? (
        <span>{initials}</span>
      ) : (
        <img
          src={src}
          alt={alt || name || 'Avatar'}
          onError={() => setImageError(true)}
          style={{
            width: '100%',
            height: '100%',
            objectFit: 'cover',
          }}
        />
      )}
    </div>
  );
};

export const AvatarStack: React.FC<AvatarStackProps> = ({
  avatars,
  size = 'md',
  max = 3,
  className,
  style,
}) => {
  const sizeStyles = getSizeStyles(size);
  const visibleAvatars = avatars.slice(0, max);
  const remainingCount = avatars.length - max;
  const overlap = sizeStyles.width * 0.3;

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        alignItems: 'center',
        ...style,
      }}
    >
      {visibleAvatars.map((avatar, index) => (
        <div
          key={index}
          style={{
            marginLeft: index === 0 ? 0 : -overlap,
            zIndex: visibleAvatars.length - index,
            position: 'relative',
          }}
        >
          <Avatar
            src={avatar.src}
            alt={avatar.alt}
            name={avatar.name}
            size={size}
          />
        </div>
      ))}
      {remainingCount > 0 && (
        <div
          style={{
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
          }}
          aria-label={`${remainingCount} more`}
        >
          +{remainingCount}
        </div>
      )}
    </div>
  );
};

export default Avatar;
