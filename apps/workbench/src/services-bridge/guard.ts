import { get, post, getApiBase } from '../../api';

export interface GuardRule {
  id: string;
  severity: string;
}

export interface GuardRulesResponse {
  items: GuardRule[];
  total: number;
  denied_path_globs?: string[];
  max_file_bytes?: number;
}

export type ExemptionRequestStatus = 'pending' | 'approved' | 'rejected';

/** One guard exemption request (backend guard.ExemptionRequest JSON shape). */
export interface ExemptionRequestRecord {
  id: string;
  path: string;
  rule_id?: string;
  reason: string;
  requester: string;
  status: ExemptionRequestStatus;
  decided_by?: string;
  created_at: string;
  decided_at?: string;
}

/** GET /api/v1/guard/rules — known rule ids + active severity (experimental). */
export async function guardRules(signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return {
        items: [...store.MOCK_GUARD_RULES],
        total: store.MOCK_GUARD_RULES.length,
        ...store.MOCK_GUARD_META,
      };
    }
  }
  return get<GuardRulesResponse>(`${getApiBase()}/api/v1/guard/rules`, undefined, signal);
}

/** GET /api/v1/guard/exemption-requests?status= — approval queue (experimental). */
export async function listExemptionRequests(
  status?: ExemptionRequestStatus,
  signal?: AbortSignal,
) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const items = (store.MOCK_EXEMPTION_REQUESTS as ExemptionRequestRecord[]).filter(
        (r) => !status || r.status === status,
      );
      return { items: items.map((r) => ({ ...r })), total: items.length };
    }
  }
  return get<{ items: ExemptionRequestRecord[]; total: number }>(
    `${getApiBase()}/api/v1/guard/exemption-requests`,
    { status },
    signal,
  );
}

/** POST /api/v1/guard/exemption-requests/:id/decide — approve or reject. */
export async function decideExemptionRequest(
  id: string,
  decision: 'approve' | 'reject',
  reason?: string,
  signal?: AbortSignal,
) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      const list = store.MOCK_EXEMPTION_REQUESTS as ExemptionRequestRecord[];
      const idx = list.findIndex((r) => r.id === id);
      if (idx === -1) throw new Error(`exemption request not found: ${id}`);
      if (list[idx].status !== 'pending') throw new Error('exemption request already decided');
      const decided: ExemptionRequestRecord = {
        ...list[idx],
        status: decision === 'approve' ? 'approved' : 'rejected',
        decided_by: 'workbench-user',
        decided_at: new Date().toISOString(),
      };
      list[idx] = decided;
      return { ...decided };
    }
  }
  return post<ExemptionRequestRecord>(
    `${getApiBase()}/api/v1/guard/exemption-requests/${id}/decide`,
    { approve: decision === 'approve', decided_by: 'workbench-user', reason },
    signal,
  );
}
