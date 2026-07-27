import { get, getApiBase } from '../../api';

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
