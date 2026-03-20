import { describe, it, expect, beforeEach } from 'vitest';
import { IsolationManager, RBACManager } from '../IsolationManager.js';
import {
  IsolationAgentRole,
  Permission,
  AccessRequest,
  DEFAULT_ROLES,
  PERMISSION_PRIORITY,
} from '../types.js';

describe('IsolationManager', () => {
  let manager: IsolationManager;

  beforeEach(() => {
    manager = new IsolationManager();
  });

  describe('createContainer', () => {
    it('should create container for main role', () => {
      const container = manager.createContainer('agent-1', 'main');

      expect(container).toBeDefined();
      expect(container.agentId).toBe('agent-1');
      expect(container.role).toBe('main');
      expect(container.isolated).toBe(true);
    });

    it('should create container for coder role', () => {
      const container = manager.createContainer('agent-2', 'coder');

      expect(container.role).toBe('coder');
      expect(container.metadata.permissions).toBeDefined();
    });

    it('should create container for reviewer role', () => {
      const container = manager.createContainer('agent-3', 'reviewer');

      expect(container.role).toBe('reviewer');
    });

    it('should create container for planner role', () => {
      const container = manager.createContainer('agent-4', 'planner');

      expect(container.role).toBe('planner');
    });

    it('should create container for custom role', () => {
      const container = manager.createContainer('agent-5', 'custom');

      expect(container.role).toBe('custom');
    });

    it('should generate unique container ids', () => {
      const c1 = manager.createContainer('agent-1', 'main');
      const c2 = manager.createContainer('agent-2', 'main');

      expect(c1.id).not.toBe(c2.id);
    });

    it('should initialize container with empty messages', () => {
      const container = manager.createContainer('agent-1', 'main');

      expect(container.messages).toEqual([]);
      expect(container.tokenCount).toBe(0);
    });

    it('should set timestamps', () => {
      const before = Date.now();
      const container = manager.createContainer('agent-1', 'main');
      const after = Date.now();

      expect(container.createdAt).toBeGreaterThanOrEqual(before);
      expect(container.createdAt).toBeLessThanOrEqual(after);
      expect(container.lastAccessedAt).toBe(container.createdAt);
    });

    it('should initialize resource usage limits', () => {
      const container = manager.createContainer('agent-1', 'main');

      expect(container.metadata.resourceUsage.tokensUsed).toBe(0);
      expect(container.metadata.resourceUsage.tokensLimit).toBeGreaterThan(0);
    });
  });

  describe('getContainer', () => {
    it('should return null for non-existent container', () => {
      const container = manager.getContainer('non-existent');
      expect(container).toBeNull();
    });

    it('should return existing container', () => {
      const created = manager.createContainer('agent-1', 'main');
      const retrieved = manager.getContainer(created.id);

      expect(retrieved).toEqual(created);
    });

    it('should update lastAccessedAt on retrieval', async () => {
      const container = manager.createContainer('agent-1', 'main');
      const originalTime = container.lastAccessedAt;

      await new Promise(resolve => setTimeout(resolve, 10));

      manager.getContainer(container.id);

      expect(container.lastAccessedAt).toBeGreaterThan(originalTime);
    });
  });

  describe('deleteContainer', () => {
    it('should delete existing container', () => {
      const container = manager.createContainer('agent-1', 'main');

      const result = manager.deleteContainer(container.id);

      expect(result).toBe(true);
      expect(manager.getContainer(container.id)).toBeNull();
    });

    it('should return false for non-existent container', () => {
      const result = manager.deleteContainer('non-existent');
      expect(result).toBe(false);
    });
  });

  describe('listContainers', () => {
    it('should return empty array when no containers', () => {
      const containers = manager.listContainers();
      expect(containers).toEqual([]);
    });

    it('should return all containers', () => {
      manager.createContainer('agent-1', 'main');
      manager.createContainer('agent-2', 'coder');

      const containers = manager.listContainers();

      expect(containers.length).toBe(2);
    });

    it('should filter by agentId', () => {
      manager.createContainer('agent-1', 'main');
      manager.createContainer('agent-1', 'coder');
      manager.createContainer('agent-2', 'main');

      const containers = manager.listContainers('agent-1');

      expect(containers.length).toBe(2);
      expect(containers.every(c => c.agentId === 'agent-1')).toBe(true);
    });
  });

  describe('checkAccess', () => {
    beforeEach(() => {
      manager.createContainer('agent-1', 'main');
      manager.createContainer('agent-2', 'reviewer');
    });

    it('should allow access for valid permission', () => {
      const request: AccessRequest = {
        agentId: 'agent-1',
        resource: 'context',
        resourceId: 'ctx-1',
        action: 'read',
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(true);
    });

    it('should deny access for missing permission', () => {
      const request: AccessRequest = {
        agentId: 'unknown-agent',
        resource: 'context',
        resourceId: 'ctx-1',
        action: 'read',
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(false);
      expect(decision.reason).toContain('No permission');
    });

    it('should deny access for insufficient level', () => {
      const request: AccessRequest = {
        agentId: 'agent-2', // reviewer has read-only
        resource: 'context',
        resourceId: 'ctx-1',
        action: 'write',
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(false);
      expect(decision.reason).toContain('Insufficient permission');
    });

    it('should allow admin to perform any action', () => {
      const request: AccessRequest = {
        agentId: 'agent-1', // main has admin
        resource: 'context',
        resourceId: 'ctx-1',
        action: 'execute',
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(true);
    });

    it('should evaluate time conditions', () => {
      manager.grantPermission('agent-3', {
        resource: 'context',
        level: 'read',
        conditions: [
          { type: 'time', value: Date.now() + 10000, operator: 'lt' },
        ],
      });

      const request: AccessRequest = {
        agentId: 'agent-3',
        resource: 'context',
        resourceId: 'ctx-1',
        action: 'read',
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(true);
    });

    it('should deny when time condition not met', () => {
      // Grant permission with a time condition that requires time > future timestamp
      // Since current time is less than future, condition 'gt' will fail
      manager.grantPermission('agent-3', {
        resource: 'context',
        level: 'read',
        conditions: [
          { type: 'time', value: Date.now() + 100000, operator: 'gt' }, // requires time > future
        ],
      });

      const request: AccessRequest = {
        agentId: 'agent-3',
        resource: 'context',
        resourceId: 'ctx-1',
        action: 'read',
        context: {}, // Must provide context for condition evaluation
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(false);
    });

    it('should evaluate count conditions', () => {
      manager.grantPermission('agent-3', {
        resource: 'file',
        level: 'read',
        conditions: [
          { type: 'count', value: 100, operator: 'lt' },
        ],
      });

      const request: AccessRequest = {
        agentId: 'agent-3',
        resource: 'file',
        resourceId: 'file-1',
        action: 'read',
        context: { count: 50 },
      };

      const decision = manager.checkAccess(request);

      expect(decision.allowed).toBe(true);
    });
  });

  describe('grantPermission', () => {
    it('should grant new permission', () => {
      manager.createContainer('agent-1', 'custom');

      manager.grantPermission('agent-1', {
        resource: 'file',
        level: 'write',
      });

      const request: AccessRequest = {
        agentId: 'agent-1',
        resource: 'file',
        resourceId: 'file-1',
        action: 'write',
      };

      const decision = manager.checkAccess(request);
      expect(decision.allowed).toBe(true);
    });

    it('should replace existing permission for same resource', () => {
      manager.createContainer('agent-1', 'custom');

      manager.grantPermission('agent-1', {
        resource: 'file',
        level: 'read',
      });

      manager.grantPermission('agent-1', {
        resource: 'file',
        level: 'admin',
      });

      const request: AccessRequest = {
        agentId: 'agent-1',
        resource: 'file',
        resourceId: 'file-1',
        action: 'execute',
      };

      const decision = manager.checkAccess(request);
      expect(decision.allowed).toBe(true);
    });
  });

  describe('revokePermission', () => {
    it('should revoke permission', () => {
      manager.createContainer('agent-1', 'main');

      manager.revokePermission('agent-1', 'file');

      const request: AccessRequest = {
        agentId: 'agent-1',
        resource: 'file',
        resourceId: 'file-1',
        action: 'read',
      };

      const decision = manager.checkAccess(request);
      expect(decision.allowed).toBe(false);
    });
  });

  describe('isolateContext', () => {
    it('should isolate container', () => {
      const container = manager.createContainer('agent-1', 'main');
      container.isolated = false;
      container.metadata.parentContainerId = 'parent-1';

      manager.isolateContext(container.id);

      expect(container.isolated).toBe(true);
      expect(container.metadata.parentContainerId).toBeUndefined();
    });

    it('should handle non-existent container', () => {
      // Should not throw
      expect(() => manager.isolateContext('non-existent')).not.toThrow();
    });
  });

  describe('mergeContext', () => {
    it('should merge contexts with write permission', () => {
      const source = manager.createContainer('agent-1', 'main');
      const target = manager.createContainer('agent-2', 'main');

      source.messages.push({
        role: 'user',
        content: 'Hello',
        timestamp: Date.now(),
      });

      const permissions: Permission[] = [
        { resource: 'context', level: 'write' },
      ];

      const result = manager.mergeContext(source.id, target.id, permissions);

      expect(result).toBe(true);
      expect(target.messages.length).toBe(1);
      expect(target.metadata.parentContainerId).toBe(source.id);
    });

    it('should fail without write permission', () => {
      const source = manager.createContainer('agent-1', 'main');
      const target = manager.createContainer('agent-2', 'main');

      const permissions: Permission[] = [
        { resource: 'context', level: 'read' },
      ];

      const result = manager.mergeContext(source.id, target.id, permissions);

      expect(result).toBe(false);
    });

    it('should fail for non-existent source', () => {
      const target = manager.createContainer('agent-2', 'main');

      const result = manager.mergeContext('non-existent', target.id, []);

      expect(result).toBe(false);
    });

    it('should fail for non-existent target', () => {
      const source = manager.createContainer('agent-1', 'main');

      const result = manager.mergeContext(source.id, 'non-existent', []);

      expect(result).toBe(false);
    });

    it('should filter sensitive data during merge', () => {
      const source = manager.createContainer('agent-1', 'main');
      const target = manager.createContainer('agent-2', 'main');

      source.messages.push({
        role: 'user',
        content: 'My email is test@example.com',
        timestamp: Date.now(),
      });

      const permissions: Permission[] = [
        { resource: 'context', level: 'write' },
      ];

      manager.mergeContext(source.id, target.id, permissions);

      // Sensitive message should be filtered out
      expect(target.messages.length).toBe(0);
    });
  });

  describe('validateInput', () => {
    it('should pass valid input', () => {
      const result = manager.validateInput('Hello world');

      expect(result.valid).toBe(true);
      expect(result.sanitized).toBe(false);
      expect(result.violations).toEqual([]);
    });

    it('should detect script injection', () => {
      const result = manager.validateInput('<script>alert("xss")</script>');

      expect(result.valid).toBe(false);
      expect(result.sanitized).toBe(true);
      expect(result.violations.some(v => v.type === 'injection')).toBe(true);
    });

    it('should detect javascript: protocol', () => {
      const result = manager.validateInput('javascript:alert(1)');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'injection')).toBe(true);
    });

    it('should detect event handlers', () => {
      const result = manager.validateInput('<img onerror=alert(1)>');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'injection')).toBe(true);
    });

    it('should detect template literals', () => {
      const result = manager.validateInput('${process.env.SECRET}');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'injection')).toBe(true);
    });

    it('should detect email addresses', () => {
      const result = manager.validateInput('Contact me at user@example.com');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'sensitive')).toBe(true);
    });

    it('should detect phone numbers', () => {
      const result = manager.validateInput('Call me at 123-456-7890');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'sensitive')).toBe(true);
    });

    it('should detect API keys', () => {
      const result = manager.validateInput('sk-abcdefghijklmnopqrstuvwxyz');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'sensitive')).toBe(true);
    });

    it('should detect password patterns', () => {
      const result = manager.validateInput('password: mysecret123');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'sensitive')).toBe(true);
    });

    it('should detect overflow', () => {
      const longInput = 'A'.repeat(1000001);
      const result = manager.validateInput(longInput);

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'overflow')).toBe(true);
      expect((result.sanitizedValue as string).length).toBe(1000000);
    });

    it('should sanitize injection attacks', () => {
      const result = manager.validateInput('<script>bad</script>');

      expect(result.sanitizedValue).not.toContain('<script>');
    });

    it('should mask sensitive data', () => {
      const result = manager.validateInput('Email: test@example.com');

      expect(result.sanitizedValue).toContain('[EMAIL]');
    });
  });

  describe('validateOutput', () => {
    it('should pass valid output', () => {
      const result = manager.validateOutput('Normal response');

      expect(result.valid).toBe(true);
      expect(result.sanitized).toBe(false);
    });

    it('should detect sensitive data in output', () => {
      const result = manager.validateOutput('Your API key is sk-abcdefghijklmnopqrstuvwxyz');

      expect(result.valid).toBe(false);
      expect(result.violations.some(v => v.type === 'sensitive')).toBe(true);
      expect(result.violations[0].severity).toBe('critical');
    });

    it('should mask sensitive data in output', () => {
      const result = manager.validateOutput('Contact: user@example.com');

      expect(result.sanitizedValue).toContain('[EMAIL]');
    });
  });
});

describe('RBACManager', () => {
  let rbac: RBACManager;

  beforeEach(() => {
    rbac = new RBACManager();
  });

  describe('constructor', () => {
    it('should initialize with default roles', () => {
      const roles = rbac.getRoles('test-agent');
      // No roles assigned yet
      expect(roles).toEqual([]);
    });
  });

  describe('defineRole', () => {
    it('should define custom role', () => {
      rbac.defineRole({
        name: 'custom-role',
        permissions: [
          { resource: 'file', level: 'read' },
        ],
      });

      rbac.assignRole('agent-1', 'custom-role');
      const roles = rbac.getRoles('agent-1');

      expect(roles.some(r => r.name === 'custom-role')).toBe(true);
    });
  });

  describe('assignRole', () => {
    it('should assign role to agent', () => {
      rbac.assignRole('agent-1', 'main');

      const roles = rbac.getRoles('agent-1');
      expect(roles.some(r => r.name === 'main')).toBe(true);
    });

    it('should allow multiple roles', () => {
      rbac.assignRole('agent-1', 'main');
      rbac.assignRole('agent-1', 'coder');

      const roles = rbac.getRoles('agent-1');
      expect(roles.length).toBe(2);
    });
  });

  describe('removeRole', () => {
    it('should remove role from agent', () => {
      rbac.assignRole('agent-1', 'main');
      rbac.assignRole('agent-1', 'coder');

      rbac.removeRole('agent-1', 'main');

      const roles = rbac.getRoles('agent-1');
      expect(roles.some(r => r.name === 'main')).toBe(false);
      expect(roles.some(r => r.name === 'coder')).toBe(true);
    });

    it('should handle removing non-existent role', () => {
      expect(() => rbac.removeRole('agent-1', 'non-existent')).not.toThrow();
    });
  });

  describe('getRoles', () => {
    it('should return empty array for unknown agent', () => {
      const roles = rbac.getRoles('unknown');
      expect(roles).toEqual([]);
    });

    it('should include inherited roles', () => {
      rbac.defineRole({
        name: 'child-role',
        permissions: [{ resource: 'file', level: 'read' }],
        inherits: ['main'],
      });

      rbac.assignRole('agent-1', 'child-role');

      const roles = rbac.getRoles('agent-1');

      // Should include both child-role and inherited main
      expect(roles.length).toBe(2);
    });
  });

  describe('hasPermission', () => {
    beforeEach(() => {
      rbac.assignRole('agent-1', 'main');
      rbac.assignRole('agent-2', 'reviewer');
    });

    it('should return true for valid permission', () => {
      expect(rbac.hasPermission('agent-1', 'context', 'admin')).toBe(true);
    });

    it('should return true for lower level permission', () => {
      expect(rbac.hasPermission('agent-1', 'context', 'read')).toBe(true);
    });

    it('should return false for insufficient level', () => {
      expect(rbac.hasPermission('agent-2', 'context', 'write')).toBe(false);
    });

    it('should return false for unknown agent', () => {
      expect(rbac.hasPermission('unknown', 'context', 'read')).toBe(false);
    });

    it('should check inherited permissions', () => {
      rbac.defineRole({
        name: 'child-role',
        permissions: [],
        inherits: ['main'],
      });

      rbac.assignRole('agent-3', 'child-role');

      expect(rbac.hasPermission('agent-3', 'context', 'admin')).toBe(true);
    });
  });
});
