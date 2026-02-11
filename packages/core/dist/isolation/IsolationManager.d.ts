/**
 * 隔离管理器实现
 * 隔离式上下文容器 + RBAC 访问控制
 */
import { IIsolationManager, IRBACManager, ContextContainer, IsolationAgentRole, Permission, PermissionLevel, ResourceType, AccessRequest, AccessDecision, IOValidationResult, RoleDefinition } from './types.js';
export declare class IsolationManager implements IIsolationManager {
    private containers;
    private agentPermissions;
    private rbacManager;
    private containerIdCounter;
    constructor();
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
    private actionToLevel;
    private evaluateCondition;
    private logAccess;
    private detectInjection;
    private containsSensitiveData;
    private sanitizeString;
    private maskSensitiveData;
    private estimateTokens;
}
/**
 * RBAC 管理器实现
 */
export declare class RBACManager implements IRBACManager {
    private roles;
    private agentRoles;
    constructor();
    defineRole(role: RoleDefinition): void;
    assignRole(agentId: string, roleName: string): void;
    removeRole(agentId: string, roleName: string): void;
    getRoles(agentId: string): RoleDefinition[];
    hasPermission(agentId: string, resource: ResourceType, level: PermissionLevel): boolean;
}
//# sourceMappingURL=IsolationManager.d.ts.map