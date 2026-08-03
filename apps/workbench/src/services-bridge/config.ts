// Global config client (GET /api/v1/config/global). Only the fields the
// workbench consumes are typed; the payload passes through otherwise.
import { get, getApiBase } from '../../api';

export interface GlobalConfigView {
  default_model?: string;
  api_pool?: unknown[];
  public_mcp?: string[];
  summary_threshold?: number;
  max_retries?: number;
  timeout?: number;
}

export async function getGlobalConfig(signal?: AbortSignal): Promise<GlobalConfigView> {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      return { ...store.MOCK_GLOBAL_CONFIG };
    }
  }
  return get<GlobalConfigView>(`${getApiBase()}/api/v1/config/global`, undefined, signal);
}
