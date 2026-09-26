// FU for the readiness capability view model (T0.12.c).
//
// The view model is the single mapping from GET /ready's capabilities to what
// the shell displays ("可浏览 / 可创建 Run / 可合入"), so the acceptance
// assertions live here as table-driven cases: all-ready, execution blocked but
// browsing works, locked vault, read_only unavailable, and — the case that must
// never regress — a backend that publishes no capabilities at all must be
// "unknown", never "ready".
import { describe, expect, it } from 'vitest';
import type { ReadinessBlocker, ReadinessCapability } from '../../generated/openapi-types';
import type { Readiness, ReadinessCapabilitiesView } from '../services-bridge/readiness';
import {
  buildReadinessView,
  canCreateRunForBackend,
  executableBackends,
  REMEDIATION_TEXT,
  UNKNOWN_REMEDIATION_TEXT,
} from './readinessView';

const ready = (): ReadinessCapability => ({ state: 'ready', blocking: [] });

function blocker(
  component: string,
  remediation: string,
  overrides: Partial<ReadinessBlocker> = {},
): ReadinessBlocker {
  return {
    component,
    state: 'not_configured',
    error_code: remediation,
    remediation,
    ...overrides,
  };
}

function capability(blocking: ReadinessBlocker[]): ReadinessCapability {
  return { state: 'unavailable', blocking };
}

function caps(overrides: Partial<ReadinessCapabilitiesView> = {}): ReadinessCapabilitiesView {
  return {
    read_only: ready(),
    execution: { ...ready(), backends: { codex: ready() } },
    merge: ready(),
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
    },
    capabilities: caps(),
    ...overrides,
  };
}

/** Build a Readiness whose capabilities are deliberately malformed. */
function readinessWithRawCapabilities(raw: unknown): Readiness {
  return {
    reachable: true,
    status: 'ready',
    components: {},
    capabilities: raw as ReadinessCapabilitiesView,
  };
}

describe('ReadinessViewStates', () => {
  it('AllReady: 三项均可用时显示 ready 且三个标志全为真', () => {
    const view = buildReadinessView(readiness());
    expect(view.display).toBe('ready');
    expect(view.label).toBe('就绪');
    expect(view.canBrowse).toBe(true);
    expect(view.canCreateRun).toBe(true);
    expect(view.canMerge).toBe(true);
    expect(view.blockers).toEqual([]);
    expect(view.unknownReason).toBeNull();
    expect(view.unreachable).toBe(false);
  });

  it('BrowseOnly: read_only 可用、execution 不可用时放行浏览并给出原因', () => {
    const view = buildReadinessView(
      readiness({
        status: 'ready',
        capabilities: caps({
          execution: {
            ...capability([
              blocker('exec_backend:codex', 'backend_not_installed'),
              blocker('migrations', 'migrations_pending'),
            ]),
            backends: { codex: capability([blocker('exec_backend:codex', 'backend_not_installed')]) },
          },
          merge: capability([blocker('migrations', 'migrations_pending')]),
        }),
      }),
    );
    expect(view.display).toBe('browse_only');
    expect(view.canBrowse).toBe(true);
    expect(view.canCreateRun).toBe(false);
    expect(view.canMerge).toBe(false);
    // Payload order is preserved and the blocker shared by execution and merge
    // is listed once.
    expect(view.blockers.map((b) => b.component)).toEqual(['exec_backend:codex', 'migrations']);
    expect(view.blockers.every((b) => b.message.length > 0 && b.action.length > 0)).toBe(true);
    expect(canCreateRunForBackend(view, 'codex')).toBe(false);
    expect(executableBackends(view)).toEqual([]);
  });

  it('Locked: 阻塞项含 vault_locked 时显示 locked（而非笼统的不可用）', () => {
    const view = buildReadinessView(
      readiness({
        capabilities: caps({
          execution: { ...capability([blocker('vault', 'vault_locked')]), backends: {} },
          merge: capability([blocker('vault', 'vault_locked')]),
        }),
      }),
    );
    expect(view.display).toBe('locked');
    expect(view.canBrowse).toBe(true);
    expect(view.canCreateRun).toBe(false);
    expect(view.blockers[0].message).toContain('密钥库');
    expect(view.blockers[0].action).toContain('解锁');
  });

  it('Unavailable: read_only 不可用即整体不可用（含后端不可达）', () => {
    const blocked = buildReadinessView(
      readiness({
        status: 'not_ready',
        capabilities: caps({ read_only: capability([blocker('memory', 'dependency_not_ready')]) }),
      }),
    );
    expect(blocked.display).toBe('unavailable');
    expect(blocked.canBrowse).toBe(false);
    expect(blocked.canCreateRun).toBe(false);
    expect(blocked.canMerge).toBe(false);
    expect(blocked.unreachable).toBe(false);
    expect(blocked.blockers.map((b) => b.component)).toEqual(['memory']);

    // A self-contradicting payload (read_only unavailable but execution ready)
    // must not read as "can create a Run".
    const contradictory = buildReadinessView(
      readiness({
        status: 'not_ready',
        capabilities: caps({ read_only: capability([blocker('memory', 'dependency_not_ready')]) }),
      }),
    );
    expect(contradictory.canBrowse).toBe(false);
    expect(contradictory.canCreateRun).toBe(false);
    expect(contradictory.canMerge).toBe(false);

    const unreachable = buildReadinessView({
      reachable: false,
      status: 'unreachable',
      components: {},
    });
    expect(unreachable.display).toBe('unavailable');
    expect(unreachable.unreachable).toBe(true);
    expect(unreachable.canBrowse).toBe(false);
    expect(unreachable.canCreateRun).toBe(false);
  });

  it('NotCheckedYetIsUnavailable: 尚未检查（null）时不可用而非 ready', () => {
    const view = buildReadinessView(null);
    expect(view.display).toBe('unavailable');
    expect(view.unreachable).toBe(true);
    expect(view.canCreateRun).toBe(false);
  });

  it('UnknownCapabilitiesIsNeverReady: 缺 capabilities 字段时未知，绝不当成 ready', () => {
    const missing = buildReadinessView({
      reachable: true,
      status: 'ready',
      components: { planner: { ready: true, required: true } },
    });
    expect(missing.display).toBe('unknown');
    expect(missing.canBrowse).toBe(false);
    expect(missing.canCreateRun).toBe(false);
    expect(missing.canMerge).toBe(false);
    expect(missing.unknownReason).toBeTruthy();
    expect(executableBackends(missing)).toEqual([]);
    expect(canCreateRunForBackend(missing, 'codex')).toBe(false);
  });

  it('CapabilitiesWithoutBackendsAreLegal: 无 backends 键表示没有已注册后端', () => {
    const withoutBackends = {
      read_only: { state: 'ready', blocking: [] },
      execution: { state: 'ready', blocking: [] },
      merge: { state: 'ready', blocking: [] },
    };
    const view = buildReadinessView(readinessWithRawCapabilities(withoutBackends));
    expect(view.display).toBe('ready');
    expect(view.backends).toEqual({});
    expect(executableBackends(view)).toEqual([]);
  });

  it('UnknownCapabilityStateIsUnknown: 未知 state 值按不可用处理而非猜测', () => {
    // The view model is fed by the bridge, which only ever publishes the two
    // documented states; a hand-built payload with an unknown state must still
    // not read as executable.
    const badState = {
      read_only: { state: 'ready', blocking: [] },
      execution: { state: 'maybe', blocking: [] },
      merge: { state: 'ready', blocking: [] },
    };
    const view = buildReadinessView(readinessWithRawCapabilities(badState));
    expect(view.canCreateRun).toBe(false);
    expect(view.display).toBe('browse_only');
  });

  it('PerBackendSeparation: 一个后端可用即可创建 Run，其他后端各自不可用', () => {
    const view = buildReadinessView(
      readiness({
        capabilities: caps({
          execution: {
            state: 'ready',
            blocking: [],
            backends: {
              codex: ready(),
              gemini: capability([blocker('exec_backend:gemini', 'backend_not_installed')]),
            },
          },
        }),
      }),
    );
    expect(view.display).toBe('ready');
    expect(canCreateRunForBackend(view, 'codex')).toBe(true);
    expect(canCreateRunForBackend(view, 'gemini')).toBe(false);
    expect(canCreateRunForBackend(view, 'claude_code')).toBe(false);
    expect(executableBackends(view)).toEqual(['codex']);
    expect(view.backends.gemini.blockers[0].remediation).toBe('backend_not_installed');
  });
});

describe('RemediationTextMapping', () => {
  it('EveryKnownRemediationCodeHasChineseText: 十个已知码逐一有中文说明与建议', () => {
    const codes = [
      'backend_not_installed',
      'migrations_pending',
      'vault_locked',
      'outbox_unavailable',
      'event_store_unavailable',
      'workspace_roots_unconfigured',
      'workspace_root_missing',
      'policy_not_installed',
      'protocol_mismatch',
      'dependency_not_ready',
    ];
    expect(Object.keys(REMEDIATION_TEXT).sort()).toEqual([...codes].sort());
    for (const code of codes) {
      const view = buildReadinessView(
        readiness({
          capabilities: caps({
            read_only: capability([blocker(`component-${code}`, code)]),
          }),
        }),
      );
      const entry = view.blockers[0];
      expect(entry.message, code).toBe(REMEDIATION_TEXT[code].message);
      expect(entry.action, code).toBe(REMEDIATION_TEXT[code].action);
      expect(entry.message).not.toContain('未知原因码');
    }
  });

  it('UnknownRemediationFallsBack: 未知 remediation 码有兜底文案且保留原始码', () => {
    const view = buildReadinessView(
      readiness({
        capabilities: caps({
          read_only: capability([blocker('frobnicator', 'future_dependency_missing')]),
        }),
      }),
    );
    const entry = view.blockers[0];
    expect(entry.message).toContain(UNKNOWN_REMEDIATION_TEXT.message);
    expect(entry.message).toContain('future_dependency_missing');
    expect(entry.action).toBe(UNKNOWN_REMEDIATION_TEXT.action);
    expect(entry.remediation).toBe('future_dependency_missing');
    expect(entry.component).toBe('frobnicator');
  });

  it('EmptyRemediationStillExplains: 空码也给出可读原因（回落到 error_code）', () => {
    const view = buildReadinessView(
      readiness({
        capabilities: caps({
          read_only: capability([
            {
              component: 'event_store',
              state: 'failed',
              error_code: 'probe_timeout',
              remediation: '',
            },
          ]),
        }),
      }),
    );
    expect(view.blockers[0].message).toContain('probe_timeout');
    expect(view.blockers[0].message).toContain(UNKNOWN_REMEDIATION_TEXT.message);
  });
});

describe('BlockerDeduplication', () => {
  it('SameBlockerAcrossCapabilitiesListedOnce: 同一阻塞项跨能力集合只出现一次', () => {
    const vault = blocker('vault', 'vault_locked');
    const view = buildReadinessView(
      readiness({
        capabilities: caps({
          execution: { ...capability([vault]), backends: {} },
          merge: capability([vault]),
        }),
      }),
    );
    expect(view.blockers).toHaveLength(1);
    expect(view.blockers[0].component).toBe('vault');
  });
});
