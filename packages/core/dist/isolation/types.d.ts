/**
 * 权限隔离类型定义
 * 隔离式上下文容器 + RBAC 访问控制
 */
import { Message } from '../hooks/types.js';
/**
 * 隔离 Agent 角色
 */
export type IsolationAgentRole = 'main' | 'coder' | 'reviewer' | 'planner' | 'custom';
/**
 * 权限级别
 */
export type PermissionLevel = 'none' | 'read' | 'write' | 'admin';
/**
 * 资源类型
 */
export type ResourceType = 'context' | 'memory' | 'file' | 'tool' | 'config' | 'secret';
/**
 * 权限定义
 */
export interface Permission {
    resource: ResourceType;
    level: PermissionLevel;
    scope?: string;
    conditions?: PermissionCondition[];
}
/**
 * 权限条件
 */
export interface PermissionCondition {
    type: 'time' | 'count' | 'pattern' | 'custom';
    value: unknown;
    operator: 'eq' | 'ne' | 'gt' | 'lt' | 'in' | 'match';
}
/**
 * 角色定义
 */
export interface RoleDefinition {
    name: string;
    permissions: Permission[];
    inherits?: string[];
    maxContextTokens?: number;
    allowedTools?: string[];
    deniedTools?: string[];
}
/**
 * 上下文容器
 */
export interface ContextContainer {
    id: string;
    agentId: string;
    role: IsolationAgentRole;
    messages: Message[];
    tokenCount: number;
    createdAt: number;
    lastAccessedAt: number;
    metadata: ContainerMetadata;
    isolated: boolean;
}
/**
 * 容器元数据
 */
export interface ContainerMetadata {
    parentContainerId?: string;
    sessionId: string;
    permissions: Permission[];
    accessLog: AccessLogEntry[];
    resourceUsage: ResourceUsage;
}
/**
 * 访问日志条目
 */
export interface AccessLogEntry {
    timestamp: number;
    action: 'read' | 'write' | 'delete' | 'execute';
    resource: ResourceType;
    resourceId: string;
    allowed: boolean;
    reason?: string;
}
/**
 * 资源使用统计
 */
export interface ResourceUsage {
    tokensUsed: number;
    tokensLimit: number;
    toolCalls: number;
    toolCallsLimit: number;
    fileAccesses: number;
    fileAccessesLimit: number;
}
/**
 * 访问请求
 */
export interface AccessRequest {
    agentId: string;
    resource: ResourceType;
    resourceId: string;
    action: 'read' | 'write' | 'delete' | 'execute';
    context?: Record<string, unknown>;
}
/**
 * 访问决策
 */
export interface AccessDecision {
    allowed: boolean;
    reason: string;
    matchedPermission?: Permission;
    conditions?: PermissionCondition[];
}
/**
 * I/O 验证结果
 */
export interface IOValidationResult {
    valid: boolean;
    sanitized: boolean;
    originalValue: unknown;
    sanitizedValue?: unknown;
    violations: IOViolation[];
}
/**
 * I/O 违规
 */
export interface IOViolation {
    type: 'injection' | 'overflow' | 'format' | 'forbidden' | 'sensitive';
    field: string;
    message: string;
    severity: 'low' | 'medium' | 'high' | 'critical';
}
/**
 * 隔离管理器接口
 */
export interface IIsolationManager {
    createContainer(agentId: string, role: IsolationAgentRole): ContextContainer;
    getContainer(containerId: string): ContextContainer | null;
    deleteContainer(containerId: string): boolean;
    listContainers(agentId?: string): ContextContainer[];
    checkAccess(request: AccessRequest): AccessDecision;
    grantPermission(agentId: string, permission: Permission): void;
    revokePermission(agentId: string, resource: ResourceType): void;
    isolateContext(containerId: string): void;
    mergeContext(sourceId: string, targetId: string, permissions: Permission[]): boolean;
    validateInput(input: unknown, schema?: Record<string, unknown>): IOValidationResult;
    validateOutput(output: unknown, schema?: Record<string, unknown>): IOValidationResult;
}
/**
 * RBAC 管理器接口
 */
export interface IRBACManager {
    defineRole(role: RoleDefinition): void;
    assignRole(agentId: string, roleName: string): void;
    removeRole(agentId: string, roleName: string): void;
    getRoles(agentId: string): RoleDefinition[];
    hasPermission(agentId: string, resource: ResourceType, level: PermissionLevel): boolean;
}
/**
 * 默认角色定义
 */
export declare const DEFAULT_ROLES: Record<IsolationAgentRole, RoleDefinition>;
/**
 * 权限级别优先级
 */
export declare const PERMISSION_PRIORITY: Record<PermissionLevel, number>;
//# sourceMappingURL=types.d.ts.map