// Readiness → capability view model (plan section 15 T0.12 step 4, section 28
// T0.12.c).
//
// The backend answers two different questions on GET /ready:
//   - the HTTP verdict (`status`): are the required legacy services and the
//     required probes wired? A 503 here means the read-only surface itself is
//     broken.
//   - `data.capabilities`: read_only / execution / merge — what the shell may
//     actually do, per backend for execution.
//
// This module turns that payload into the three states the shell displays
// ("可浏览 / 可创建 Run / 可合入") plus the Chinese explanation of every
// blocker. It is a pure function of the response: no caching, no I/O, so a
// re-check after a dependency was repaired produces a fresh verdict without
// restarting the frontend.
//
// "Unknown" is a first-class outcome, never a silent "ready": a backend that
// does not publish capabilities (older build) or publishes something
// unparseable leaves every flag false with an explicit reason. A missing
// capability set must never let the UI claim it can create a Run.

import type {
  ReadinessBlocker,
  ReadinessCapability,
} from '../../generated/openapi-types';
import type { Readiness, ReadinessCapabilitiesView } from '../services-bridge/readiness';

/** The summary state shown on the gate / status strip. */
export type CapabilityDisplayState =
  | 'ready'
  | 'browse_only'
  | 'locked'
  | 'unavailable'
  | 'no_merge'
  | 'unknown';

/** One blocking dependency, ready for display. */
export interface CapabilityBlockerView {
  component: string;
  /** Machine-readable dependency state (ready|degraded|failed|not_configured). */
  state: string;
  errorCode: string;
  /** Machine-readable remediation code (may be an unknown code from a newer backend). */
  remediation: string;
  /** Chinese explanation of what is blocking. */
  message: string;
  /** Chinese suggestion of what would unblock it. */
  action: string;
}

/** One backend's execution capability. */
export interface BackendCapabilityView {
  state: 'ready' | 'unavailable';
  blockers: CapabilityBlockerView[];
}

export interface ReadinessView {
  display: CapabilityDisplayState;
  /** Chinese label of `display`. */
  label: string;
  /** read_only is ready: the user may browse and read. */
  canBrowse: boolean;
  /** execution is ready for at least one backend: a Run may be created. */
  canCreateRun: boolean;
  /** merge is ready: finished work may be merged back. */
  canMerge: boolean;
  /** Per-backend execution capability, keyed by backend name (codex, ...). */
  backends: Record<string, BackendCapabilityView>;
  /** Deduplicated blockers of read_only/execution/merge, stable order. */
  blockers: CapabilityBlockerView[];
  /** Why the capability set is unknown, or null when it was parsed. */
  unknownReason: string | null;
  /** The backend process could not be reached at all. */
  unreachable: boolean;
}

/**
 * Chinese copy for every remediation code the backend may publish (plan
 * section 15 T0.12 step 5). Unknown codes fall back to UNKNOWN_REMEDIATION_TEXT.
 */
export const REMEDIATION_TEXT: Record<string, { message: string; action: string }> = {
  backend_not_installed: {
    message: '执行后端尚未安装或未接线',
    action: '安装并配置该执行后端（claude_code / codex / gemini），或改用已就绪的后端',
  },
  migrations_pending: {
    message: '运行时数据库迁移尚未完成',
    action: '让后端完成运行时库迁移后重新检查',
  },
  vault_locked: {
    message: '密钥库已锁定',
    action: '解锁密钥库（输入主密码或配置解锁方式）',
  },
  outbox_unavailable: {
    message: '事件 outbox 派发不可用',
    action: '启动 outbox 派发器并确认事件库可写',
  },
  event_store_unavailable: {
    message: '事件存储不可用',
    action: '检查事件存储连接、权限与磁盘空间',
  },
  workspace_roots_unconfigured: {
    message: '未配置工作区根目录',
    action: '配置工作区根目录（CODEFLOW_WORKSPACE_ROOTS）后重新检查',
  },
  workspace_root_missing: {
    message: '已配置的工作区根目录不存在',
    action: '恢复该目录，或在配置中改到存在的路径',
  },
  policy_not_installed: {
    message: '策略评估器未安装或未要求强制执行',
    action: '启动后端时安装策略评估器并开启强制执行',
  },
  protocol_mismatch: {
    message: '前后端协议版本不一致',
    action: '把前端与后端升级到同一协议版本',
  },
  dependency_not_ready: {
    message: '依赖尚未就绪',
    action: '修复该依赖后重新检查',
  },
};

/** Fallback copy for a remediation code this build does not know. */
export const UNKNOWN_REMEDIATION_TEXT: { message: string; action: string } = {
  message: '依赖未就绪（未知原因码）',
  action: '查看后端日志，修复该依赖后重新检查',
};

const DISPLAY_LABELS: Record<CapabilityDisplayState, string> = {
  ready: '就绪',
  browse_only: '仅可浏览',
  locked: '已锁定',
  unavailable: '不可用',
  no_merge: '可创建但不可合入',
  unknown: '能力未知',
};

/** Chinese copy for one blocker; unknown codes keep the raw code visible. */
export function blockerView(blocker: ReadinessBlocker): CapabilityBlockerView {
  const remediation = blocker.remediation ?? '';
  const known = REMEDIATION_TEXT[remediation];
  const fallbackDetail = remediation || blocker.error_code || '未提供原因码';
  return {
    component: blocker.component,
    state: blocker.state,
    errorCode: blocker.error_code ?? '',
    remediation,
    message: known
      ? known.message
      : `${UNKNOWN_REMEDIATION_TEXT.message}（${fallbackDetail}）`,
    action: known ? known.action : UNKNOWN_REMEDIATION_TEXT.action,
  };
}

function blockerViews(blocking: ReadinessBlocker[] | undefined): CapabilityBlockerView[] {
  return (blocking ?? []).map(blockerView);
}

function capabilityOf(capability: ReadinessCapability | undefined): {
  state: 'ready' | 'unavailable';
  blockers: CapabilityBlockerView[];
} {
  const ready = capability?.state === 'ready';
  return {
    state: ready ? 'ready' : 'unavailable',
    blockers: ready ? [] : blockerViews(capability?.blocking),
  };
}

/** Deduplicate blockers by component + error code + remediation, keeping order. */
function dedupe(blockers: CapabilityBlockerView[]): CapabilityBlockerView[] {
  const seen = new Set<string>();
  const out: CapabilityBlockerView[] = [];
  for (const blocker of blockers) {
    const key = `${blocker.component}\u0000${blocker.errorCode}\u0000${blocker.remediation}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(blocker);
  }
  return out;
}

function unknownView(reason: string, unreachable: boolean): ReadinessView {
  return {
    display: 'unknown',
    label: DISPLAY_LABELS.unknown,
    canBrowse: false,
    canCreateRun: false,
    canMerge: false,
    backends: {},
    blockers: [],
    unknownReason: reason,
    unreachable,
  };
}

function unreachableView(): ReadinessView {
  return {
    display: 'unavailable',
    label: DISPLAY_LABELS.unavailable,
    canBrowse: false,
    canCreateRun: false,
    canMerge: false,
    backends: {},
    blockers: [],
    unknownReason: null,
    unreachable: true,
  };
}

/**
 * Map one /ready response to the display model. `null` (never checked yet) and
 * an unreachable backend both yield "not usable"; capabilities are only
 * reported as available when the backend published a parseable set.
 */
export function buildReadinessView(readiness: Readiness | null | undefined): ReadinessView {
  if (!readiness || !readiness.reachable) return unreachableView();

  const capabilities: ReadinessCapabilitiesView | null | undefined = readiness.capabilities;
  if (!capabilities) {
    return unknownView('后端未返回能力集合（capabilities），无法判断可执行性', false);
  }

  const readOnly = capabilityOf(capabilities.read_only);
  const execution = capabilityOf(capabilities.execution);
  const merge = capabilityOf(capabilities.merge);

  const backends: Record<string, BackendCapabilityView> = {};
  for (const [name, backend] of Object.entries(capabilities.execution.backends ?? {})) {
    backends[name] = capabilityOf(backend);
  }

  const canBrowse = readOnly.state === 'ready';
  // Execution and merge are clamped by read_only: the backend already implies
  // this (a not-ready read_only makes both unavailable), and a UI that claimed
  // "can create a Run" while the read-only surface is broken is exactly the
  // failure the plan forbids. Clamping keeps that true even for a payload that
  // contradicts itself.
  const canCreateRun = canBrowse && execution.state === 'ready';
  const canMerge = canBrowse && merge.state === 'ready';
  const blockers = dedupe([...readOnly.blockers, ...execution.blockers, ...merge.blockers]);
  const locked = blockers.some((blocker) => blocker.remediation === 'vault_locked');

  let display: CapabilityDisplayState;
  if (!canBrowse) {
    display = 'unavailable';
  } else if (!canCreateRun && locked) {
    display = 'locked';
  } else if (!canCreateRun) {
    display = 'browse_only';
  } else if (!canMerge) {
    // Unreachable while execution depends on workspace (a ready execution
    // implies a ready workspace); it becomes reachable when T1.09 appends the
    // merge-specific dependencies.
    display = 'no_merge';
  } else {
    display = 'ready';
  }

  return {
    display,
    label: DISPLAY_LABELS[display],
    canBrowse,
    canCreateRun,
    canMerge,
    backends,
    blockers,
    unknownReason: null,
    unreachable: false,
  };
}

/**
 * Whether a Run may be created for one execution backend. T1.14 (Run creation
 * UI) asks per backend: a usable codex must not be blocked by an uninstalled
 * gemini. An unknown backend is never executable.
 */
export function canCreateRunForBackend(view: ReadinessView, backend: string): boolean {
  return view.backends[backend]?.state === 'ready';
}

/** Backends a Run may target right now, sorted by name. */
export function executableBackends(view: ReadinessView): string[] {
  return Object.keys(view.backends)
    .filter((name) => view.backends[name].state === 'ready')
    .sort();
}
