/**
 * Avatar - 头像组件
 * 支持 size（sm/md/lg）和 stack 模式
 */
import React from 'react';
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
    avatars: Array<{
        src?: string;
        alt?: string;
        name?: string;
    }>;
    size?: AvatarSize;
    max?: number;
    className?: string;
    style?: React.CSSProperties;
}
export declare const Avatar: React.FC<AvatarProps>;
export declare const AvatarStack: React.FC<AvatarStackProps>;
export default Avatar;
//# sourceMappingURL=Avatar.d.ts.map