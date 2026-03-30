import { get, post, patch } from '../api';
import { API_ENDPOINTS } from '../api';
import type { PluginCatalogResponse, PluginDetailResponse, PluginListResponse } from '../types';

const BASE = API_ENDPOINTS.plugins;

export interface PluginListParams {
  scope?: string;
  status?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

export function listPlugins(params?: PluginListParams, signal?: AbortSignal) {
  return get<PluginListResponse>(BASE, params as Record<string, string | number | undefined>, signal);
}

export function getPlugin(pluginId: string, signal?: AbortSignal) {
  return get<PluginDetailResponse>(`${BASE}/${pluginId}`, undefined, signal);
}

export function listMarketplacePlugins(params?: Pick<PluginListParams, 'search' | 'limit' | 'offset'>, signal?: AbortSignal) {
  return get<PluginCatalogResponse>(`${BASE}/marketplace`, params as Record<string, string | number | undefined>, signal);
}

export function installPlugin(pluginId: string, signal?: AbortSignal) {
  return post<PluginDetailResponse>(`${BASE}/${pluginId}/install`, undefined, signal);
}

export function togglePlugin(pluginId: string, enabled: boolean, signal?: AbortSignal) {
  return patch<PluginDetailResponse>(`${BASE}/${pluginId}`, { enabled }, signal);
}
