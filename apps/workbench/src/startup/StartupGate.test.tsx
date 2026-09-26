// @vitest-environment jsdom
// FU for the StartupGate capability rendering (T0.12.c).
//
// The gate must release the shell for the read-only surface even when execution
// and merge are unavailable, must keep blocking only when read_only is
// unusable, and must never present a dependency failure as "offline". A later
// check after a dependency was repaired updates the strip with no restart.
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import type { Readiness, ReadinessCapabilitiesView } from '../services-bridge/readiness';

// React 19 的 act 需要显式声明 act 环境
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const fetchReadiness = vi.fn<() => Promise<Readiness>>();
vi.mock('../services-bridge/readiness', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services-bridge/readiness')>();
  return { ...actual, fetchReadiness: () => fetchReadiness() };
});

import { StartupGate } from './StartupGate';

const CHILD_TEXT = 'shell-child-visible';

let container: HTMLDivElement;
let root: Root;

function readyCapability() {
  return { state: 'ready' as const, blocking: [] };
}

/** An execution capability set (with its backend map) that is unavailable. */
function unavailableExecution(component: string, remediation: string) {
  return {
    ...unavailable(component, remediation),
    backends: { codex: unavailable(component, remediation) },
  };
}

function unavailable(component: string, remediation: string) {
  return {
    state: 'unavailable' as const,
    blocking: [{ component, state: 'not_configured' as const, error_code: remediation, remediation }],
  };
}

function capabilities(overrides: Partial<ReadinessCapabilitiesView> = {}): ReadinessCapabilitiesView {
  return {
    read_only: readyCapability(),
    execution: { ...readyCapability(), backends: { codex: readyCapability() } },
    merge: readyCapability(),
    ...overrides,
  };
}

function readiness(overrides: Partial<Readiness> = {}): Readiness {
  return {
    reachable: true,
    status: 'ready',
    components: {
      planner: { ready: true, required: true },
      project: { ready: true, required: true },
      context: { ready: true, required: true },
    },
    capabilities: capabilities(),
    ...overrides,
  };
}

/** Mount the gate and let the initial check settle. */
async function mountGate() {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => {
    root.render(
      <StartupGate>
        <div>{CHILD_TEXT}</div>
      </StartupGate>,
    );
  });
  // The check itself is async (fetchReadiness mock); flush its continuation.
  await act(async () => {
    await Promise.resolve();
  });
}

function text(): string {
  return container.textContent ?? '';
}

function strip() {
  return container.querySelector('[data-capability-strip]');
}

function capabilityState(key: string): string | null {
  return container.querySelector(`[data-capability="${key}"]`)?.getAttribute('data-state') ?? null;
}

beforeAll(() => {
  // jsdom lacks matchMedia; the gate's dependencies do not use it but motion does.
  if (!window.matchMedia) {
    // @ts-expect-error minimal stub for the jsdom environment
    window.matchMedia = () => ({
      matches: false,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    });
  }
});

afterEach(() => {
  if (root) act(() => root.unmount());
  container?.remove();
  fetchReadiness.mockReset();
});

describe('StartupGateCapabilityRendering', () => {
  it('BrowseOnlyReleasesShell: read_only ready + execution unavailable → 子树可见、显示原因、不出现 offline', async () => {
    fetchReadiness.mockResolvedValue(
      readiness({
        capabilities: capabilities({
          execution: {
            ...unavailable('vault', 'vault_locked'),
            backends: { codex: unavailable('exec_backend:codex', 'backend_not_installed') },
          },
          merge: unavailable('workspace', 'workspace_roots_unconfigured'),
        }),
      }),
    );
    await mountGate();

    // The read-only surface opened: the shell subtree is rendered.
    expect(text()).toContain(CHILD_TEXT);
    const bar = strip();
    expect(bar).not.toBeNull();
    // Capability states, not a single "offline".
    expect(capabilityState('browse')).toBe('ready');
    expect(capabilityState('run')).toBe('unavailable');
    expect(capabilityState('merge')).toBe('unavailable');
    expect(text()).not.toContain('连接后端失败');
    expect(text()).not.toContain('以离线模式继续');
    // A locked vault is its own state, not a generic "unavailable".
    expect(bar?.getAttribute('data-capability-strip')).toBe('locked');
    expect(text()).toContain('可浏览');
    expect(text()).toContain('可创建 Run');
    expect(text()).toContain('可合入');
    // The blocking reason is available without covering the workspace.
    const reasonButton = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').includes('原因'),
    );
    expect(reasonButton).toBeTruthy();
    await act(async () => {
      reasonButton!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    expect(text()).toContain('密钥库已锁定');
    expect(text()).toContain('执行后端尚未安装或未接线');
  });

  it('BrowseOnlyNoVaultLock: 非锁定依赖失败显示 browse_only 且列出原因', async () => {
    fetchReadiness.mockResolvedValue(
      readiness({
        capabilities: capabilities({
          execution: {
            ...unavailable('migrations', 'migrations_pending'),
            backends: { codex: unavailable('exec_backend:codex', 'backend_not_installed') },
          },
          merge: unavailable('migrations', 'migrations_pending'),
        }),
      }),
    );
    await mountGate();

    // The read-only surface opened: the shell subtree is rendered.
    expect(text()).toContain(CHILD_TEXT);
    const bar = strip();
    expect(bar).not.toBeNull();
    // Capability states, not a single "offline".
    expect(capabilityState('browse')).toBe('ready');
    expect(capabilityState('run')).toBe('unavailable');
    expect(capabilityState('merge')).toBe('unavailable');
    expect(text()).not.toContain('连接后端失败');
    expect(text()).not.toContain('以离线模式继续');
    expect(bar?.getAttribute('data-capability-strip')).toBe('browse_only');
    expect(text()).toContain('仅可浏览');

    // The blocking reason is available without covering the workspace.
    const reasonButton = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').includes('原因'),
    );
    expect(reasonButton).toBeTruthy();
    await act(async () => {
      reasonButton!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    expect(text()).toContain('运行时数据库迁移尚未完成');
    expect(text()).toContain('执行后端尚未安装或未接线');
  });

  it('AllReadyShowsThreeUsable: 全部就绪时三项均显示可用', async () => {
    fetchReadiness.mockResolvedValue(readiness());
    await mountGate();

    expect(text()).toContain(CHILD_TEXT);
    expect(capabilityState('browse')).toBe('ready');
    expect(capabilityState('run')).toBe('ready');
    expect(capabilityState('merge')).toBe('ready');
    expect(text()).toContain('可浏览');
    expect(text()).toContain('可创建 Run');
    expect(text()).toContain('可合入');
  });

  it('ReadOnlyUnavailableBlocks: read_only 不可用时子树不渲染且不显示能力条', async () => {
    fetchReadiness.mockResolvedValue(
      readiness({
        status: 'not_ready',
        capabilities: capabilities({
          read_only: unavailable('memory', 'dependency_not_ready'),
          execution: unavailableExecution('memory', 'dependency_not_ready'),
          merge: unavailable('memory', 'dependency_not_ready'),
        }),
      }),
    );
    await mountGate();

    expect(text()).not.toContain(CHILD_TEXT);
    expect(strip()).toBeNull();
    // Still not "offline": the backend answered.
    expect(text()).not.toContain('以离线模式继续');
  });

  it('MissingCapabilitiesAreUnknownNotReady: 缺 capabilities 时不宣称可创建 Run', async () => {
    fetchReadiness.mockResolvedValue({
      reachable: true,
      status: 'ready',
      components: {
        planner: { ready: true, required: true },
        project: { ready: true, required: true },
        context: { ready: true, required: true },
      },
    });
    await mountGate();

    // Legacy fallback keeps the shell usable for browsing, but the strip must
    // not claim execution/merge.
    expect(text()).toContain(CHILD_TEXT);
    expect(capabilityState('browse')).toBe('unavailable');
    expect(capabilityState('run')).toBe('unavailable');
    expect(capabilityState('merge')).toBe('unavailable');
    expect(strip()?.getAttribute('data-capability-strip')).toBe('unknown');
    expect(text()).toContain('能力未知');
    const reasonButton = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').includes('原因'),
    );
    expect(reasonButton).toBeTruthy();
    await act(async () => {
      reasonButton!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    expect(text()).toContain('后端未返回能力集合');
  });

  it('RecoveredBackendUpdatesOnRecheck: 从 unavailable 恢复到 ready 后重新检查即更新', async () => {
    fetchReadiness.mockResolvedValueOnce(
      readiness({
        capabilities: capabilities({
          execution: unavailableExecution('vault', 'vault_locked'),
          merge: unavailable('vault', 'vault_locked'),
        }),
      }),
    );
    await mountGate();
    expect(strip()?.getAttribute('data-capability-strip')).toBe('locked');
    expect(capabilityState('run')).toBe('unavailable');

    // The dependency is repaired; a re-check (the strip's button, or the
    // background interval) must reflect it without a frontend restart.
    fetchReadiness.mockResolvedValueOnce(readiness());
    const recheck = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent ?? '').includes('重新检查'),
    );
    expect(recheck).toBeTruthy();
    await act(async () => {
      recheck!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });

    expect(strip()?.getAttribute('data-capability-strip')).toBe('ready');
    expect(capabilityState('run')).toBe('ready');
    expect(capabilityState('merge')).toBe('ready');
    expect(fetchReadiness).toHaveBeenCalledTimes(2);
  });
});
