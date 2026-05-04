/**
 * 隔离管理器实现
 * 隔离式上下文容器 + RBAC 访问控制
 */

import {
  IIsolationManager,
  IRBACManager,
  ContextContainer,
  IsolationAgentRole,
  Permission,
  PermissionLevel,
  ResourceType,
  AccessRequest,
  AccessDecision,
  IOValidationResult,
  IOViolation,
  RoleDefinition,
  DEFAULT_ROLES,
  PERMISSION_PRIORITY,
} from './types.js';
import { Message, getMessageText } from '../hooks/types.js';

export class IsolationManager implements IIsolationManager {
  private containers: Map<string, ContextContainer> = new Map();
  private agentPermissions: Map<string, Permission[]> = new Map();
  private rbacManager: RBACManager;
  private containerIdCounter = 0;

  constructor() {
    this.rbacManager = new RBACManager();
  }

  createContainer(agentId: string, role: IsolationAgentRole): ContextContainer {
    const id = `container_${++this.containerIdCounter}_${Date.now()}`;
    const roleDefinition = DEFAULT_ROLES[role];

    const container: ContextContainer = {
      id,
      agentId,
      role,
      messages: [],
      tokenCount: 0,
      createdAt: Date.now(),
      lastAccessedAt: Date.now(),
      metadata: {
        sessionId: `session_${Date.now()}`,
        permissions: roleDefinition.permissions,
        accessLog: [],
        resourceUsage: {
          tokensUsed: 0,
          tokensLimit: roleDefinition.maxContextTokens || 100000,
          toolCalls: 0,
          toolCallsLimit: 1000,
          fileAccesses: 0,
          fileAccessesLimit: 500,
        },
      },
      isolated: true,
    };

    this.containers.set(id, container);
    this.agentPermissions.set(agentId, roleDefinition.permissions);
    this.rbacManager.assignRole(agentId, role);

    return container;
  }

  getContainer(containerId: string): ContextContainer | null {
    const container = this.containers.get(containerId);
    if (container) {
      container.lastAccessedAt = Date.now();
    }
    return container || null;
  }

  deleteContainer(containerId: string): boolean {
    return this.containers.delete(containerId);
  }

  listContainers(agentId?: string): ContextContainer[] {
    const all = Array.from(this.containers.values());
    if (agentId) {
      return all.filter(c => c.agentId === agentId);
    }
    return all;
  }

  checkAccess(request: AccessRequest): AccessDecision {
    const permissions = this.agentPermissions.get(request.agentId) || [];

    // 查找匹配的权限
    const matchedPermission = permissions.find(p => {
      if (p.resource !== request.resource) return false;
      if (p.scope && p.scope !== request.resourceId) return false;
      return true;
    });

    if (!matchedPermission) {
      return {
        allowed: false,
        reason: `No permission found for resource: ${request.resource}`,
      };
    }

    // 检查权限级别
    const requiredLevel = this.actionToLevel(request.action);
    const hasLevel = PERMISSION_PRIORITY[matchedPermission.level] >= PERMISSION_PRIORITY[requiredLevel];

    if (!hasLevel) {
      return {
        allowed: false,
        reason: `Insufficient permission level: ${matchedPermission.level} < ${requiredLevel}`,
        matchedPermission,
      };
    }

    // 检查条件
    if (matchedPermission.conditions) {
      for (const condition of matchedPermission.conditions) {
        if (!this.evaluateCondition(condition, request.context)) {
          return {
            allowed: false,
            reason: `Condition not met: ${condition.type}`,
            matchedPermission,
            conditions: matchedPermission.conditions,
          };
        }
      }
    }

    // 记录访问日志
    this.logAccess(request, true);

    return {
      allowed: true,
      reason: 'Access granted',
      matchedPermission,
    };
  }

  grantPermission(agentId: string, permission: Permission): void {
    const permissions = this.agentPermissions.get(agentId) || [];

    // 移除同资源的旧权限
    const filtered = permissions.filter(p => p.resource !== permission.resource);
    filtered.push(permission);

    this.agentPermissions.set(agentId, filtered);
  }

  revokePermission(agentId: string, resource: ResourceType): void {
    const permissions = this.agentPermissions.get(agentId) || [];
    const filtered = permissions.filter(p => p.resource !== resource);
    this.agentPermissions.set(agentId, filtered);
  }

  isolateContext(containerId: string): void {
    const container = this.containers.get(containerId);
    if (container) {
      container.isolated = true;
      container.metadata.parentContainerId = undefined;
    }
  }

  mergeContext(
    sourceId: string,
    targetId: string,
    permissions: Permission[]
  ): boolean {
    const source = this.containers.get(sourceId);
    const target = this.containers.get(targetId);

    if (!source || !target) return false;

    // 检查目标容器是否有写权限
    const hasWriteAccess = permissions.some(
      p => p.resource === 'context' && PERMISSION_PRIORITY[p.level] >= PERMISSION_PRIORITY['write']
    );

    if (!hasWriteAccess) return false;

    // 合并消息（过滤敏感内容）
    const filteredMessages = source.messages.filter(m =>
      !this.containsSensitiveData(getMessageText(m.content))
    );

    target.messages.push(...filteredMessages);
    target.tokenCount += this.estimateTokens(filteredMessages);
    target.metadata.parentContainerId = sourceId;

    return true;
  }

  validateInput(input: unknown, schema?: Record<string, unknown>): IOValidationResult {
    const violations: IOViolation[] = [];
    let sanitizedValue = input;

    if (typeof input === 'string') {
      // 检查注入攻击
      if (this.detectInjection(input)) {
        violations.push({
          type: 'injection',
          field: 'input',
          message: 'Potential injection attack detected',
          severity: 'critical',
        });
        sanitizedValue = this.sanitizeString(input);
      }

      // 检查敏感数据
      if (this.containsSensitiveData(input)) {
        violations.push({
          type: 'sensitive',
          field: 'input',
          message: 'Sensitive data detected',
          severity: 'high',
        });
        sanitizedValue = this.maskSensitiveData(input);
      }

      // 检查长度溢出
      if (input.length > 1000000) {
        violations.push({
          type: 'overflow',
          field: 'input',
          message: 'Input exceeds maximum length',
          severity: 'medium',
        });
        sanitizedValue = (input as string).slice(0, 1000000);
      }
    }

    return {
      valid: violations.length === 0,
      sanitized: violations.length > 0,
      originalValue: input,
      sanitizedValue: violations.length > 0 ? sanitizedValue : undefined,
      violations,
    };
  }

  validateOutput(output: unknown, schema?: Record<string, unknown>): IOValidationResult {
    const violations: IOViolation[] = [];
    let sanitizedValue = output;

    if (typeof output === 'string') {
      // 检查敏感数据泄露
      if (this.containsSensitiveData(output)) {
        violations.push({
          type: 'sensitive',
          field: 'output',
          message: 'Sensitive data in output',
          severity: 'critical',
        });
        sanitizedValue = this.maskSensitiveData(output);
      }
    }

    return {
      valid: violations.length === 0,
      sanitized: violations.length > 0,
      originalValue: output,
      sanitizedValue: violations.length > 0 ? sanitizedValue : undefined,
      violations,
    };
  }

  // ==================== Private Methods ====================

  private actionToLevel(action: string): PermissionLevel {
    switch (action) {
      case 'read':
        return 'read';
      case 'write':
      case 'delete':
        return 'write';
      case 'execute':
        return 'admin';
      default:
        return 'read';
    }
  }

  private evaluateCondition(
    condition: { type: string; value: unknown; operator: string },
    context?: Record<string, unknown>
  ): boolean {
    if (!context) return true;

    switch (condition.type) {
      case 'time': {
        const now = Date.now();
        const value = condition.value as number;
        switch (condition.operator) {
          case 'lt':
            return now < value;
          case 'gt':
            return now > value;
          default:
            return true;
        }
      }
      case 'count': {
        const count = (context.count as number) || 0;
        const value = condition.value as number;
        switch (condition.operator) {
          case 'lt':
            return count < value;
          case 'gt':
            return count > value;
          default:
            return true;
        }
      }
      default:
        return true;
    }
  }

  private logAccess(request: AccessRequest, allowed: boolean): void {
    const containers = this.listContainers(request.agentId);
    for (const container of containers) {
      container.metadata.accessLog.push({
        timestamp: Date.now(),
        action: request.action,
        resource: request.resource,
        resourceId: request.resourceId,
        allowed,
      });

      // 限制日志大小
      if (container.metadata.accessLog.length > 1000) {
        container.metadata.accessLog = container.metadata.accessLog.slice(-500);
      }
    }
  }

  private detectInjection(input: string): boolean {
    const patterns = [
      /<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi,
      /javascript:/gi,
      /on\w+\s*=/gi,
      /\$\{.*\}/g,
      /`.*`/g,
    ];

    return patterns.some(p => p.test(input));
  }

  private containsSensitiveData(input: string): boolean {
    const patterns = [
      /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b/,
      /\b\d{3}[-.]?\d{3}[-.]?\d{4}\b/,
      /\b(?:sk-|pk-|api[_-]?key)[a-zA-Z0-9]{20,}\b/i,
      /\b(?:password|secret|token|key)\s*[:=]\s*\S+/i,
    ];

    return patterns.some(p => p.test(input));
  }

  private sanitizeString(input: string): string {
    return input
      .replace(/<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi, '')
      .replace(/javascript:/gi, '')
      .replace(/on\w+\s*=/gi, '');
  }

  private maskSensitiveData(input: string): string {
    return input
      .replace(/\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b/g, '[EMAIL]')
      .replace(/\b\d{3}[-.]?\d{3}[-.]?\d{4}\b/g, '[PHONE]')
      .replace(/\b(?:sk-|pk-|api[_-]?key)[a-zA-Z0-9]{20,}\b/gi, '[API_KEY]')
      .replace(/\b(?:password|secret|token|key)\s*[:=]\s*\S+/gi, '[REDACTED]');
  }

  private estimateTokens(messages: Message[]): number {
    return messages.reduce((sum, m) => sum + Math.ceil(getMessageText(m.content).length / 4), 0);
  }
}

/**
 * RBAC 管理器实现
 */
export class RBACManager implements IRBACManager {
  private roles: Map<string, RoleDefinition> = new Map();
  private agentRoles: Map<string, Set<string>> = new Map();

  constructor() {
    // 初始化默认角色
    for (const [name, definition] of Object.entries(DEFAULT_ROLES)) {
      this.roles.set(name, definition);
    }
  }

  defineRole(role: RoleDefinition): void {
    this.roles.set(role.name, role);
  }

  assignRole(agentId: string, roleName: string): void {
    if (!this.agentRoles.has(agentId)) {
      this.agentRoles.set(agentId, new Set());
    }
    this.agentRoles.get(agentId)!.add(roleName);
  }

  removeRole(agentId: string, roleName: string): void {
    this.agentRoles.get(agentId)?.delete(roleName);
  }

  getRoles(agentId: string): RoleDefinition[] {
    const roleNames = this.agentRoles.get(agentId) || new Set();
    const roles: RoleDefinition[] = [];

    for (const name of roleNames) {
      const role = this.roles.get(name);
      if (role) {
        roles.push(role);

        // 处理继承
        if (role.inherits) {
          for (const inheritedName of role.inherits) {
            const inherited = this.roles.get(inheritedName);
            if (inherited) {
              roles.push(inherited);
            }
          }
        }
      }
    }

    return roles;
  }

  hasPermission(
    agentId: string,
    resource: ResourceType,
    level: PermissionLevel
  ): boolean {
    const roles = this.getRoles(agentId);

    for (const role of roles) {
      const permission = role.permissions.find(p => p.resource === resource);
      if (permission && PERMISSION_PRIORITY[permission.level] >= PERMISSION_PRIORITY[level]) {
        return true;
      }
    }

    return false;
  }
}
