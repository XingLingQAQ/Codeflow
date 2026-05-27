import { API_ENDPOINTS } from '../api';
import type { AuditLogListResponse } from '../types';

const getBase = () => API_ENDPOINTS.audit;

function buildUrl(params?: {
  actor_id?: string;
  resource_id?: string;
  resource_type?: string;
  outcome?: string;
  limit?: number;
  offset?: number;
}) {
  const search = new URLSearchParams();
  Object.entries(params ?? {}).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      search.set(key, String(value));
    }
  });
  const qs = search.toString();
  return `${getBase()}/logs${qs ? `?${qs}` : ''}`;
}

export async function listAuditLogs(
  params?: {
    actor_id?: string;
    resource_id?: string;
    resource_type?: string;
    outcome?: string;
    limit?: number;
    offset?: number;
  },
  signal?: AbortSignal,
): Promise<AuditLogListResponse> {
  const response = await fetch(buildUrl(params), {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
    signal,
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch audit logs: HTTP ${response.status}`);
  }

  const body = await response.json();
  if (body?.success === true && body.data) {
    return body.data as AuditLogListResponse;
  }

  return {
    entries: Array.isArray(body?.entries) ? body.entries : [],
    total: typeof body?.total === 'number' ? body.total : 0,
    has_more: Boolean(body?.has_more),
  };
}
