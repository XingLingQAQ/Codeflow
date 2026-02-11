/**
 * 权限隔离类型定义
 * 隔离式上下文容器 + RBAC 访问控制
 */
/**
 * 默认角色定义
 */
export const DEFAULT_ROLES = {
    main: {
        name: 'main',
        permissions: [
            { resource: 'context', level: 'admin' },
            { resource: 'memory', level: 'admin' },
            { resource: 'file', level: 'write' },
            { resource: 'tool', level: 'admin' },
            { resource: 'config', level: 'read' },
            { resource: 'secret', level: 'none' },
        ],
        maxContextTokens: 200000,
    },
    coder: {
        name: 'coder',
        permissions: [
            { resource: 'context', level: 'write' },
            { resource: 'memory', level: 'read' },
            { resource: 'file', level: 'write' },
            { resource: 'tool', level: 'write' },
            { resource: 'config', level: 'read' },
            { resource: 'secret', level: 'none' },
        ],
        maxContextTokens: 100000,
        allowedTools: ['read_file', 'write_file', 'execute_command', 'search'],
    },
    reviewer: {
        name: 'reviewer',
        permissions: [
            { resource: 'context', level: 'read' },
            { resource: 'memory', level: 'read' },
            { resource: 'file', level: 'read' },
            { resource: 'tool', level: 'read' },
            { resource: 'config', level: 'read' },
            { resource: 'secret', level: 'none' },
        ],
        maxContextTokens: 50000,
        allowedTools: ['read_file', 'search'],
    },
    planner: {
        name: 'planner',
        permissions: [
            { resource: 'context', level: 'write' },
            { resource: 'memory', level: 'write' },
            { resource: 'file', level: 'read' },
            { resource: 'tool', level: 'read' },
            { resource: 'config', level: 'read' },
            { resource: 'secret', level: 'none' },
        ],
        maxContextTokens: 80000,
    },
    custom: {
        name: 'custom',
        permissions: [
            { resource: 'context', level: 'read' },
            { resource: 'memory', level: 'none' },
            { resource: 'file', level: 'none' },
            { resource: 'tool', level: 'none' },
            { resource: 'config', level: 'none' },
            { resource: 'secret', level: 'none' },
        ],
        maxContextTokens: 10000,
    },
};
/**
 * 权限级别优先级
 */
export const PERMISSION_PRIORITY = {
    none: 0,
    read: 1,
    write: 2,
    admin: 3,
};
//# sourceMappingURL=types.js.map